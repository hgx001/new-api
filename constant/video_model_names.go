package constant

// ════════════════════════════════════════════════════════════════════════════
// 跨层引用的视频模型公开名
// ════════════════════════════════════════════════════════════════════════════
//
// 为什么单独一个文件：有些模型名要同时被 middleware（路由判定）与 relay 渠道包
// （adaptor 的后处理判定）引用，两边各写一份字符串时，改名只改一处就会变成
// 「路由过去了，后处理不认识」——任务照样成功、交付档位却掉一档，没有任何报错。
//
// 放在 constant 包：它不 import 任何业务包，middleware / relay / setting 都能
// 单向依赖它，不会引入 import 环。

const (
	// Seedance25DoubaoModel 是豆包官网链路（Seedance 2.5）的对外模型名。
	//
	// ⚠️ 它**不是** constant.Seedance25Family 的成员：族里的两个名字是
	// 促销族（seedance2.5-特惠 / -活动，上游为有赞 prime），本名字是豆包官网
	// 那条链路（ch20，manwu adaptor）。两者能力口径不同，不能混进同一份
	// 能力定义里（族的能力表会被档位校验、脱敏、广场白名单共用）。
	//
	// 引用方：
	//   - middleware：规则一的目标、规则五的源（doubaoTargetModel）；
	//   - relay/channel/task/manwu：改派到 CapCut 备选链路后的「超分交付」判定
	//     （按 RouteOriginModel 识别「这条 CapCut 任务是不是豆包降级来的」）。
	Seedance25DoubaoModel = "seedance-2-5"

	// Wan3TehuiLegacyModel 是促销族母本的**历史别名**（wan3.0-720p-特惠）。
	//
	// 客户带这个旧名字进来时，别名中间件（middleware/wan3_tehui_alias.go）
	// 只改请求体里的 model，**计费原点仍保留旧名** —— 所以凡是按
	// RouteOriginModel 判定「客户买的是哪一档交付」的地方（当前是 CapCut
	// 交付前的超分判定），必须把它和促销族一视同仁：它同样按 720P 售卖。
	//
	// 漏掉它的后果：这一批老客户的 CapCut 任务只按 480p 生成、又不超分，
	// 最后交付的是 480P 原片 —— 任务成功、账单照收，客户才发现糊了。
	Wan3TehuiLegacyModel = "wan3.0-720p-特惠"
)
