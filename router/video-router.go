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
	// ⚠️ Seedance25TeHuiRoute30s **必须早于 Distribute()**：
	// 渠道是在 Distribute 里读 body 的 model 字段选定的，条件路由靠改写 body
	// 来改派模型（见 middleware/seedance_route.go 的说明）。
	// 它内部对任何不确定的情况都「放弃路由」，不会挡客户的请求。
	videoV1Router.Use(middleware.TokenAuth(), middleware.Seedance25TeHuiRoute30s(), middleware.Distribute())
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
