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
	// ⚠️ 顺序是「豆包优先 → 压缩兜底 → 特惠分流 → 活动版参考视频改派」的实现：
	//   · Seedance25TeHuiRoute 先按故障转移 / 首尾帧 / 时长 / 比例改派
	//     （2026-11-11 起 9:16 直接改派到心笔 480P 内链；原「超长提示词改派豆包」已删除）；
	//   · Wan3PromptCompress 处理「没被改派到豆包」且仍超过 5000 字符的请求；
	//   · Wan3TehuiXinbiRoute 再把 wan3.0-720p-特惠 里心笔能跑的请求分流过去
	//     （480P 生成 + 交付 720P，成本更低）；带首尾帧/参考视频/音频的留在 Wan3；
	//   · Seedance25HuoDongRefVideoRoute 最后把活动版带参考视频的请求改派到有赞 prime
	//     （心笔会把参考视频秒数并入上游计费，按次售价可能倒挂）。必须排在特惠分流之后，
	//     否则改写成特惠的请求会被特惠分流再次改写回心笔。
	// 四者都必须早于 Distribute()：渠道是读 body 的 model 字段选定的。
	// 任何一步不确定都「放弃/放行」，不会挡客户的请求。
	videoV1Router.Use(
		middleware.TokenAuth(),
		middleware.Seedance25TeHuiRoute(),
		middleware.Wan3PromptCompress(),
		middleware.Wan3TehuiXinbiRoute(),
		middleware.Seedance25HuoDongRefVideoRoute(),
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
	uploadsV1Router := router.Group("/v1")
	uploadsV1Router.Use(middleware.RouteTag("relay"))
	uploadsV1Router.Use(middleware.TokenAuth())
	{
		uploadsV1Router.POST("/uploads", controller.UploadStagingFile)
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
