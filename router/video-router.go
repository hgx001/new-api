package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", controller.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	// ⚠️ 顺序是「豆包优先 → 压缩兜底 → 特惠分流」的实现：
	//   · Seedance25TeHuiRoute 先按故障转移 / 首尾帧 / 时长 / 比例改派
	//     （2026-11-11 起 9:16 直接改派到心笔 480P 内链；原「超长提示词改派豆包」已删除）；
	//   · Wan3PromptCompress 处理「没被改派到豆包」且仍超过 5000 字符的请求；
	//   · Wan3TehuiXinbiRoute 再把 wan3.0-720p-特惠 里心笔能跑的请求分流过去
	//     （480P 生成 + 交付 720P，成本更低）；带首尾帧/参考视频/音频的留在 Wan3。
	// 三者都必须早于 Distribute()：渠道是读 body 的 model 字段选定的。
	// 任何一步不确定都「放弃/放行」，不会挡客户的请求。
	//
	// 2026-11-11：活动版常驻链路已从心笔切回有赞原生 720P，
	// 「活动版参考视频改派有赞」规则随之删除 —— 源链路就是目标链路，
	// 再改写成特惠只会多绕一次素材搬运，没有任何收益。
	//
	// 2026-11-14：wan3.0-720p-特惠 已停止单独供货，客户带这个旧名字进来的
	// 请求由 Wan3TehuiAliasRoute 无条件等价成 seedance2.5-特惠（不看参数、
	// 不受 seedanceroute 开关影响），随后照常走 seedance2.5-特惠 的全部规则。
	// 必须排在 Seedance25TeHuiRoute **之前**：一是别名先改名、规则再分流；
	// 二是规则二/三内部改派到 wan3.0-720p-特惠 时本中间件已执行完，
	// 不会把内部改派（首尾帧仍需 wan3 直连）再打回特惠链路。
	videoV1Router.Use(
		middleware.TokenAuth(),
		// 必须在所有条件路由之前：它们会改写 body 的 model/seconds，
		// 之后捕获到的就不是客户原始请求参数了（日志详情要按原始请求展示）。
		middleware.CaptureTaskRequestParams(),
		middleware.Wan3TehuiAliasRoute(),
		middleware.Seedance25TeHuiRoute(),
		middleware.Wan3PromptCompress(),
		middleware.Wan3TehuiXinbiRoute(),
		middleware.Distribute(),
	)
	{
		videoV1Router.POST("/video/generations", controller.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", controller.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", controller.RelayTask)
		videoV1Router.GET("/videos/:task_id", controller.RelayTaskFetch)
	}

	// 本地参考图上交换公网 URL（manwu 渠道闭环，2026-10-11）：multipart 薄转发到
	// ArcReel 暂存图床。只挂 TokenAuth 不挂 Distribute——没有模型路由语义，
	// 渠道（baseURL+密钥）在 controller 内按 manwu 类型解析。
	//
	// 2026-11-14 新增 /v1/files：签发 COS 直传预签名，客户端把素材**直接**
	// PUT 到对象存储，字节不经过本机（/v1/uploads 的字节要走 3Mbps 出口）。
	// 两者共存：/v1/uploads 适合小图且想一步到位；/v1/files 适合大素材或
	// 素材源在境外（代下载受 ArcReel 侧 60s 硬预算约束，超过 4MB 必定 400）。
	filesV1Router := router.Group("/v1")
	filesV1Router.Use(middleware.RouteTag("relay"))
	filesV1Router.Use(middleware.TokenAuth())
	{
		filesV1Router.POST("/uploads", controller.UploadStagingFile)
		filesV1Router.POST("/files", controller.PresignCosUploadFile)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", controller.RelayTask)
		klingV1Router.POST("/videos/image2video", controller.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", controller.RelayTask)
	}
}
