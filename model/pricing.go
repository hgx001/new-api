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
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
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
	// MegaAI 渠道的 wan-3.0（上游按次 + 分辨率档计价，与官网 wan3.0-video 区分开）
	if name == "wan-3.0" {
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
	if strings.HasPrefix(name, "wan3.0-video") {
		return true
	}
	// 三元巡狩 wanwan 1080P（拼车）慢速档：按次计费，次日出片
	if name == "wan3.0-1080p-慢速" {
		return true
	}
	// AutoDL ComfyUI：全部已配置工作流（autodl: 前缀）
	if strings.HasPrefix(name, "autodl:") {
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
	// 漫屋 ArcReel 豆包官网视频（公开短名，转发前由 manwu adaptor 翻译成官网真名；
	// 2026-10-12 去掉 db- 前缀：db-seedance-2-0 → seedance-2-0-fast、db-seedance-2-5 → seedance-2-5）
	if name == "seedance-2-5" || name == "seedance-2-0-fast" {
		return true
	}
	// 漫屋 ArcReel Gemini 官网视频 / Nano Banana Pro 远端图片：价格与 ArcReel 站点口径一致
	//（¥1.0/次、¥0.3/张），上广场。
	if name == "gemini-web-video" || name == "Nano Banana Pro" {
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
	// ⚠️ 与 MegaAI 渠道的 `wan-3.0` 是两个商品（上面 `name == "wan-3.0"` 是精确
	// 比较，不会误吃本行；本行也不能写成 `strings.HasPrefix(name, "wan-3.0")`，
	// 那会把 MegaAI 那档也一起放进来）。
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
