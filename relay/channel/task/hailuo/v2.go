package hailuo

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// ---------------------------------------------------------------------------
// MiniMax v2（H3 家族）—— 与 v1（Hailuo）字段形状完全不同，独立成文件。
//
// v1：POST /v1/video_generation，扁平字段 prompt/first_frame_image/
//     subject_reference，轮询 ?task_id=，成功后再 /v1/files/retrieve 换直链。
// v2：POST /v2/video_generation，**content[] 多模态数组**（type=text/image_url/
//     video_url/audio_url + role），轮询 /v2/query/video_generation/{task_id}，
//     成功即 task.content.url（**单步取流，无需 file_id 中转**）。
// 失败时 v2 也不走 base_resp，而是 {"type":"error","error":{type,message,http_code}}。
// ---------------------------------------------------------------------------

const (
	// 端点：v2 生成与轮询。注意轮询是 **路径参数**，不是 query。
	V2VideoEndpoint        = "/v2/video_generation"
	V2QueryTaskEndpointFmt = "/v2/query/video_generation/%s"
	// H3-Context-IR：只产提示词，不产视频。对标即梦「视频反解」。
	V2ContextIREndpoint = "/v2/h3_context_ir"

	// 对外模型名。Context-IR 的载荷 model 必须是 MiniMax-H3（端点名 ≠ 模型名）。
	ModelH3          = "MiniMax-H3"
	ModelH3Max       = "MiniMax-H3-Max"
	ModelH3ContextIR = "MiniMax-H3-Context-IR"

	// v2 分辨率取值（官方文档大写形式）。
	Resolution480P = "480P"
	Resolution2K   = "2K"
)

// v2 输入素材硬限制（官方文档 Input Requirements；超限上游会 400，
// 这里提前拦成 400 省一次往返）。
const (
	v2MaxReferenceImages = 9
	v2MaxReferenceVideos = 3
	v2MaxReferenceAudios = 3
	v2MaxTotalMediaBytes = 64 << 20 // 整个请求体 ≤ 64MB
)

// v2 content[] 的 role 取值。首尾帧与参考素材**互斥**（上游明确拒绝混用）。
const (
	roleFirstFrame     = "first_frame"
	roleLastFrame      = "last_frame"
	roleReferenceImage = "reference_image"
	roleReferenceVideo = "reference_video"
	roleReferenceAudio = "reference_audio"
)

// v2 允许的画面比例。
var v2Ratios = []string{"adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}

// ---------------------------------------------------------------------------
// 载荷
// ---------------------------------------------------------------------------

// V2ContentItem 是 v2 content[] 的一项。四种 type 共用 role 字段（text 不用）。
type V2ContentItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	VideoURL string `json:"video_url,omitempty"`
	AudioURL string `json:"audio_url,omitempty"`
	Role     string `json:"role,omitempty"`
}

// V2VideoRequest 是 v2 建单载荷。
type V2VideoRequest struct {
	Model      string          `json:"model"`
	Content    []V2ContentItem `json:"content"`
	Duration   int             `json:"duration,omitempty"`
	Resolution string          `json:"resolution,omitempty"`
	Ratio      string          `json:"ratio,omitempty"`
}

// V2TaskContent 是 v2 产物容器：视频给 url，Context-IR 给 prompt。
type V2TaskContent struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// V2Usage 是上游回传的**真实**用量。计费以它为准，而不是我们预估的时长——
// 参考图超出免费张数要另计费，预估会系统性少收。
type V2Usage struct {
	TotalSeconds    int `json:"total_seconds"`
	InputSeconds    int `json:"input_seconds"`
	OutputSeconds   int `json:"output_seconds"`
	InputImageCount int `json:"input_image_count"`
}

type V2Task struct {
	ID         string        `json:"id"`
	Model      string        `json:"model"`
	Status     string        `json:"status"`
	Content    V2TaskContent `json:"content"`
	Resolution string        `json:"resolution"`
	Duration   int           `json:"duration"`
	Ratio      string        `json:"ratio"`
	TaskType   string        `json:"task_type"`
	Modality   string        `json:"modality"`
	Usage      *V2Usage      `json:"usage"`
}

type V2QueryTaskResponse struct {
	Task *V2Task `json:"task"`
	// v2 失败形状：{"type":"error","error":{...,"http_code":"402"}}
	Type     string       `json:"type,omitempty"`
	Error    *V2ErrorBody `json:"error,omitempty"`
	BaseResp BaseResp     `json:"base_resp"`
}

type V2ErrorBody struct {
	Type     string `json:"type"`
	Message  string `json:"message"`
	HTTPCode string `json:"http_code"`
}

// V2CreateResponse 是 v2 建单响应（与 v1 同形：task_id + base_resp）。
type V2CreateResponse struct {
	TaskID   string       `json:"task_id"`
	BaseResp BaseResp     `json:"base_resp"`
	Type     string       `json:"type,omitempty"`
	Error    *V2ErrorBody `json:"error,omitempty"`
}

// v2 任务状态枚举（官方 callback 文档）。
const (
	V2StatusQueued    = "queued"
	V2StatusRunning   = "running"
	V2StatusSucceeded = "succeeded"
	V2StatusFailed    = "failed"
	V2StatusCancelled = "cancelled"
)

// ---------------------------------------------------------------------------
// 能力矩阵
// ---------------------------------------------------------------------------

// V2ModelSpec 描述一个 v2 模型。**门控一律 fail-loud**：越界直接 400，
// 绝不静默改档——静默改档会交付一个和用户要求不同的成片。
type V2ModelSpec struct {
	Name         string
	UpstreamName string // 载荷里的 model；Context-IR 恒为 MiniMax-H3
	ContextIR    bool   // true → 端点为 /v2/h3_context_ir，产物是提示词
	Resolutions  []string
	// ResolutionRatios 相对**基础档**（列表首项）的价格倍率，对应官方价目：
	// H3 768P $0.08/s → 2K $0.13/s = 1.625；H3-Max 480P $0.05/s → 768P $0.08/s = 1.6。
	// 写在 spec 里而不是散在计费逻辑中，上游调价只改这一处。
	ResolutionRatios map[string]float64
	MinDuration      int
	MaxDuration      int
	DefaultRes       string
	DefaultDur       int
	RequireRatio     bool // 纯文生视频必须显式给比例（上游不接受 adaptive）
}

var v2ModelSpecs = map[string]V2ModelSpec{
	ModelH3: {
		Name: ModelH3, UpstreamName: ModelH3,
		Resolutions:      []string{Resolution768P, Resolution2K},
		ResolutionRatios: map[string]float64{Resolution768P: 1.0, Resolution2K: 1.625},
		MinDuration:      4, MaxDuration: 15,
		DefaultRes: Resolution768P, DefaultDur: 6,
		RequireRatio: true,
	},
	ModelH3Max: {
		Name: ModelH3Max, UpstreamName: ModelH3Max,
		Resolutions:      []string{Resolution480P, Resolution768P},
		ResolutionRatios: map[string]float64{Resolution480P: 1.0, Resolution768P: 1.6},
		MinDuration:      5, MaxDuration: 15,
		DefaultRes: Resolution768P, DefaultDur: 6,
		RequireRatio: true,
	},
	ModelH3ContextIR: {
		Name: ModelH3ContextIR, UpstreamName: ModelH3,
		ContextIR:        true,
		Resolutions:      []string{Resolution768P, Resolution2K},
		ResolutionRatios: map[string]float64{Resolution768P: 1.0, Resolution2K: 1.625},
		MinDuration:      4, MaxDuration: 15,
		DefaultRes: Resolution768P, DefaultDur: 6,
		RequireRatio: true,
	},
}

func isV2Model(model string) bool {
	_, ok := v2ModelSpecs[model]
	return ok
}

func lookupV2Spec(model string) (V2ModelSpec, bool) {
	spec, ok := v2ModelSpecs[model]
	return spec, ok
}

// ---------------------------------------------------------------------------
// 入参校验
// ---------------------------------------------------------------------------

func validateV2Request(req *relaycommon.TaskSubmitReq, spec V2ModelSpec) *dto.TaskError {
	// prompt 对所有 v2 模型都是必填（包括 Context-IR：上游要求 content 里必须
	// 有一个非空 text 项）。不校验的话错误会漏到 BuildRequestPayload 才暴露，
	// 变成 500 build_request_failed —— 用户能自己修的错误必须是 400。
	if strings.TrimSpace(req.Prompt) == "" {
		return taskErrBadRequest("prompt is required")
	}
	// 纯文生视频（content 只有 text）时上游不接受 adaptive：必须在本地拦。
	if spec.RequireRatio {
		ratio, err := resolveV2Ratio(req)
		if err != nil {
			return taskErrBadRequest(err.Error())
		}
		if ratio == "adaptive" && len(collectV2Media(req)) == 0 {
			return taskErrBadRequest(fmt.Sprintf(
				"ratio is required for text-to-video %s: pass metadata.ratio (one of %s) or size",
				spec.Name, strings.Join(nonAdaptiveV2Ratios(), "/")))
		}
	}
	if req.Duration != 0 && (req.Duration < spec.MinDuration || req.Duration > spec.MaxDuration) {
		return taskErrBadRequest(fmt.Sprintf(
			"duration must be between %d and %d for %s, got %d",
			spec.MinDuration, spec.MaxDuration, spec.Name, req.Duration))
	}
	if _, err := resolveV2Resolution(req, spec); err != nil {
		return taskErrBadRequest(err.Error())
	}
	if err := validateV2Media(req); err != nil {
		return taskErrBadRequest(err.Error())
	}
	return nil
}

func taskErrBadRequest(msg string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("%s", msg), "invalid_request", http.StatusBadRequest)
}

// validateV2Media 校验素材数量、URL 形态，并强制**首尾帧与参考素材互斥**。
// 混用上游会直接 400，这里提前拦下并说清原因。
func validateV2Media(req *relaycommon.TaskSubmitReq) error {
	items := collectV2Media(req)
	counts := map[string]int{}
	for _, it := range items {
		counts[it.role]++
	}

	frames := counts[roleFirstFrame] + counts[roleLastFrame]
	refs := counts[roleReferenceImage] + counts[roleReferenceVideo] + counts[roleReferenceAudio]
	if frames > 0 && refs > 0 {
		return fmt.Errorf(
			"first/last frame and reference materials are mutually exclusive: got %d frame(s) and %d reference(s)",
			frames, refs)
	}
	if counts[roleFirstFrame] > 1 {
		return fmt.Errorf("at most 1 first_frame image is allowed, got %d", counts[roleFirstFrame])
	}
	if counts[roleLastFrame] > 1 {
		return fmt.Errorf("at most 1 last_frame image is allowed, got %d", counts[roleLastFrame])
	}
	if counts[roleReferenceImage] > v2MaxReferenceImages {
		return fmt.Errorf("at most %d reference images are allowed, got %d",
			v2MaxReferenceImages, counts[roleReferenceImage])
	}
	if counts[roleReferenceVideo] > v2MaxReferenceVideos {
		return fmt.Errorf("at most %d reference videos are allowed, got %d",
			v2MaxReferenceVideos, counts[roleReferenceVideo])
	}
	if counts[roleReferenceAudio] > v2MaxReferenceAudios {
		return fmt.Errorf("at most %d reference audios are allowed, got %d",
			v2MaxReferenceAudios, counts[roleReferenceAudio])
	}
	for _, it := range items {
		if err := validateV2MediaURL(it); err != nil {
			return err
		}
	}
	return nil
}

func validateV2MediaURL(it V2MediaItem) error {
	raw := strings.TrimSpace(it.url)
	if raw == "" {
		return fmt.Errorf("%s url is required", it.role)
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		// data: URI 对远端上游不可达（它要自己去取），提前拦掉。
		return fmt.Errorf("%s must be a public http(s) URL, got scheme %q",
			it.role, mediaScheme(raw))
	}
	return nil
}

func mediaScheme(u string) string {
	if idx := strings.Index(u, ":"); idx > 0 {
		return u[:idx]
	}
	return "(none)"
}

// ---------------------------------------------------------------------------
// 素材归一：把 new-api 的多种入参写法收敛成 content[] 的 role 序列
// ---------------------------------------------------------------------------

// V2MediaItem 是归一后的素材（role + 类型 + URL）。
type V2MediaItem struct {
	role string
	kind string // image / video / audio
	url  string
}

// collectV2Media 把 Image/Images/Media/InputReference 归一为 role 序列。
//
// 角色判定规则（显式优先，缺失才猜）：
//  1. media[].type 含 first/last/reference → 显式角色
//  2. media[].url 里带 #first_frame / #last_frame 片段 → 显式首尾帧
//  3. 单张图 + input_reference 给了视频 → 该图当首帧
//  4. 单张图且无 input_reference → 首帧
//  5. 多张图 → 参考图
//  6. 视频/音频 → 参考素材
func collectV2Media(req *relaycommon.TaskSubmitReq) []V2MediaItem {
	var out []V2MediaItem
	appendImage := func(u string) { out = append(out, V2MediaItem{role: "", kind: "image", url: u}) }

	// input_reference：视频/音频 URL 的主要入口（与即梦反解同约定）。
	if ref := strings.TrimSpace(req.InputReference); ref != "" {
		switch {
		case isAudioURL(ref):
			out = append(out, V2MediaItem{role: roleReferenceAudio, kind: "audio", url: ref})
		case isVideoURL(ref):
			out = append(out, V2MediaItem{role: roleReferenceVideo, kind: "video", url: ref})
		default:
			appendImage(ref)
		}
	}

	for _, raw := range req.Images {
		if u := strings.TrimSpace(raw); u != "" {
			kind, role := classifyV2Media("", u)
			out = append(out, V2MediaItem{role: role, kind: kind, url: u})
		}
	}
	if img := strings.TrimSpace(req.Image); img != "" {
		kind, role := classifyV2Media("", img)
		out = append(out, V2MediaItem{role: role, kind: kind, url: img})
	}
	if aud := strings.TrimSpace(req.Audio); aud != "" {
		out = append(out, V2MediaItem{role: roleReferenceAudio, kind: "audio", url: aud})
	}
	for _, aud := range req.Audios {
		if strings.TrimSpace(aud) != "" {
			out = append(out, V2MediaItem{role: roleReferenceAudio, kind: "audio", url: aud})
		}
	}

	// 显式角色覆盖：media[] 里的 type/片段标注优先于上面的启发式。
	for _, m := range req.Media {
		u := strings.TrimSpace(m.URL)
		if u == "" {
			continue
		}
		kind, role := classifyV2Media(m.Type, u)
		if role == "" {
			// 没标注就并入同类型素材，沿用启发式角色。
			for i := range out {
				if out[i].kind == kind && out[i].role == "" {
					out[i].url = u
					goto nextMedia
				}
			}
			out = append(out, V2MediaItem{role: "", kind: kind, url: u})
		} else {
			out = append(out, V2MediaItem{role: role, kind: kind, url: u})
		}
	nextMedia:
	}

	// 惰性定角色：空 role 的图片按「单张→首帧 / 多张→参考图」落位。
	images := 0
	for _, it := range out {
		if it.kind == "image" {
			images++
		}
	}
	assigned := 0
	for i := range out {
		if out[i].kind == "image" && out[i].role == "" {
			if images == 1 && assigned == 0 {
				out[i].role = roleFirstFrame
			} else {
				out[i].role = roleReferenceImage
			}
			assigned++
		}
	}
	return out
}

func classifyV2Media(declaredType, url string) (kind string, role string) {
	t := strings.ToLower(declaredType)
	lowerURL := strings.ToLower(url)
	switch {
	case strings.Contains(t, "first"):
		return "image", roleFirstFrame
	case strings.Contains(t, "last"):
		return "image", roleLastFrame
	case strings.Contains(t, "reference_image"):
		return "image", roleReferenceImage
	case strings.Contains(t, "reference_video"), strings.Contains(t, "video"):
		return "video", roleReferenceVideo
	case strings.Contains(t, "reference_audio"), strings.Contains(t, "audio"):
		return "audio", roleReferenceAudio
	}
	// URL 片段标注（显式意图，比启发式优先）。
	if idx := strings.Index(lowerURL, "#"); idx >= 0 {
		switch strings.Trim(lowerURL[idx+1:], "?") {
		case "first_frame":
			return "image", roleFirstFrame
		case "last_frame":
			return "image", roleLastFrame
		}
	}
	switch {
	case isAudioURL(url):
		return "audio", ""
	case isVideoURL(url):
		return "video", ""
	default:
		return "image", ""
	}
}

func isVideoURL(u string) bool {
	lower := strings.ToLower(u)
	if idx := strings.Index(lower, "?"); idx >= 0 {
		lower = lower[:idx]
	}
	return strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov") ||
		strings.HasSuffix(lower, ".webm")
}

func isAudioURL(u string) bool {
	lower := strings.ToLower(u)
	if idx := strings.Index(lower, "?"); idx >= 0 {
		lower = lower[:idx]
	}
	return strings.HasSuffix(lower, ".wav") || strings.HasSuffix(lower, ".mp3")
}

// ---------------------------------------------------------------------------
// 比例 / 分辨率
// ---------------------------------------------------------------------------

// resolveV2Ratio 归一画面比例。来源优先级：metadata.ratio/aspect_ratio →
// Size("1280x720") 归一 → adaptive。
func resolveV2Ratio(req *relaycommon.TaskSubmitReq) (string, error) {
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if v, ok := req.Metadata[key]; ok {
				if s := normalizeV2Ratio(fmt.Sprintf("%v", v)); s != "" {
					return s, nil
				}
			}
		}
	}
	if size := strings.TrimSpace(req.Size); size != "" {
		if s := ratioFromSizeV2(size); s != "" {
			return s, nil
		}
	}
	return "adaptive", nil
}

func normalizeV2Ratio(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return ""
	}
	for _, allowed := range v2Ratios {
		if v == allowed {
			return v
		}
	}
	return ""
}

func ratioFromSizeV2(size string) string {
	w, h, ok := parseWH(size)
	if !ok || w <= 0 || h <= 0 {
		return ""
	}
	target := float64(w) / float64(h)
	best, bestDiff := "", math.MaxFloat64
	for _, allowed := range v2Ratios {
		if allowed == "adaptive" {
			continue
		}
		aw, ah, ok := parseRatio(allowed)
		if !ok {
			continue
		}
		diff := math.Abs(float64(aw)/float64(ah) - target)
		if diff < bestDiff {
			best, bestDiff = allowed, diff
		}
	}
	return best
}

// parseRatio 解析 "16:9" 这类比例（分隔符是冒号，不是 parseWH 的 x/*）。
func parseRatio(s string) (int, int, bool) {
	s = strings.TrimSpace(s)
	idx := strings.Index(s, ":")
	if idx < 0 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(s[:idx]))
	h, err2 := strconv.Atoi(strings.TrimSpace(s[idx+1:]))
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func parseWH(s string) (int, int, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, 0, false
	}
	sep := strings.IndexAny(s, "x*×")
	if sep < 0 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(s[:sep]))
	h, err2 := strconv.Atoi(strings.TrimSpace(s[sep+1:]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return w, h, true
}

// resolveV2Resolution 归一分辨率。**越界不回落默认值**，直接报错。
func resolveV2Resolution(req *relaycommon.TaskSubmitReq, spec V2ModelSpec) (string, error) {
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" && strings.TrimSpace(req.Size) != "" {
		if r, ok := resolutionFromSizeV2(req.Size); ok {
			raw = r
		}
	}
	if raw == "" {
		return spec.DefaultRes, nil
	}
	up := strings.ToUpper(strings.TrimSpace(raw))
	for _, allowed := range spec.Resolutions {
		if up == allowed {
			return up, nil
		}
	}
	return "", fmt.Errorf("resolution must be one of %s for %s, got %s",
		strings.Join(spec.Resolutions, "/"), spec.Name, raw)
}

func resolutionFromSizeV2(size string) (string, bool) {
	w, h, ok := parseWH(size)
	if !ok {
		return "", false
	}
	short := w
	if h < short {
		short = h
	}
	switch {
	case short >= 2000:
		return Resolution2K, true
	case short >= 1000:
		return "1080P", true
	case short >= 700:
		return Resolution768P, true
	case short >= 500:
		return Resolution480P, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// content[] 组装
// ---------------------------------------------------------------------------

func buildV2Content(req *relaycommon.TaskSubmitReq) []V2ContentItem {
	items := make([]V2ContentItem, 0, 8)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		items = append(items, V2ContentItem{Type: "text", Text: prompt})
	}
	for _, m := range collectV2Media(req) {
		switch m.kind {
		case "video":
			items = append(items, V2ContentItem{Type: "video_url", VideoURL: m.url, Role: m.role})
		case "audio":
			items = append(items, V2ContentItem{Type: "audio_url", AudioURL: m.url, Role: m.role})
		default:
			items = append(items, V2ContentItem{Type: "image_url", ImageURL: m.url, Role: m.role})
		}
	}
	return items
}

// ---------------------------------------------------------------------------
// 产物尺寸回填
// ---------------------------------------------------------------------------

// v2SizeFor 产出下游 `size` 字段用的 WxH。取不到精确像素时用比例构造
// 「看起来合理」的尺寸：宁可给比例一致的近似值，也不回一个与成片不符的尺寸。
func v2SizeFor(task *V2Task, fallbackRatio string) string {
	if task == nil {
		return ""
	}
	if size := sizeForResolution(task.Resolution, task.Ratio); size != "" {
		return size
	}
	ratio := firstNonEmpty(task.Ratio, fallbackRatio)
	if ratio == "" || ratio == "adaptive" {
		return ""
	}
	return sizeForResolution("", ratio)
}

// sizeForResolution 由分辨率高度与比例推出 WxH。高度就是分辨率档位，
// 宽度按比例反推（16:9 @768P → 1366x768）。
func sizeForResolution(resolution, ratio string) string {
	if ratio == "" || ratio == "adaptive" {
		return ""
	}
	rw, rh, ok := parseRatio(ratio)
	if !ok {
		return ""
	}
	var height int
	switch strings.ToUpper(strings.TrimSpace(resolution)) {
	case Resolution2K:
		height = 1440
	case "1080P":
		height = 1080
	case Resolution768P:
		height = 768
	case Resolution480P:
		height = 480
	default:
		return ""
	}
	width := height * rw / rh
	if width <= 0 {
		return ""
	}
	// 偶数宽高对 H.264 更友好。
	if width%2 != 0 {
		width++
	}
	return fmt.Sprintf("%dx%d", width, height)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
