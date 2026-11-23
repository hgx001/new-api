package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

type Pricing struct {
	ModelName   string `json:"model_name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Tags        string `json:"tags,omitempty"`
	VendorID    int    `json:"vendor_id,omitempty"`
	// SortWeight 是模型广场排序权重（数值越大越靠前，0 = 不置顶）。
	// 由 models 表携带，前端按它排序；不再在前端维护置顶名单。
	SortWeight             int                     `json:"sort_weight"`
	QuotaType              int                     `json:"quota_type"`
	ModelRatio             float64                 `json:"model_ratio"`
	ModelPrice             float64                 `json:"model_price"`
	OwnerBy                string                  `json:"owner_by"`
	CompletionRatio        float64                 `json:"completion_ratio"`
	CacheRatio             *float64                `json:"cache_ratio,omitempty"`
	CreateCacheRatio       *float64                `json:"create_cache_ratio,omitempty"`
	ImageRatio             *float64                `json:"image_ratio,omitempty"`
	AudioRatio             *float64                `json:"audio_ratio,omitempty"`
	AudioCompletionRatio   *float64                `json:"audio_completion_ratio,omitempty"`
	EnableGroup            []string                `json:"enable_groups"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	BillingMode            string                  `json:"billing_mode,omitempty"`
	BillingExpr            string                  `json:"billing_expr,omitempty"`
	PricingVersion         string                  `json:"pricing_version,omitempty"`
}

type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

var (
	pricingMap           []Pricing
	vendorsList          []PricingVendor
	supportedEndpointMap map[string]common.EndpointInfo
	lastGetPricingTime   time.Time
	updatePricingLock    sync.Mutex

	// 缓存映射：模型名 -> 启用分组 / 计费类型
	modelEnableGroups     = make(map[string][]string)
	modelQuotaTypeMap     = make(map[string]int)
	modelEnableGroupsLock = sync.RWMutex{}
)

var (
	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	modelSupportEndpointsLock = sync.RWMutex{}
)

func GetPricing() []Pricing {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		updatePricingLock.Lock()
		defer updatePricingLock.Unlock()
		// Double check after acquiring the lock
		if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
			modelSupportEndpointsLock.Lock()
			defer modelSupportEndpointsLock.Unlock()
			updatePricing()
		}
	}
	return pricingMap
}

func InvalidatePricingCache() {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()

	pricingMap = nil
	vendorsList = nil
	lastGetPricingTime = time.Time{}
}

// GetVendors 返回当前定价接口使用到的供应商信息
func GetVendors() []PricingVendor {
	if time.Since(lastGetPricingTime) > time.Minute*1 || len(pricingMap) == 0 {
		// 保证先刷新一次
		GetPricing()
	}
	return vendorsList
}

func GetModelSupportEndpointTypes(model string) []constant.EndpointType {
	if model == "" {
		return make([]constant.EndpointType, 0)
	}
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	return make([]constant.EndpointType, 0)
}

func updatePricing() {
	//modelRatios := common.GetModelRatios()
	enableAbilities, err := GetAllEnableAbilityWithChannels()
	if err != nil {
		common.SysLog(fmt.Sprintf("GetAllEnableAbilityWithChannels error: %v", err))
		return
	}
	// 预加载模型元数据与供应商一次，避免循环查询
	var allMeta []Model
	_ = DB.Find(&allMeta).Error
	metaMap := make(map[string]*Model)
	prefixList := make([]*Model, 0)
	suffixList := make([]*Model, 0)
	containsList := make([]*Model, 0)
	for i := range allMeta {
		m := &allMeta[i]
		if m.NameRule == NameRuleExact {
			metaMap[m.ModelName] = m
		} else {
			switch m.NameRule {
			case NameRulePrefix:
				prefixList = append(prefixList, m)
			case NameRuleSuffix:
				suffixList = append(suffixList, m)
			case NameRuleContains:
				containsList = append(containsList, m)
			}
		}
	}

	// 将非精确规则模型匹配到 metaMap
	for _, m := range prefixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasPrefix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range suffixList {
		for _, pricingModel := range enableAbilities {
			if strings.HasSuffix(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}
	for _, m := range containsList {
		for _, pricingModel := range enableAbilities {
			if strings.Contains(pricingModel.Model, m.ModelName) {
				if _, exists := metaMap[pricingModel.Model]; !exists {
					metaMap[pricingModel.Model] = m
				}
			}
		}
	}

	// 预加载供应商
	var vendors []Vendor
	_ = DB.Find(&vendors).Error
	vendorMap := make(map[int]*Vendor)
	for i := range vendors {
		vendorMap[vendors[i].Id] = &vendors[i]
	}

	// 初始化默认供应商映射
	initDefaultVendorMapping(metaMap, vendorMap, enableAbilities)

	// 构建对前端友好的供应商列表
	vendorsList = make([]PricingVendor, 0, len(vendorMap))
	for _, v := range vendorMap {
		vendorsList = append(vendorsList, PricingVendor{
			ID:          v.Id,
			Name:        v.Name,
			Description: v.Description,
			Icon:        v.Icon,
		})
	}

	modelGroupsMap := make(map[string]*types.Set[string])

	for _, ability := range enableAbilities {
		groups, ok := modelGroupsMap[ability.Model]
		if !ok {
			groups = types.NewSet[string]()
			modelGroupsMap[ability.Model] = groups
		}
		groups.Add(ability.Group)
	}

	//这里使用切片而不是Set，因为一个模型可能支持多个端点类型，并且第一个端点是优先使用端点
	modelSupportEndpointsStr := make(map[string][]string)

	// 先根据已有能力填充原生端点
	for _, ability := range enableAbilities {
		endpoints := modelSupportEndpointsStr[ability.Model]
		channelTypes := common.GetEndpointTypesByChannelType(ability.ChannelType, ability.Model)
		for _, channelType := range channelTypes {
			if !common.StringsContains(endpoints, string(channelType)) {
				endpoints = append(endpoints, string(channelType))
			}
		}
		modelSupportEndpointsStr[ability.Model] = endpoints
	}

	// 再补充模型自定义端点：若配置有效则替换默认端点，不做合并
	for modelName, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			endpoints := make([]string, 0, len(raw))
			for k, v := range raw {
				switch v.(type) {
				case string, map[string]interface{}:
					if !common.StringsContains(endpoints, k) {
						endpoints = append(endpoints, k)
					}
				}
			}
			if len(endpoints) > 0 {
				modelSupportEndpointsStr[modelName] = endpoints
			}
		}
	}

	modelSupportEndpointTypes = make(map[string][]constant.EndpointType)
	for model, endpoints := range modelSupportEndpointsStr {
		supportedEndpoints := make([]constant.EndpointType, 0)
		for _, endpointStr := range endpoints {
			endpointType := constant.EndpointType(endpointStr)
			supportedEndpoints = append(supportedEndpoints, endpointType)
		}
		modelSupportEndpointTypes[model] = supportedEndpoints
	}

	// 构建全局 supportedEndpointMap（默认 + 自定义覆盖）
	supportedEndpointMap = make(map[string]common.EndpointInfo)
	// 1. 默认端点
	for _, endpoints := range modelSupportEndpointTypes {
		for _, et := range endpoints {
			if info, ok := common.GetDefaultEndpointInfo(et); ok {
				if _, exists := supportedEndpointMap[string(et)]; !exists {
					supportedEndpointMap[string(et)] = info
				}
			}
		}
	}
	// 2. 自定义端点（models 表）覆盖默认
	for _, meta := range metaMap {
		if strings.TrimSpace(meta.Endpoints) == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(meta.Endpoints), &raw); err == nil {
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					supportedEndpointMap[k] = common.EndpointInfo{Path: val, Method: "POST"}
				case map[string]interface{}:
					ep := common.EndpointInfo{Method: "POST"}
					if p, ok := val["path"].(string); ok {
						ep.Path = p
					}
					if m, ok := val["method"].(string); ok {
						ep.Method = strings.ToUpper(m)
					}
					supportedEndpointMap[k] = ep
				default:
					// ignore unsupported types
				}
			}
		}
	}

	pricingMap = make([]Pricing, 0)
	for model, groups := range modelGroupsMap {
		if !isAllowedPricingModel(model) {
			continue
		}
		pricing := Pricing{
			ModelName:              model,
			EnableGroup:            groups.Items(),
			SupportedEndpointTypes: modelSupportEndpointTypes[model],
		}

		// 补充模型元数据（描述、标签、供应商、状态）
		if meta, ok := metaMap[model]; ok {
			// 若模型被禁用(status!=1)，则直接跳过，不返回给前端
			if meta.Status != 1 {
				continue
			}
			pricing.Description = meta.Description
			pricing.Icon = meta.Icon
			pricing.Tags = meta.Tags
			pricing.VendorID = meta.VendorID
			pricing.SortWeight = meta.SortWeight
		}
		modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
		if findPrice {
			pricing.ModelPrice = modelPrice
			pricing.QuotaType = 1
		} else {
			modelRatio, _, _ := ratio_setting.GetModelRatio(model)
			pricing.ModelRatio = modelRatio
			pricing.CompletionRatio = ratio_setting.GetCompletionRatio(model)
			pricing.QuotaType = 0
		}
		if cacheRatio, ok := ratio_setting.GetCacheRatio(model); ok {
			pricing.CacheRatio = &cacheRatio
		}
		if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(model); ok {
			pricing.CreateCacheRatio = &createCacheRatio
		}
		if imageRatio, ok := ratio_setting.GetImageRatio(model); ok {
			pricing.ImageRatio = &imageRatio
		}
		if ratio_setting.ContainsAudioRatio(model) {
			audioRatio := ratio_setting.GetAudioRatio(model)
			pricing.AudioRatio = &audioRatio
		}
		if ratio_setting.ContainsAudioCompletionRatio(model) {
			audioCompletionRatio := ratio_setting.GetAudioCompletionRatio(model)
			pricing.AudioCompletionRatio = &audioCompletionRatio
		}
		if billingMode := billing_setting.GetBillingMode(model); billingMode == "tiered_expr" {
			if expr, ok := billing_setting.GetBillingExpr(model); ok && strings.TrimSpace(expr) != "" {
				pricing.BillingMode = billingMode
				pricing.BillingExpr = expr
			}
		}
		pricingMap = append(pricingMap, pricing)
	}

	// 防止大更新后数据不通用
	if len(pricingMap) > 0 {
		pricingMap[0].PricingVersion = "5a90f2b86c08bd983a9a2e6d66c255f4eaef9c4bc934386d2b6ae84ef0ff1f1f"
	}

	// 刷新缓存映射，供高并发快速查询
	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range pricingMap {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()

	lastGetPricingTime = time.Now()
}

func isAllowedPricingModel(name string) bool {
	// 文本模型：只保留 gpt-6 系列 + MiniMax M3 家族
	if strings.HasPrefix(name, "gpt-6") {
		return true
	}
	if strings.EqualFold(name, "MiniMax-M3") || strings.EqualFold(name, "MiniMax-M3.1") {
		return true
	}
	// 二春(erchun) wan3.0 仅开放 480P 的对外模型名
	if name == "wan3.0-480p" {
		return true
	}
	// Youkou 直连文本模型
	if name == "deepseek-v4-flash" {
		return true
	}
	if name == "deepseek-v4-flash-vision-exp" {
		return true
	}
	if name == "deepseek-v4-pro" {
		return true
	}
	// 图像模型：gpt-image-2/2.5 + youkou 图像（doubao / gemini）
	if name == "gpt-image-2" {
		return true
	}
	if name == "gpt-image-2.5" {
		return true
	}
	// 官方渠道图片模型：上游同为 gpt-image-2，独立命名以区分二手图片渠道
	if name == "gpt-image-2.5-官方" {
		return true
	}
	// h3-image（2026-11-13 上架）：GPU 服务器 MiniMax H3-Image 文生图，
	// 同步 POST /v1/images/generations，按张 ¥0.05/张。对外只放行这个
	// 公开名；上游兼容 id qwen_image_21 只存在于渠道 model_mapping，
	// **不上广场**（客户侧不能出现 qwen 字样）。
	if name == "h3-image" {
		return true
	}
	if name == "doubao-seedream-5-0-260128" {
		return true
	}
	if name == "gemini-3-pro-image-preview" {
		return true
	}
	if name == "gemini-3.1-flash-image-preview" {
		return true
	}
	// 视频模型：全部上游模型均展示
	if name == "aliyun:wan-3.0" {
		return true
	}
	if strings.HasPrefix(name, "hailuo-h3") {
		return true
	}
	// MiniMax 官方 v2 视频（H3 / H3 Max）：按秒 + 分辨率倍率计价，上广场。
	if name == "MiniMax-H3" || name == "MiniMax-H3-Max" {
		return true
	}
	if name == "seedance2.0特惠版" {
		return true
	}
	if name == "seedance2.5特惠版" {
		return true
	}
	if strings.HasPrefix(name, "sd2.5") {
		return true
	}
	if name == "sd-2.5" {
		return true
	}
	// MegaAI 渠道（newapi.megabyai.cc）2026-11-23 重新接入后上架的 T3-seedance-2.5：
	// 按次计费 + 分辨率分档（对外 480p ¥4 / 720p ¥5），4-30 秒、全比例、
	// 参考图 ≤30 / 视频 ≤10 / 音频 ≤10。
	// 精确匹配：站内还有 seedance-2-5 / seedance-2.5 / T2-seedance-2.0 等相近名，
	// 前缀匹配会误命中别家商品。
	if name == "T3-seedance-2.5" {
		return true
	}
	// 有赞渠道的新独立模型（2026-11-18 上架，公开名 **wan-3.0-yz**）：
	// 上游独立模型 ID 是 wan-3.0，按秒计费 ¥0.20/秒（720P）/ ¥0.25/秒（1080P），
	// 时长 10-30 秒逐秒可选。
	//
	// ⚠️ 这个公开名**曾经**是 "wan-3.0"（2026-11-18 改名为 wan-3.0-yz）。
	// "wan-3.0" 那个名字先后属于过 MegaAI 渠道（已停售，代码侧已改名归档为
	// "wan-3.0-megabyai-retired"），现在它只是**上游 ID**，不是任何商品的公开名。
	// 精确匹配，不会与下面 wan-3.0-pro / wan-3.0-特惠 互相命中。
	if name == "wan-3.0-yz" {
		return true
	}
	// 有赞渠道的 wan-3.0-pro（2026-11-18 上架）：上游独立模型，按条计费
	// 720P ¥4.5、1080P ¥5.5；时长 4-30 秒、画幅仅 16:9 / 9:16。
	// 精确匹配，不会与上面有赞的 wan-3.0-yz 互相命中。
	if name == "wan-3.0-pro" {
		return true
	}
	// 有赞渠道的 hk-wan3（2026-11-19 上架，2026-11-20 补广场与描述）：
	// WAN3 官渠满血满参，按秒计费 480P ¥0.18 / 720P ¥0.25 / 1080P ¥0.48
	// （基准档 480P），时长 4-30 秒。参考视频**不额外收费**，只按生成秒数收。
	// 精确匹配，不与上面的 wan-3.0-yz / wan-3.0-pro 互相命中。
	if name == "hk-wan3" {
		return true
	}
	// 有赞 Wan3 的对外别名：客户看到 seedance2.5-特惠，实际由 wan3.0-video-prime
	// 执行（2026-10-29 起）。此前它挂在 MegaAI 渠道按次卖 ¥5.0/条，2026-10-26
	// 随该渠道停售而空置。现计费为**独立定价**：按秒 720P ¥0.20 / 1080P ¥0.30，
	// 不跟随 wan3.0-video-prime（2026-10-29 老板明确）。
	// 所以它必须显示「/秒」而不是「/次」（见前端 isPerSecondModel）。
	if name == "seedance2.5-特惠" {
		return true
	}
	// seedance2.5-活动（2026-11-02 上架）：seedance2.5 促销族的**按次档**，
	// 与母本 seedance2.5-特惠 同货源同链路，只是定价口径不同
	// （¥4.5/条，与时长无关；2026-11-11 起支持 2-30 秒）。
	//
	// ⚠️ 判定走**能力组**（constant.IsSeedance25PerCall），不要在这里手写模型名 ——
	// 能力组是「谁是这一族 + 谁按次计费」的唯一来源；将来促销族再加按次档位，
	// 这里自动生效，不会出现「加了模型忘了上广场」。
	//
	// ⚠️ 母本刻意**不**用能力组判定：母本是**按秒**档，与按次档必须分开 ——
	// 前端的「/秒 vs /次」显示依赖这个区分（见 web 的 isPerSecondModel）。
	// 母本走上面的精确匹配，活动版走下面的能力组，**互不干扰**。
	if constant.IsSeedance25PerCall(name) {
		return true
	}
	if strings.HasPrefix(name, "xuan:") {
		return true
	}
	// wan3 系：官网标准版（wan3.0-video-官网）与有赞两个在售模型
	// （wan3.0-video / wan3.0-video-prime，2026-10-28 起与上游命名一致）共用该前缀。
	// ⚠️ 旧名必须先拦掉再走前缀，否则 wan3.0-video-prime-1080p 会被前缀吃掉：
	// 两者仍留在渠道 17 的 models 里，只为让历史客户的请求拿到可读错误
	// （可调用、不上广场）。渠道一旦被编辑，UpdateAbilities 会按 channels.models
	// 重新生成能力行 —— 白名单若放行，它们就会重新出现在广场，与「广场命名
	// 与上游一致」的约定冲突。wan2.7-r2v 则已被上游下架。
	switch name {
	case "wan3.0-smart", "wan3.0-video-prime-1080p", "wan2.7-r2v":
		return false
	}
	// wan3.0-720p-特惠（2026-10-29 上架，2026-10-30 改名）：官网渠道的 720P 成片档。
	// ⚠️ 必须显式放行 —— 它**不以 "wan3.0-video" 开头**（是 "wan3.0-720p-"），
	// 下面的前缀判断拦不住它，不加就会在广场里看不到、也算不出价。
	//
	// ⚠️ 旧名 `wan3.0-720p-官网` **故意不在这里**（过渡期只保可调用）：
	// 广场是「客户挑模型」的界面，同一份货挂两个名字会让客户不知道该用哪个，
	// 也会让「广场命名」与上游对齐的约定破功。旧名仍需配 ModelPrice，
	// 否则旧名调用会算不出价（见 setting/ratio_setting/model_ratio.go）。
	if name == "wan3.0-720p-特惠" {
		return true
	}
	// wan3.0-720p-按次（2026-11-01 上架）：心笔 Model Router 渠道的 720P 成片档。
	// 同样以 "wan3.0-720p-" 开头、不以 "wan3.0-video" 开头，必须显式放行。
	// 与特惠的区别只在计费口径（按次 ¥4.5/次 vs 按秒 ¥0.20/秒），链路相同。
	if name == "wan3.0-720p-按次" {
		return true
	}
	if strings.HasPrefix(name, "wan3.0-video") {
		return true
	}
	// 客美 Kemeitv（渠道 71）的两个万相特价变体（2026-11-19 上架）：
	// wan3.0-video-tj / wan3.0-video-prime-tj。
	//
	// ⚠️ 它们**本来就被上面的 "wan3.0-video" 前缀判断放行**，
	//	这里再显式写一遍是**回归守卫**：万一后人收紧前缀判断（比如为了
	//	挡掉某个重名模型），这两个新上架的模型会静默从广场消失 ——
	//	客户看不到、也不算得出价，而渠道与 abilities 都在，是最难查的一类故障。
	//
	// 与站内已有 wan3.0-video / wan3.0-video-prime 是**两条独立货源**，
	// 对外价按同价位对齐（见 setting/ratio_setting/model_ratio.go）。
	switch name {
	case "wan3.0-video-tj", "wan3.0-video-prime-tj":
		return true
	}
	// 三元巡狩 wanwan 1080P（拼车）慢速档：按次计费，次日出片
	if name == "wan3.0-1080p-慢速" {
		return true
	}
	// wan3.0-班车（2026-11-04 上架，原名「wan3.0-排队」）：视频生成车站排队通道
	// 的万相 wan3 全模态，攒批发车。同样以 "wan3.0-" 开头、不以 "wan3.0-video"
	// 开头，必须显式放行，否则广场里看不到、也算不出价。
	//
	// ⚠️ 旧名「wan3.0-排队」**故意不在这里**：改名时生产库尚无该模型的渠道与
	// 能力记录，不存在旧名调用方，广场上不该同时挂两个名字。
	if name == "wan3.0-班车" {
		return true
	}
	// AutoDL ComfyUI：全部已配置工作流（autodl: 前缀）
	if strings.HasPrefix(name, "autodl:") {
		return true
	}
	// SeetaCloud（2026-11-06 接入；2026-11-08 V2 退役）：自建 ComfyUI 实例的
	// ArcReel-H3 网关，对外只留 minimax_h3_omni（多模态参考 9 图 + 3 视频 +
	// 3 音频 + 1080p）。V2 与两个旧中文名都不再上广场；适配器里的映射仍保留，
	// 让在途请求跑完。
	if name == "minimax_h3_omni" {
		return true
	}
	// Youkou Seedance（volcengine-ark-video 协议，最小成本逐个接入）
	if name == "hmseedance_v2.0不支持真人" {
		return true
	}
	// 漫屋 ArcReel Seedance 2.0/2.5 视频生成
	if name == "seedance-2.0" || name == "seedance-2.5" {
		return true
	}
	// 漫屋 ArcReel ElevenLabs（elevenlabs.io 图像和视频创作台，2026-11-21 上架）：
	// 只一档 el-seedance-2.5（Seedance 2.5 Flash）。
	// 按秒计价：480p ¥0.25/秒、720p ¥0.50/秒（ModelPrice 配 480p 基准价，
	// EstimateBilling 给 seconds 倍率 + 720p size 倍率 2.0）。
	//
	// ⚠️ 与 CapCut 的 cp-seedance-2.5flash 只差前缀，是**两个独立商品**
	// （不同官网、不同账号池、不同价），不要合并进 IsCapcutFamily。
	if name == "el-seedance-2.5" || name == "el-seedance-2.0mini" || name == "el-gemini-1.2flash" {
		return true
	}
	// 漫屋 ArcReel CapCut（剪映国际版）五档：cp-seedance-2.0mini /
	// -2.0fast / -2.0 / -2.5flash / -2.5。
	//
	// ⚠️ 判定走**能力组**（constant.IsCapcutFamily），不要在这里手写模型名 ——
	// 能力组是「谁是这一族」的唯一来源；将来加第六档，这里自动生效，
	// 不会出现「加了模型忘了上广场」。
	//
	// ⚠️ 能力组用**精确集合**：公开名 `cp-seedance-2.5flash` 包含子串
	// `cp-seedance-2.5`，前缀匹配会误伤别家渠道的枚举表。
	if constant.IsCapcutFamily(name) {
		return true
	}
	// 漫屋 ArcReel 两档视频（sd-seedance-2.0fast / sd-seedance-2.0，2026-11-22 上架）：
	// fast ¥0.10/秒、标准 ¥0.22/秒，固定 720p + 9:16。
	//
	// ⚠️ 判定同样走**能力组**（constant.IsTiktokFamily），不在这里手写模型名 ——
	// 能力组是「谁是这一族」的唯一来源，将来加档位这里自动生效。
	// ⚠️ 能力组是精确集合：`sd-seedance-2.0fast` 包含子串 `sd-seedance-2.0`，
	// 前缀匹配会把 fast 档当成标准档（价差 2 倍以上）。
	if constant.IsTiktokFamily(name) {
		return true
	}
	// 漫屋 ArcReel 豆包官网视频（公开短名，转发前由 manwu adaptor 翻译成官网真名；
	// 2026-10-12 去掉 db- 前缀：db-seedance-2-0 → seedance-2-0-fast、db-seedance-2-5 → seedance-2-5）
	if name == "seedance-2-5" || name == "seedance-2-0-fast" {
		return true
	}
	// 豆包 Seedance 2.0 **标准版**（2026-11-22 上架，按秒 ¥0.25）。
	//
	// ⚠️ 与 dola 的 `seedance-2.0`（点号，按次 ¥1.0）、CapCut 的 `cp-seedance-2.0`
	// 是三个不同商品：不同官网、不同账号池、不同计价，**不互为别名**。
	if name == "seedance-2-0" {
		return true
	}
	// 豆包 Seedance **2.5 Flash**（2026-11-22 上架，按秒 ¥0.25、4–15 秒、固定 720p）。
	//
	// ⚠️ 三个近名互不命中：按次的 `seedance-2-5`、CapCut 的
	// `cp-seedance-2.5flash`（¥0.15/秒、480p）、ElevenLabs 的 `el-seedance-2.5`。
	// 本白名单只放行**对外公开名**，上游真名
	// `doubao-seedance-2-5-flash-260628` 只存在于 adaptor 的 modelSpecs。
	if name == "seedance-2-5-flash" {
		return true
	}
	// 漫屋 ArcReel Gemini 官网视频 / Nano Banana Pro 远端图片：价格与 ArcReel 站点口径一致
	//（¥1.0/次、¥0.3/张），上广场。
	if name == "gemini-web-video" || name == "Nano Banana Pro" {
		return true
	}
	// 漫屋 ArcReel 豆包系图片两个模型（2026-11-07 上架）：公开名 = 官网显示名
	//（Seedream 5.0 Flash / Seedream 4.5），按张 ¥0.2/张，与 ArcReel 站点口径一致。
	//
	// ⚠️ 只放行这两个**公开名**：上游档位 id（doubao-seedream-5-0-flash /
	// dola-seedream-4-5）只存在于 adaptor 的 modelSpecs 里，不对外暴露。
	if name == "Seedream 5.0 Flash" || name == "Seedream 4.5" {
		return true
	}
	// 漫屋 ArcReel 通义万相 wan2.7 免灵感值（relax 慢队列）：图生 / 编辑两个模型共用
	// platformId=tongyiwan，靠素材类型分流两条 DOM 路线（同上游 Wan2_7）。
	// ¥0.30/次，与 ArcReel 站点口径一致；交付预期「通常 3 小时左右出一条」写在
	// models 表 description 里（广场正文）。
	if name == "wan2.7-图生" || name == "wan2.7-编辑" {
		return true
	}
	// 漫屋 ArcReel → Picsart 免费无限档 Wan 3.0（2026-10-24 上架，2026-10-25 改名「特惠」）：
	// platformId=picsart，固定 30 秒、分辨率 480P/720P，按次 ¥2.00/条（480P 用 size 倍率折 ¥1.50）。
	// ⚠️ 与有赞渠道的 `wan-3.0-yz` 是两个商品（上面 `name == "wan-3.0-yz"` 是
	// 精确比较，不会误吃本行；本行也不能写成 `strings.HasPrefix(name, "wan-3.0")`，
	// 那会把有赞那一档（wan-3.0-yz / wan-3.0-pro）也一起放进来）。
	// ⚠️ 改名前的旧名 `wan-3.0-无限` **故意不在这里**：白名单只管广场展示，
	// 不影响调用（见 relay/channel/task/manwu/adaptor.go 的 ModelWan3TeHuiLegacy）。
	if name == "wan-3.0-特惠" {
		return true
	}
	// ⚠️ jimeng-video-reverse 故意**不**上广场：反解定价（¥1.0/次）是占位值，
	// 尚未商务核定。在价格确认前只保证可调用（白名单只管广场展示，不影响调用），
	// 确认后再加这一行，并同步接入教程的模型总览
	//（web/default/src/features/tutorials/content-models.ts 的 MODEL_OVERVIEW_ITEMS）。
	// MiniMax-H3-Context-IR 同理（占位价 ¥1.0/次，官方未公布 Context-IR 价目）。
	return false
}

// GetSupportedEndpointMap 返回全局端点到路径的映射
func GetSupportedEndpointMap() map[string]common.EndpointInfo {
	return supportedEndpointMap
}
