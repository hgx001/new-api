package model

// ════════════════════════════════════════════════════════════════════════════
// 客户日志的「渠道隐藏」契约
// ════════════════════════════════════════════════════════════════════════════
//
// 这是 seedance2.5-特惠 三道防线的第 ② 道（① 任务 DTO 在
// relay/seedance25_tehui_channel_hide_test.go，③ API 权限在 router 包）。
//
// 日志比任务 DTO 更容易漏，原因是它有**两个**渠道出口：
//
//	· 结构体字段   ChannelId / ChannelName / UpstreamRequestId —— 直接序列化
//	· Other 字段   一个 JSON 字符串，里面塞着 channel_id / channel_name /
//	               channel_type / upstream_model_name / admin_info …
//	               —— 它是个字符串，结构体层面看不出任何问题，只能逐个 key 删
//
// 第二类是真正的坑：给 Log 结构体加了字段、改脱敏函数都没用，因为渠道信息
// 藏在 Other 这个"自由格式"里。所以这里既要守字段，也要守那份 key 清单。
//
// 口径（2026-10-30 老板定）：凡是能看出走了哪个渠道、哪个上游的，一律过滤掉。
// 客户不需要知道我们内部怎么调度；知道了就能反推货源、比价、绕开我们。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userLogRequiredDroppedKeys 是 Other 里**必须**被删掉的渠道痕迹键。
//
// 为什么单独列一份而不直接引用 userLogDroppedOtherKeys：如果哪天有人
// 「精简」了那份清单（比如觉得 is_model_mapped 没什么用），直接引用就等于
// 拿被改过的清单验证自己，永远通过。这份是**独立的期望值**，清单被删就红。
//
// 每个键后面的注释是「删它的理由」，也是判断新键该不该加进来的标准：
// 这个键能不能帮客户定位到某个具体渠道或上游？
var userLogRequiredDroppedKeys = []string{
	"channel_id",          // 渠道编号：对着渠道表一查就是哪家
	"channel_name",        // 渠道名（"YouzanWan3" 直接点名货源）
	"channel_type",        // 渠道类型编号（63 = 有赞 Wan3）
	"channel",             // 部分链路把编号写成 channel
	"upstream_request_id", // 上游任务号：能拿去上游后台查单
	"upstream_model_name", // 上游真实模型名（wan3.0-video-prime）
	"is_model_mapped",     // 是否做了模型映射：本身就是改派痕迹
	"route_target_model",  // 条件路由改派的目标模型
	"admin_info",          // 管理员排障信息（含 use_channel 候选链）
	"audit_info",          // 操作审计（含 operator / route 信息）
}

// TestUserLogDroppedOtherKeys_渠道键一个都不能删 守的是那份删除清单本身。
//
// 这是最容易被"顺手优化"的地方：清单看起来又长又啰嗦，有人会觉得
// 「channel 和 channel_id 差不多，留一个就行」—— 但不同链路写的 key 不一样，
// 删任何一个都会让某条链路重新漏出渠道编号。
func TestUserLogDroppedOtherKeys_渠道键一个都不能删(t *testing.T) {
	present := make(map[string]bool, len(userLogDroppedOtherKeys))
	for _, key := range userLogDroppedOtherKeys {
		present[key] = true
	}
	for _, key := range userLogRequiredDroppedKeys {
		assert.True(t, present[key],
			"userLogDroppedOtherKeys 里缺了 %q —— 这个键能帮客户定位到具体渠道/上游，"+
				"删掉它对应的链路就会重新漏出渠道信息", key)
	}
}

// TestFormatUserLogs_渠道字段精确清零 断言三个渠道字段的**精确值**。
//
// 为什么不用 NotContains("channel":12)：那只证明"12 没出现"，证明不了
// "被置成了 0"。如果哪天改成置 -1、或者只在某些条件下置 0，那种断言照样通过。
func TestFormatUserLogs_渠道字段精确清零(t *testing.T) {
	logs := []*Log{
		{
			Type:              LogTypeError,
			ModelName:         "seedance2.5-特惠",
			ChannelId:         17,
			ChannelName:       "有赞 Wan3", // 生产 channels.name 原值（id=17, type=63）
			UpstreamRequestId: "wz_83d06520-ed98-4621-9e40",
			Content:           `status_code=400, {"error":{"message":"model wan3.0-video-prime not available"}}`,
		},
	}

	formatUserLogs(logs, 0)

	assert.Equal(t, 0, logs[0].ChannelId, "渠道编号必须精确置 0")
	assert.Equal(t, "", logs[0].ChannelName, "渠道名必须清空 —— 它直接点名货源")
	assert.Equal(t, "", logs[0].UpstreamRequestId,
		"上游任务号必须清空 —— 客户能拿它去上游后台查单")
}

// TestFormatUserLogs_Other里的渠道键全部被删 覆盖 Other 这个"自由格式"出口。
//
// 用真机上出现过的 Other 形状（错误日志带 admin_info.use_channel 候选链），
// 断言删除后一个渠道键都不剩，但**客户需要的字段要留**（status_code 之类）。
func TestFormatUserLogs_Other里的渠道键全部被删(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeError,
			ModelName: "seedance2.5-特惠",
			ChannelId: 17,
			Other: `{"channel_id":17,"channel_name":"有赞 Wan3","channel_type":63,` +
				`"channel":17,"upstream_request_id":"wz_83d06520","upstream_model_name":"wan3.0-video-prime",` +
				`"is_model_mapped":true,"route_target_model":"wan3.0-720p-特惠",` +
				`"admin_info":{"use_channel":["17"]},"audit_info":{"operator":"admin"},` +
				`"status_code":400,"request_path":"/v1/videos"}`,
		},
	}

	formatUserLogs(logs, 0)

	var other map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &other),
		"Other 必须仍是合法 JSON —— 删 key 不能把结构搞坏")

	for _, key := range userLogRequiredDroppedKeys {
		_, exists := other[key]
		assert.False(t, exists, "Other 里的 %q 必须删掉", key)
	}

	// 该留的还是要留：客户靠 status_code 判断错误类型
	assert.Equal(t, float64(400), other["status_code"], "错误码要留")
	assert.Equal(t, "/v1/videos", other["request_path"], "请求路径要留")
}

// TestFormatUserLogs_任务号与耗时可给客户看 断言两个新字段**不脱敏**。
//
// 为什么它们可以给客户看：
//   - task_id 是**本站**的公开任务号（task_xxx），客户提交任务时就拿到了，
//     他本来就在用这个 ID 查任务，拿去对不上任何渠道。
//   - task_duration 是任务跑了多久，与走哪条链路无关。
//
// 对比 upstream_request_id：那是**上游**的任务号，客户拿它能去上游后台查单，
// 等于泄露了货源，所以必须清空。两者的区别就是「本站的」与「上游的」。
func TestFormatUserLogs_任务号与耗时可给客户看(t *testing.T) {
	logs := []*Log{
		{
			Type:         LogTypeConsume,
			ModelName:    "seedance2.5-特惠",
			ChannelId:    17,
			TaskId:       "task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q",
			TaskDuration: 143,
			UseTime:      2,
		},
	}

	formatUserLogs(logs, 0)

	assert.Equal(t, "task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q", logs[0].TaskId,
		"本站公开任务号要给客户 —— 这是他自己提交任务时拿到的 ID")
	assert.Equal(t, 143, logs[0].TaskDuration,
		"任务完成时长要给客户 —— 与渠道无关")
	assert.Equal(t, 0, logs[0].ChannelId,
		"但渠道编号仍然必须清空 —— 耗时可以给，货源不能给")
}

// ────────────────────────────────────────────────────────────────────────────
// Log 字段清单守卫
// ────────────────────────────────────────────────────────────────────────────

// logKnownFields 是 model.Log 的**全部** JSON 字段。
//
// 与 relay 那边的 TaskDto 守卫同一个道理：给 Log 加字段，默认就是原样序列化，
// 没人会记得回来改 formatUserLogs。想加字段？先在这里加一行，然后回答
// 「这个字段会不会暴露渠道、上游模型名、上游成本、上游地址」。
//
// 特别注意：channel_name 这个字段之所以是安全的，**只因为** formatUserLogs
// 把它清空了。它不是"没有这个字段"，而是"有但被清空"。
var logKnownFields = []string{
	"id",
	"user_id",
	"created_at",
	"type",
	"content",
	"username",
	"token_name",
	"model_name",
	"quota",
	"prompt_tokens",
	"completion_tokens",
	"use_time",
	"task_duration", // 任务真实完成时长（秒）：与渠道无关，可直接给客户
	"is_stream",
	"channel",      // ⚠️ json tag 就叫 channel（值是渠道 ID），必须置 0
	"channel_name", // ⚠️ 必须清空
	"token_id",
	"group",
	"ip",
	"request_id",
	"upstream_request_id", // ⚠️ 必须清空
	"task_id",             // 本站公开任务号（task_xxx）：客户自己就在用这个 ID 查任务
	"other",               // ⚠️ 自由格式，靠 userLogDroppedOtherKeys 逐个删
}

// logChannelSensitiveFields 是需要脱敏介入的字段。
var logChannelSensitiveFields = map[string]string{
	"channel":             "渠道编号（json tag 是 channel）：formatUserLogs 必须置 0",
	"channel_name":        "渠道名：必须清空",
	"upstream_request_id": "上游任务号：必须清空",
	"other":               "自由格式 JSON：靠 userLogDroppedOtherKeys 逐个删",
}

// TestLog_字段清单守卫 防止「给日志加字段忘了脱敏」。
//
// 日志是客户在网页上摊开看的，任何一个字段漏出去都算泄露。
// 反射比对能发现"多出来的字段"—— 而多出来的字段才是泄露的来源。
func TestLog_字段清单守卫(t *testing.T) {
	actual := logJSONFieldNames(reflect.TypeOf(Log{}))

	assert.ElementsMatch(t, logKnownFields, actual,
		"model.Log 的字段变了。\n"+
			"新增字段前请回答：它会暴露渠道、上游模型名、上游成本或上游地址吗？\n"+
			"如果会，必须在 formatUserLogs 里清掉（或加进 userLogDroppedOtherKeys），并更新本清单。\n"+
			"如果不会，把它加进 logKnownFields 即可。")

	fieldSet := make(map[string]bool, len(actual))
	for _, name := range actual {
		fieldSet[name] = true
	}
	for field, why := range logChannelSensitiveFields {
		assert.True(t, fieldSet[field],
			"渠道敏感字段 %q 不见了（%s）。\n"+
				"如果它被改名了，formatUserLogs 里的清理代码就失效了 —— "+
				"请同步修改，否则下游日志里会重新看到渠道信息。", field, why)
	}
}

// logJSONFieldNames 取出一个结构体所有会被序列化的 JSON 字段名。
// 与 relay 包的 jsonFieldNames 同实现 —— 两个包各自持有，避免为测试建公共依赖。
func logJSONFieldNames(t reflect.Type) []string {
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
