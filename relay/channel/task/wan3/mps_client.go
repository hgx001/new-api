package wan3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ─────────────────────────────────────────────────────────────────────────────
// 腾讯云 MPS（媒体处理）客户端 —— 手写实现，不引入腾讯云 SDK。
//
// 为什么不引 SDK：
//   · SDK 体积大（models.go 有 3 万多行），只为了两个接口不值当；
//   · 引入 SDK 会把 SecretId/SecretKey 的用法散落到各处，不如集中在这一个文件里。
//
// 权威参考：tencentcloud/mps/v20190612（client.go 里 APIVersion = "2019-06-12"）。
// ⚠️ 云文档站的版本号每次抓取都不一样，**以 SDK 源码为准**。
//
// ─────────────────────────────────────────────────────────────────────────────

const (
	// mpsService 腾讯云 API 服务名。
	mpsService = "mps"
	// mpsHost 腾讯云 MPS 接入点。
	mpsHost = "mps.tencentcloudapi.com"
	// mpsAPIVersion MPS API 版本（唯一版本目录 v20190612）。
	mpsAPIVersion = "2019-06-12"
	// mpsRegion 服务地域，必须与 COS 桶同区。
	mpsRegion = "ap-guangzhou"
)

// mpsActionProcessMedia 提交媒体处理任务。
const mpsActionProcessMedia = "ProcessMedia"

// mpsActionDescribeTaskDetail 查询任务详情。
const mpsActionDescribeTaskDetail = "DescribeTaskDetail"

// mpsRequestTimeout 单次 HTTP 请求超时。
//
// 注意：MPS 的 ProcessMedia 是**异步**的 —— 提交只返回 TaskId，
// 处理时长由后续轮询决定。所以这个超时只覆盖「提交/查询」的往返。
const mpsRequestTimeout = 30 * time.Second

// mpsClient 是 MPS API 的最小客户端。
type mpsClient struct {
	secretID  string
	secretKey string
	region    string
	http      *http.Client
}

// newMPSClient 构造一个 MPS 客户端。
func newMPSClient(secretID, secretKey, region string) *mpsClient {
	if region == "" {
		region = mpsRegion
	}
	return &mpsClient{
		secretID:  secretID,
		secretKey: secretKey,
		region:    region,
		http:      &http.Client{Timeout: mpsRequestTimeout},
	}
}

// mpsAPIError 是腾讯云返回的接口错误。
type mpsAPIError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
	RequestID string `json:"RequestId"`
}

func (e *mpsAPIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("tencent mps error %s: %s (requestId=%s)", e.Code, e.Message, e.RequestID)
}

// mpsResponse 是腾讯云统一响应信封。
type mpsResponse struct {
	Response struct {
		Error *mpsAPIError `json:"Error"`
		// ProcessMedia
		TaskId string `json:"TaskId"`
		// DescribeTaskDetail：Status / Progress 在**顶层**，不在嵌套任务体里
		// （TaskDetail 只在 ScheduleTask 类型任务下存在）。
		Status   string `json:"Status"`
		Progress string `json:"Progress"`
		ErrCode  string `json:"ErrCode"`
		// 嵌套任务体：WorkflowTask（ProcessMedia 产出）/ ScheduleTask。
		WorkflowTask *mpsTaskBody `json:"WorkflowTask"`
		ScheduleTask *mpsTaskBody `json:"ScheduleTask"`
	} `json:"Response"`
}

// mpsTaskBody 是任务体，承载输入输出信息。
//
// ⚠️ 产物路径**不在** TranscodeTaskSet 里 —— 提交体是 TranscodeTaskSet，
// 但查询结果里产物挂在 WorkflowTask.MediaProcessResultSet[].TranscodeTask.Output.Path。
type mpsTaskBody struct {
	Status   string `json:"Status"`
	Progress string `json:"Progress"`
	ErrCode  string `json:"ErrCode"`
	Message  string `json:"Message"`
	InputInfo struct {
		Type string `json:"Type"`
	} `json:"InputInfo"`
	OutputStorage struct {
		Type string `json:"Type"`
	} `json:"OutputStorage"`
	// MediaProcessResultSet 是查询结果里的产物集合。
	MediaProcessResultSet []mpsProcessTaskResult `json:"MediaProcessResultSet"`
	// TranscodeTaskSet 是提交体里的任务数组（查询结果一般不返回，保留兜底）。
	TranscodeTaskSet []mpsTranscodeTask `json:"TranscodeTaskSet"`
}

// mpsProcessTaskResult 是单个处理任务的结果。
type mpsProcessTaskResult struct {
	Type string `json:"Type"`
	// TranscodeTask 是转码结果，产物路径在 Output.Path。
	TranscodeTask *mpsTranscodeTask `json:"TranscodeTask"`
}

// mpsTranscodeTask 是转码任务项，输出路径在 Output.Path。
type mpsTranscodeTask struct {
	Status   string `json:"Status"`
	ErrCode  string `json:"ErrCodeExt"`
	Message  string `json:"Message"`
	Progress int64  `json:"Progress"`
	Input    *struct {
		Object string `json:"Object"`
	} `json:"Input"`
	Output *struct {
		// Path 是 COS 对象路径（**不是** ObjectPath —— 那是另一类任务的字段）。
		Path       string `json:"Path"`
		ObjectPath string `json:"ObjectPath"`
		Bucket     string `json:"Bucket"`
		Region     string `json:"Region"`
		Width      int64  `json:"Width"`
		Height     int64  `json:"Height"`
	} `json:"Output"`
}

// outputPath 返回输出对象的 COS 路径。
func (t *mpsTranscodeTask) outputPath() string {
	if t == nil || t.Output == nil {
		return ""
	}
	if t.Output.Path != "" {
		return t.Output.Path
	}
	return t.Output.ObjectPath
}

// call 发起一次腾讯云 API 调用。
//
// 参数顺序 ⚠️：腾讯云官方 hmacsha256 的签名是 **message 在前、key 在后**
// （hmacsha256(message, key)），与 Go 标准库的 hmac.New(key, message) 相反。
// 写反了会一直报 SignatureDoesNotMatch，且报错信息不提示顺序问题。
func (c *mpsClient) call(ctx context.Context, action string, payload map[string]any) (*mpsResponse, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload failed: %w", action, err)
	}

	endpoint := "https://" + mpsHost
	now := time.Now().UTC()
	date := now.Format("2006-01-02")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new %s request failed: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", mpsHost)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", mpsAPIVersion)
	req.Header.Set("X-TC-Region", c.region)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", now.Unix()))
	req.Header.Set("Authorization", c.authorization(date, action, body))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", action, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s response failed: %w", action, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned http %d: %s", action, resp.StatusCode, truncateBody(respBody))
	}

	var parsed mpsResponse
	if err := common.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal %s response failed: %w", action, err)
	}
	if parsed.Response.Error != nil {
		return nil, parsed.Response.Error
	}
	return &parsed, nil
}

// authorization 生成 TC3-HMAC-SHA256 签名头。
//
// 派生链：secretDate = hmacsha256(date, "TC3"+secKey)
//	      → secretService = hmacsha256(service, secretDate)
//	      → secretKey = hmacsha256("tc3_request", secretService)
// 签名串 = "TC3-HMAC-SHA256\n" + timestamp + "\n" + date/service/tc3_request
//	      + "\n" + sha256Hex(body)
func (c *mpsClient) authorization(date, action string, body []byte) string {
	secretDate := hmacSHA256([]byte(date), []byte("TC3"+c.secretKey))
	secretService := hmacSHA256([]byte(mpsService), secretDate)
	secretKey := hmacSHA256([]byte("tc3_request"), secretService)

	timestamp := fmt.Sprintf("%d", time.Now().UTC().Unix())
	signedHeaders := "content-type;host"
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", "application/json", mpsHost)
	hashedPayload := sha256Hex(body)

	canonicalRequest := strings.Join([]string{
		"POST", "/", "", canonicalHeaders, signedHeaders, hashedPayload,
	}, "\n")

	credentialScope := strings.Join([]string{date, mpsService, "tc3_request"}, "/")
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256", timestamp, credentialScope, sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256([]byte(stringToSign), secretKey))
	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.secretID, credentialScope, signedHeaders, signature)
}

// hmacSHA256 返回 HMAC-SHA256 计算结果。
//
// ⚠️ 参数顺序是 **(message, key)** —— 与腾讯云官方一致，与 Go 标准库相反。
func hmacSHA256(message, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	return mac.Sum(nil)
}

// sha256Hex 返回内容的十六进制 SHA256。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// truncateBody 截断响应体，避免日志里塞进超长内容。
func truncateBody(body []byte) string {
	const limit = 300
	s := strings.TrimSpace(string(body))
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// ─────────────────────────────────────────────────────────────────────────────
// 提交与轮询
// ─────────────────────────────────────────────────────────────────────────────

// mpsSuperResRequest 是一次超分任务的输入参数。
type mpsSuperResRequest struct {
	// InputURL 官网生成的成片直链（OSS 签名 URL）。
	//
	// 用 Type=URL 直链输入，**不需要先把成片下载再传 COS** ——
	// 省掉一次上传 + 一次下载，也省掉一次落盘。
	InputURL string
	// Bucket / Region 产物落地的 COS 桶与地域。
	Bucket string
	Region string
	// OutputDir 输出目录（单层，形如 /mps_api_test/）。
	OutputDir string
	// Width / Height 输出分辨率（2 倍超分）。
	Width  int
	Height int
	// BitrateKbps 输出码率。
	BitrateKbps int
	// FPS 输出帧率，0 表示不指定。
	FPS int
	// SuperResolutionType 算法档位：lq / hq。
	SuperResolutionType string
	// SessionID 会话 ID。
	SessionID string
}

// mpsSuperResResult 是一次超分任务的结果。
type mpsSuperResResult struct {
	// TaskID 腾讯云侧任务 ID。
	TaskID string
	// Status 终态：SUCCESS / FAIL。
	Status string
	// ObjectPath 产物在 COS 中的对象路径。
	ObjectPath string
	// ErrCode 失败原因。
	ErrCode string
}

// submitSuperRes 提交一个 2 倍超分任务，返回腾讯云 TaskId。
//
// 请求体有 5 个坑（全部实测踩过）：
//  1. MediaProcessTask 下面是 **TranscodeTaskSet（数组）**，不是 TranscodeTask；
//  2. Type 值**大小写敏感**：URL / COS 必须大写，写成 Cos 报
//     "InputInfo.Type(Cos) not supported"；
//  3. OutputDir 只能是**目录**（以 / 开头结尾），且只能是单层；
//  4. Definition=0 时走 RawParameter.EnhanceConfig.VideoEnhance.SuperResolution；
//  5. 超分与大模型增强**互斥**，同时配会失败。
func (c *mpsClient) submitSuperRes(ctx context.Context, req mpsSuperResRequest) (string, error) {
	superResType := req.SuperResolutionType
	if superResType != "hq" {
		superResType = "lq"
	}
	bitrate := req.BitrateKbps
	if bitrate <= 0 {
		bitrate = 5000
	}
	fps := req.FPS
	if fps <= 0 {
		fps = 30
	}

	videoTemplate := map[string]any{
		"Codec":   "h264",
		"Fps":     fps,
		"Bitrate": bitrate,
		"Width":   req.Width,
		"Height":  req.Height,
		"Gop":     60,
		// ⚠️ FillType 必须用 black，不能用 stretch。
		//
		// stretch 的语义是「拉伸每一帧填满画面」—— 宽高比不一致时画面会被压扁/拉长。
		// 官网成片可能是 16:9 / 9:16 / 1:1 等各种比例，固定宽高 + stretch 会让
		// 竖屏片变成胖画面。
		//
		// black = 保持原始宽高比，边缘留黑边；配合 ResolutionAdaptive=open
		// （按输入宽高自动适配，Width/Height 作为上限），任何比例都正确。
		"FillType": "black",
		// ResolutionAdaptive=open：按输入视频宽高比自适应，
		// Width/Height 表示「不超过的最大尺寸」而不是「强制尺寸」。
		"ResolutionAdaptive": "open",
	}

	payload := map[string]any{
		"InputInfo": map[string]any{
			// ⚠️ 大写 "URL"
			"Type": "URL",
			"UrlInputInfo": map[string]any{
				"Url": req.InputURL,
			},
		},
		"OutputStorage": map[string]any{
			// ⚠️ 大写 "COS"，且字段名是 Bucket / Region（不是 CosBucketName / CosRegion）
			"Type": "COS",
			"CosOutputStorage": map[string]any{
				"Bucket": req.Bucket,
				"Region": req.Region,
			},
		},
		"OutputDir": req.OutputDir,
		"MediaProcessTask": map[string]any{
			// ⚠️ 是数组
			"TranscodeTaskSet": []any{
				map[string]any{
					"Definition": 0,
					"RawParameter": map[string]any{
						"Container":    "mp4",
						"VideoTemplate": videoTemplate,
						"AudioTemplate": map[string]any{
							"Codec":        "aac",
							"Bitrate":      64,
							"SampleRate":   44100,
							"AudioChannel": 2,
						},
						"EnhanceConfig": map[string]any{
							"VideoEnhance": map[string]any{
								// ⚠️ 超分与大模型增强互斥，这里只配超分
								"SuperResolution": map[string]any{
									"Switch": "ON",
									"Type":   superResType,
									"Size":   2,
								},
							},
						},
					},
				},
			},
		},
		"SessionId": req.SessionID,
	}

	resp, err := c.call(ctx, mpsActionProcessMedia, payload)
	if err != nil {
		return "", err
	}
	if resp.Response.TaskId == "" {
		return "", fmt.Errorf("mps ProcessMedia returned empty TaskId")
	}
	return resp.Response.TaskId, nil
}

// describeSuperRes 查询超分任务状态。
func (c *mpsClient) describeSuperRes(ctx context.Context, taskID string) (*mpsSuperResResult, error) {
	resp, err := c.call(ctx, mpsActionDescribeTaskDetail, map[string]any{
		"TaskId": taskID,
	})
	if err != nil {
		return nil, err
	}

	result := &mpsSuperResResult{
		TaskID:  taskID,
		Status:  resp.Response.Status,
		ErrCode: resp.Response.ErrCode,
	}
	// Status 在**响应顶层**；兜底从嵌套任务体里找。
	if result.Status == "" {
		for _, body := range []*mpsTaskBody{resp.Response.WorkflowTask, resp.Response.ScheduleTask} {
			if body != nil && body.Status != "" {
				result.Status = body.Status
				break
			}
		}
	}
	// 产物路径：WorkflowTask.MediaProcessResultSet[].TranscodeTask.Output.Path
	for _, body := range []*mpsTaskBody{resp.Response.WorkflowTask, resp.Response.ScheduleTask} {
		if body == nil {
			continue
		}
		for _, mr := range body.MediaProcessResultSet {
			if mr.TranscodeTask == nil {
				continue
			}
			if result.ErrCode == "" && mr.TranscodeTask.ErrCode != "" {
				result.ErrCode = mr.TranscodeTask.ErrCode
			}
			if p := mr.TranscodeTask.outputPath(); p != "" {
				result.ObjectPath = p
				break
			}
		}
		if result.ObjectPath != "" {
			break
		}
		// 兜底：万一腾讯云把产物挂在提交体的任务数组上
		for i := range body.TranscodeTaskSet {
			if p := body.TranscodeTaskSet[i].outputPath(); p != "" {
				result.ObjectPath = p
				break
			}
		}
		if result.ObjectPath != "" {
			break
		}
	}
	return result, nil
}

// sortedKeys 返回有序的 map key 列表（调试与测试用）。
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
