package relay

// ════════════════════════════════════════════════════════════════════════════
// seedance2.5-特惠 的「渠道隐藏」契约 —— 改代码前请先读完这段
// ════════════════════════════════════════════════════════════════════════════
//
// 事实：seedance2.5-特惠 是**别名**，底层跑的是 wan3（有赞渠道，2026-10-29 起
// 上游真实模型 wan3.0-video-prime）。这层别名存在的唯一目的，就是不让下游
// 知道真实货源。
//
// 一旦客户知道真实上游，会发生三件事，每一件都直接打掉这门生意：
//   · 反推成本 → 拿我们的售价跟上游比价
//   · 绕过我们 → 直接找上游对接
//   · 理解改派 → 发现「我调 A、实际跑 B」，质疑交付
//
// 所以「隐藏渠道」不是锦上添花的洁癖，是这个模型的**商业前提**。
// 本文件把这条前提变成可执行的断言，防止后续改动顺手把它拆了。
//
// ── 三道防线（改任何一道之前，先看这里的测试为什么这么写）────────────────
//
//	① 任务 DTO   relay.TaskModel2Dto → redactChannelInfo / redactAliasTaskUpstream
//	② 日志       model.formatUserLogs → 清 channel 字段 + userLogDroppedOtherKeys
//	③ API 权限   /api/channel 全 AdminAuth，/api/ratio_sync/channels 是 RootAuth
//
// 本文件守 ①；② 在 model/channel_hide_guard_test.go；③ 在 router 包。
//
// ── 最容易踩的坑 ─────────────────────────────────────────────────────────
//
//   · 「顺手给 TaskDto 加个 channel_name 字段」—— 结构体一加，序列化就带出去，
//     redactChannelInfo 里没人知道要清它。TestTaskDto_字段清单守卫 会当场拦住。
//   · 「platform 是渠道类型编号，前端要显示，留着吧」—— "63" 对着渠道类型表
//     一查就是有赞 Wan3。必须换成「上游」，前端本来就有 fallback 文案。
//   · 「Suno 保留了 data，那 seedance 也保留吧」—— Suno 的 data 是**交付物**
//     （音频片段），seedance 的 data 是**上游回执**（上游任务号 + 上游成本 +
//     上游直链）。两者名字一样，性质相反。
//   · 「条件路由改派到 wan3.0-720p-特惠 了，那是客户能看到的模型名，不用遮」——
//     错。目标模型同样不能出现，客户看到 model 变了就知道被改派了。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance25TeHui 是被保护的别名模型。测试里反复用到，抽成常量避免手误。
const seedance25TeHui = "seedance2.5-特惠"

// 上游真实身份（测试里用作「不该出现的字样」）。取值与生产库一致：
// channels 表里 id=17 就是「有赞 Wan3」type=63，abilities 表把它同时挂在
// seedance2.5-特惠 与 wan3.0-video-prime 上（2026-11-01 核对）。
const (
	upstreamRealModel    = "wan3.0-video-prime"
	upstreamChannelName  = "有赞 Wan3" // channels.name 原值
	upstreamChannelType  = "63"      // constant 里「有赞 Wan3」的类型编号
	upstreamChannelID    = 17        // 生产渠道 #17
	upstreamRawReceiptJS = `{"id":"wz_83d06520","model":"wan3.0-video-prime","billing_amount":1.8,"sourceUrl":"https://dashscope-result.oss-cn-beijing.aliyuncs.com/raw.mp4"}`
	upstreamVideoURL     = "https://d4c280ea2c37b44805c584296cb271a0.r2.cloudflarestorage.com/hailuo-video/vid_1.m4v"
)

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：别名白名单
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25TeHui_必须在别名白名单里 守的是**所有脱敏的总开关**。
//
// model.IsUpstreamAliasModel 决定 RedactUpstreamIdentity / redactAliasTaskUpstream
// 要不要干活。从 upstreamAliasModels 里删掉一行，这两条链路就静默 return ——
// 不报错、不告警，上游模型名和上游回执原样漏给下游。
//
// 为什么两个名字都要测：2026-10-30 改名时新旧名并存过一段，旧名漏出白名单
// 就等于旧调用方的响应里照样能看到 wan3。历史上真的差点漏。
func TestSeedance25TeHui_必须在别名白名单里(t *testing.T) {
	for _, name := range []string{
		seedance25TeHui,  // 现用名
		"seedance2.5特惠版", // 历史旧名，仍在兼容期
	} {
		assert.True(t, model.IsUpstreamAliasModel(name),
			"模型 %q 必须留在 upstreamAliasModels 里 —— 删掉它，"+
				"redactAliasTaskUpstream 会直接 return，上游模型名与上游回执全部漏给下游", name)
	}

	// 反证：非别名模型不该被误判成别名。
	//
	// 为什么这条也重要：把 wan3.0-720p-特惠 之外的一堆模型塞进白名单，
	// 会让它们的报错文案被过度改写（客户看不懂自己买的是什么）。
	// 白名单要精确，不能图省事写成一类。
	for _, name := range []string{"gpt-4o", "wan3.0-video", "seedance-2-5"} {
		assert.False(t, model.IsUpstreamAliasModel(name),
			"模型 %q 不是别名，不该进白名单 —— 会把客户自己买的模型名也打散", name)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：任务 DTO 全字段审计（最坏情况）
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25TeHui_任务DTO不暴露渠道 是**最坏情况**审计：
// 把库里能填的渠道字段全部填满（渠道 ID、渠道类型、上游回执、上游模型名、
// 上游直链、带货源的失败文案），然后断言对外 DTO 一个都不剩。
//
// 为什么按「序列化后的字符串」断言而不是逐字段：客户是从整个响应体里翻的，
// 任何一个字段漏出去都算泄露。逐字段断言只能保证你想到的那些字段 ——
// 而泄露恰恰发生在没想到的地方。
func TestSeedance25TeHui_任务DTO不暴露渠道(t *testing.T) {
	task := &model.Task{
		ID:        4242,
		TaskID:    "task_seedance_probe",
		ChannelId: upstreamChannelID,
		Platform:  constant.TaskPlatform(upstreamChannelType),
		Status:    model.TaskStatusFailure,
		Properties: model.Properties{
			UpstreamModelName: upstreamRealModel, // 库里记的是上游真名
			OriginModelName:   seedance25TeHui,
		},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "wz_83d06520-ed98-4621-9e40",
			ResultURL:      upstreamVideoURL,
		},
		FailReason: "youzan wan3 rejected the request: model " + upstreamRealModel + " not available",
		Data:       json.RawMessage(upstreamRawReceiptJS),
	}

	out := TaskModel2Dto(task)
	require.NotNil(t, out)

	// ── 逐字段：三个渠道字段必须被 redactChannelInfo 处理 ──
	assert.Equal(t, 0, out.ChannelId,
		"channel_id 必须置 0 —— 客户拿编号对着渠道表一查就知道是哪家")
	assert.Equal(t, "上游", out.Platform,
		"platform 其实是渠道类型编号（%q = 有赞 Wan3），必须换成「上游」", upstreamChannelType)
	assert.Nil(t, out.Data,
		"data 是上游完整回执（上游任务号 + 上游成本 + 上游原片直链），必须清空")

	props, ok := out.Properties.(model.Properties)
	require.True(t, ok, "properties 结构不能变，前端按固定字段渲染")
	assert.Equal(t, seedance25TeHui, props.UpstreamModelName,
		"upstream_model_name 必须换回对外别名 —— 这是客户最容易看到上游真名的地方")

	// ── 整包：序列化后一个渠道痕迹都不能剩 ──
	raw, err := common.Marshal(out)
	require.NoError(t, err)
	body := string(raw)

	for _, banned := range []struct {
		needle string
		why    string
	}{
		{`"channel_id":17`, "渠道编号不能给下游"},
		{`"platform":"63"`, "渠道类型编号一查就知道是「有赞 Wan3」"},
		{upstreamChannelName, "渠道名直接点名货源"},
		{upstreamRealModel, "上游真实模型名（wan3.0-video-prime）"},
		{"wan3", "wan3 字样本身（含 wan-3.0 / wan3.0 各种写法）"},
		{"youzan", "供应商名"},
		{"billing_amount", "上游成本：给了就能算出我们赚多少"},
		{"sourceUrl", "上游原片直链"},
		{"aliyuncs.com", "上游存储域名"},
		{"r2.cloudflarestorage.com", "上游存储域名"},
		{"hailuo-video", "上游货源目录"},
		{"wz_83d06520", "上游任务号：能拿去上游后台查"},
	} {
		assert.NotContains(t, body, banned.needle,
			"对外响应体不能出现渠道痕迹 —— %s", banned.why)
	}

	// ── 该留的必须留：别为了脱敏把交付信息也抹了 ──
	assert.Contains(t, body, seedance25TeHui, "模型名用客户请求的源模型")
	assert.Contains(t, body, "task_seedance_probe", "任务号要留，客户靠它查任务")
	assert.Contains(t, body, "上游", "对客户统一说「上游」")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 3：platform 白名单
// ────────────────────────────────────────────────────────────────────────────

// TestRedactChannelInfo_只有suno保留平台原值 锁住 platform 的**白名单**语义。
//
// redactChannelInfo 里写的是：
//
//	keepsData := platform == suno || platform == mj   // data 保留
//	if platform != suno { platform = "上游" }          // 只有 suno 留原值
//
// 注意两个判断**不对称**：mj 的 data 保留，但 platform 照样改成「上游」。
// 这不是笔误 —— suno 的 platform 要留给前端判断要不要渲染音频播放器
// （task-logs-columns.tsx 里 `platform === 'suno'`），mj 没有这个需求。
//
// 为什么要表驱动逐个钉：这个白名单一旦被写成黑名单（"只要不是 xxx 就保留"），
// 新接一个渠道就会默认漏出它的类型编号，而且没有任何测试会红。
func TestRedactChannelInfo_只有suno保留平台原值(t *testing.T) {
	for _, tc := range []struct {
		name         string
		platform     string
		wantPlatform string
		wantDataNil  bool
		why          string
	}{
		{
			name:         "有赞 Wan3（seedance2.5-特惠 的真实渠道）",
			platform:     upstreamChannelType,
			wantPlatform: "上游",
			wantDataNil:  true,
			why:          "编号 63 对着渠道表就是「有赞 Wan3」，等于直接报出货源",
		},
		{
			name:         "漫屋",
			platform:     "57",
			wantPlatform: "上游",
			wantDataNil:  true,
			why:          "任何渠道类型编号都不该给下游",
		},
		{
			name:         "空 platform（老数据）",
			platform:     "",
			wantPlatform: "上游",
			wantDataNil:  true,
			why:          "空值也要归一成「上游」，避免前端渲染出空白单元格",
		},
		{
			name:         "Suno",
			platform:     string(constant.TaskPlatformSuno),
			wantPlatform: string(constant.TaskPlatformSuno),
			wantDataNil:  false,
			why:          "suno 是公开产品线，前端靠 platform 判断要不要渲染音频播放器；data 是交付物（音频片段）",
		},
		{
			name:         "Midjourney",
			platform:     string(constant.TaskPlatformMidjourney),
			wantPlatform: "上游",
			wantDataNil:  false,
			why:          "mj 的 data 是交付物（图片）要留，但 platform 没有前端依赖，照样遮成「上游」",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID:    "task_platform_probe",
				ChannelId: upstreamChannelID,
				Platform:  constant.TaskPlatform(tc.platform),
				Data:      json.RawMessage(upstreamRawReceiptJS),
			}

			out := TaskModel2Dto(task)

			assert.Equal(t, tc.wantPlatform, out.Platform, "%s", tc.why)
			assert.Equal(t, 0, out.ChannelId, "channel_id 对所有平台都必须置 0")
			if tc.wantDataNil {
				assert.Nil(t, out.Data, "data 必须清空 —— %s", tc.why)
			} else {
				assert.NotNil(t, out.Data, "data 是交付物，不能清 —— %s", tc.why)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 4：条件路由改派后仍要隐藏
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25TeHui_条件路由改派后仍隐藏渠道 守的是最隐蔽的一条路径。
//
// seedance2.5-特惠 会被条件路由改派：
//
//	30 秒        → seedance-2-5（豆包）
//	9:16 非 30 秒 → wan3.0-720p-特惠（wan3 渠道）
//
// 改派后 task 上记的是**目标**模型与**目标**渠道。如果脱敏按 task 上的模型名
// 判断（而不是用户请求的源模型），目标模型不在别名白名单里 → 直接 return →
// data / fail_reason / result_url / channel_id 全部原样漏出。
//
// 这就是 redactAliasTaskUpstream 里那句「口径按用户请求的源模型判断」的由来，
// 也是本测试存在的理由：它钉的是**判断依据**，不是判断结果。
func TestSeedance25TeHui_条件路由改派后仍隐藏渠道(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_routed",
		ChannelId: 12,                          // 改派后的 wan3 渠道
		Platform:  constant.TaskPlatform("12"), // 改派后的渠道类型
		Status:    model.TaskStatusFailure,
		Properties: model.Properties{
			UpstreamModelName: "wan3.0-video",   // 改派后的上游模型
			OriginModelName:   "wan3.0-720p-特惠", // 改派后的目标模型（客户不该看到）
		},
		PrivateData: model.TaskPrivateData{
			// 用户真正请求的源模型 —— 脱敏必须按它判断
			RouteOriginModel: seedance25TeHui,
			ResultURL:        upstreamVideoURL,
		},
		FailReason: "wan3.0-720p-特惠 rejected: wan3 upstream busy",
		Data:       json.RawMessage(upstreamRawReceiptJS),
	}

	out := TaskModel2Dto(task)
	raw, err := common.Marshal(out)
	require.NoError(t, err)
	body := string(raw)

	// 对外必须是源模型，不能是改派目标
	assert.Contains(t, body, seedance25TeHui,
		"对外 model 必须是用户请求的源模型 —— 改派是内部调度，不该让客户看见")
	assert.NotContains(t, body, "wan3.0-720p-特惠",
		"改派目标模型名不能出现 —— 客户看到 model 变了就知道被改派了")
	assert.NotContains(t, body, `"channel_id":12`,
		"改派后的渠道同样不能暴露")

	// 别名口径的脱敏必须照样生效（这正是「按源模型判断」的意义）
	assert.Nil(t, out.Data, "改派后 data 同样要清 —— 里面是上游回执")
	assert.Equal(t, 0, out.ChannelId, "改派后 channel_id 同样要置 0")
	assert.Equal(t, "上游", out.Platform, "改派后 platform 同样要换成「上游」")

	props, ok := out.Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, seedance25TeHui, props.UpstreamModelName,
		"upstream_model_name 要用源模型名，不能用改派目标")
	assert.Equal(t, seedance25TeHui, props.OriginModelName,
		"origin_model_name 必须换回源模型 —— 客户在任务列表里最容易看到它")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 5：TaskDto 字段清单（防「顺手加个字段」）
// ────────────────────────────────────────────────────────────────────────────

// taskDtoKnownFields 是 dto.TaskDto 的**全部** JSON 字段。
//
// 这份清单不是抄着玩的：它是「新字段必须显式审一遍」的闸门。
// 想给 TaskDto 加字段？先在这里加一行，然后回答一个问题 ——
// 这个字段会不会暴露渠道、上游模型、上游成本、上游地址？
// 答不上来就别加。
//
// 历史教训：redactChannelInfo 只清它知道的字段。结构体新增的字段
// 默认是「原样序列化」，没人会记得回来改脱敏逻辑。
var taskDtoKnownFields = []string{
	"id",
	"created_at",
	"updated_at",
	"task_id",
	"platform", // ⚠️ 渠道类型编号，必须被换成「上游」
	"user_id",
	"group",
	"channel_id", // ⚠️ 渠道编号，必须置 0
	"quota",
	"action",
	"status",
	"fail_reason",
	"result_url",
	"result_text",
	"submit_time",
	"start_time",
	"finish_time",
	"progress",
	"properties", // ⚠️ 内含 upstream_model_name / origin_model_name
	"username",
	"data", // ⚠️ 上游原始回执，非 Suno/MJ 必须清空
}

// taskDtoChannelSensitiveFields 是需要脱敏介入的字段，附上原因。
//
// 单独列出来是为了做**存在性校验**：如果有人把 channel_id 改名成 channel，
// 或者把 platform 挪进 properties，字段清单会变，但下面这份名单不会 ——
// 两相对照就能发现「敏感字段被搬走了」，而搬走往往意味着脱敏失效。
var taskDtoChannelSensitiveFields = map[string]string{
	"channel_id": "渠道编号：redactChannelInfo 必须置 0",
	"platform":   "渠道类型编号（\"63\" = 有赞 Wan3）：必须换成「上游」",
	"data":       "上游原始回执（上游任务号/成本/直链）：非 Suno/MJ 必须清空",
	"properties": "含 upstream_model_name / origin_model_name：必须换成源模型名",
}

// TestTaskDto_字段清单守卫 是**防「不小心改了暴露渠道」的主力**。
//
// 它做两件事：
//  1. 断言 TaskDto 的 JSON 字段集合与已知清单**完全一致** —— 新增/删除/改名
//     任何字段都会红，逼改动者回来审一遍。
//  2. 断言四个渠道敏感字段仍然存在 —— 防止它们被「优化」进别的结构体，
//     那样 redactChannelInfo 就清不到它们了。
//
// 为什么用反射而不是硬编码：硬编码只能发现「少了一个字段」，
// 发现不了「多了一个字段」—— 而多出来的字段才是泄露的来源。
func TestTaskDto_字段清单守卫(t *testing.T) {
	actual := jsonFieldNames(reflect.TypeOf(dto.TaskDto{}))

	assert.ElementsMatch(t, taskDtoKnownFields, actual,
		"dto.TaskDto 的字段变了。\n"+
			"新增字段前请回答：它会暴露渠道、上游模型名、上游成本或上游地址吗？\n"+
			"如果会，必须在 relay.TaskModel2Dto 的 redactChannelInfo 里清掉，并更新本清单。\n"+
			"如果不会，把它加进 taskDtoKnownFields 即可。")

	// 敏感字段存在性校验
	fieldSet := make(map[string]bool, len(actual))
	for _, name := range actual {
		fieldSet[name] = true
	}
	for field, why := range taskDtoChannelSensitiveFields {
		assert.True(t, fieldSet[field],
			"渠道敏感字段 %q 不见了（%s）。\n"+
				"如果它被改名或搬进了别的结构体，redactChannelInfo 就清不到它 —— "+
				"请同步修改脱敏逻辑，否则下游会重新看到渠道信息。", field, why)
	}
}

// jsonFieldNames 取出一个结构体所有**会被序列化**的 JSON 字段名。
//
// 跳过 json:"-" 的字段；tag 里带逗号（如 `json:"x,omitempty"`）只取第一段。
// 没有 json tag 的字段按 Go 字段名兜底（encoding/json 的实际行为），
// 但这种字段本身就是隐患，所以一并暴露在清单里让人看见。
func jsonFieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
}
