package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	anonymousRequestBodyLimit := middleware.AnonymousRequestBodyLimit()
	{
		apiRouter.GET("/setup", controller.GetSetup)
		apiRouter.POST("/setup", anonymousRequestBodyLimit, controller.PostSetup)
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.GET("/home/metrics", controller.GetHomeMetrics)
		apiRouter.GET("/uptime/status", controller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)
		// VPN 套餐订单与收入是独立账（不进用户 quota / top_ups），管理端单独只读查看。
		apiRouter.GET("/vpn/admin/orders", middleware.AdminAuth(), controller.AdminGetVpnOrders)
		// 经营统计：充值实付（现金口径）与渠道消耗金额（经营口径）分开统计，
		// 上游没有可用账单接口，成本由管理员手工录入。全部接口要求 AdminAuth。
		financeRoute := apiRouter.Group("/finance")
		financeRoute.Use(middleware.AdminAuth())
		{
			financeRoute.GET("/summary", controller.GetFinanceSummary)
			financeRoute.GET("/channels", controller.GetFinanceChannels)
			financeRoute.GET("/channels/options", controller.GetFinanceChannelOptions)
			financeRoute.GET("/trend", controller.GetFinanceTrend)
			financeRoute.GET("/costs", controller.GetFinanceCosts)
			financeRoute.POST("/costs", controller.CreateFinanceCost)
			financeRoute.PUT("/costs/:id", controller.UpdateFinanceCost)
			financeRoute.DELETE("/costs/:id", controller.VoidFinanceCost)
			financeRoute.GET("/export", controller.ExportFinanceCosts)
		}
		// 节点 agent 内部接口：专用 Bearer 令牌鉴权，不复用 AdminAuth。
		apiRouter.GET("/vpn/node/manifest", controller.VpnNodeManifest)
		apiRouter.POST("/vpn/node/connections", anonymousRequestBodyLimit, controller.VpnNodeConnections)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)

		// 云服务器租赁：价格目录与报价公开（购买页未登录也要能看价），
		// 下单与实例管理必须登录。金额由服务端算，前端传价无效。
		serverPublicRoute := apiRouter.Group("/server")
		{
			serverPublicRoute.GET("/catalog", controller.GetServerCatalog)
			serverPublicRoute.POST("/quote", controller.QuoteServerPrice)
		}
		serverRoute := apiRouter.Group("/server")
		serverRoute.Use(middleware.UserAuth())
		{
			serverRoute.POST("/orders", middleware.CriticalRateLimit(), controller.PurchaseServer)
			serverRoute.GET("/orders", controller.GetServerOrders)
			serverRoute.GET("/orders/:trade_no", controller.GetServerOrderStatus)
			serverRoute.GET("/pay-methods", controller.GetServerPayMethods)
			serverRoute.POST("/pay/alipay", middleware.CriticalRateLimit(), controller.RequestServerAlipayPay)
			serverRoute.GET("/instances", controller.GetServerInstances)
			serverRoute.GET("/instances/:id", controller.GetServerInstance)
			serverRoute.GET("/instances/:id/metrics", controller.GetServerInstanceMetrics)
			serverRoute.GET("/instances/:id/events", controller.GetServerInstanceEvents)
			serverRoute.POST("/instances/:id/start", middleware.CriticalRateLimit(), controller.StartServerInstance)
			serverRoute.POST("/instances/:id/stop", middleware.CriticalRateLimit(), controller.StopServerInstance)
			serverRoute.POST("/instances/:id/reboot", middleware.CriticalRateLimit(), controller.RebootServerInstance)
			serverRoute.POST("/instances/:id/reset-password", middleware.CriticalRateLimit(), controller.ResetServerInstancePassword)
			serverRoute.POST("/instances/:id/renew", middleware.CriticalRateLimit(), controller.RenewServerInstance)
			serverRoute.POST("/instances/:id/release", middleware.CriticalRateLimit(), controller.ReleaseServerInstance)
		}
		// 人工开台（服务器分配）：把已付款的订单变成可用机器，只给管理员。
		// 路径带 provisioning 前缀，和用户侧的 /server 路由不重叠。
		serverProvisionRoute := apiRouter.Group("/server/provisioning")
		serverProvisionRoute.Use(middleware.AdminAuth())
		{
			serverProvisionRoute.GET("/orders", controller.GetServerProvisionOrders)
			serverProvisionRoute.GET("/summary", controller.GetServerProvisionSummary)
			serverProvisionRoute.POST("/assign", middleware.CriticalRateLimit(), controller.AssignServerOrder)
		}
		// 云服务器订单的支付回调：无需登录（支付宝服务器调用），但必须 RSA2 验签。
		apiRouter.POST("/server/pay/alipay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.ServerAlipayNotify)
		apiRouter.GET("/server/pay/alipay/notify", middleware.PaymentNotifyRateLimit(), controller.ServerAlipayNotify)

		// 条件路由的运维开关（暂停 / 恢复「按时长改派到豆包」那条规则）。
		//
		// 调用方是客户端软件（每个操作员机器上的一份安装包），所以**不用**
		// AdminAuth：把管理员凭据放进客户端安装包，泄露一次等于交出整个后台。
		// 改用单用途共享密钥（option VideoRouteMaintenanceKey），
		// 只授权「暂停/恢复路由规则」这一件事，可随时轮换。
		// 密钥未配置时接口返回 404（见 middleware/route_maintenance.go）。
		videoRouteMaintenance := apiRouter.Group("/video-route/maintenance")
		videoRouteMaintenance.Use(middleware.VideoRouteMaintenanceRateLimit())
		videoRouteMaintenance.Use(middleware.VideoRouteMaintenanceAuth())
		{
			videoRouteMaintenance.GET("/doubao", controller.GetDoubaoRouteMaintenance)
			videoRouteMaintenance.POST("/doubao/pause", controller.PauseDoubaoRoute)
			videoRouteMaintenance.POST("/doubao/resume", controller.ResumeDoubaoRoute)
		}

		// 管理后台的「跨模型改派」开关（登录态，与上面共用同一套 controller）。
		//
		// 为什么要两个入口：上面的共享密钥是给客户端安装包用的（不可能把管理员
		// 凭据塞进安装包），而运营在网页后台按按钮时用的是登录态。
		// 逻辑只有一份，鉴权与审计来源由包装层区分。
		videoRouteAdmin := apiRouter.Group("/video-route")
		videoRouteAdmin.Use(middleware.AdminAuth())
		{
			videoRouteAdmin.GET("/policy", controller.GetDoubaoRouteAdmin)
			videoRouteAdmin.POST("/policy/pause", controller.PauseDoubaoRouteAdmin)
			videoRouteAdmin.POST("/policy/resume", controller.ResumeDoubaoRouteAdmin)
			// 按源模型单独开关「按时长改派」（促销族里只停活动版用）。
			videoRouteAdmin.POST("/policy/source-switch", controller.SetSourceSecondsRuleAdmin)

			// 豆包链路开关（合并卡片）：一个开关 = 豆包通不通 ——
			// 关闭同时停规则一（特惠/活动留常驻）并强制规则五（seedance-2-5 → 班车），
			// 到下一个北京时间凌晨 2 点自动恢复。
			videoRouteAdmin.POST("/policy/doubao-link", controller.SetDoubaoLinkAdmin)

			// 规则六（短时长分流）链路开关：一个开关 = 心笔内链通不通 ——
			// 打开：≤阈值（默认 10 秒）的短请求改派 wan3.0-720p-特惠-内链；
			// 关闭：规则六对所有源模型停用，短请求走原链路。
			// 与豆包开关的差异：心笔没有按天重置的额度，纯手动开关，
			// 无自动恢复时间、无接单计数、不强制故障转移。
			videoRouteAdmin.POST("/policy/short-link", controller.SetShortLinkAdmin)

			// 规则六的「有赞标准版兜底」开关（2026-11-14）：
			// 打开后心笔接不住的短请求改派 wan3.0-video；
			// 与心笔开关并列时心笔优先，两个都关闭走原链路。
			videoRouteAdmin.POST("/policy/short-youzan-link", controller.SetShortYouzanLinkAdmin)

			// 规则六的第三条分支「CapCut 快速档」开关（2026-11-14）：
			// 打开后短请求改派 cp-seedance-2.0fast。
			// ⚠️ 三条分支同时只能有一条生效 —— 打开本条时自动关掉心笔与有赞。
			videoRouteAdmin.POST("/policy/short-capcut-link", controller.SetShortCapcutLinkAdmin)

			// 规则七（电商/产品推荐内容 → cp-seedance-2.5flash，2026-11-14）：
			// 总开关 + 参数条件勾选（9:16 / 时长阈值）+ 关键词表。
			// 带参考视频的请求一律不路由（老板明确要求），与开关无关。
			videoRouteAdmin.POST("/policy/ecommerce", controller.SetEcommerceRouteAdmin)
			videoRouteAdmin.POST("/policy/ecommerce/conditions", controller.SetEcommerceConditionsAdmin)
			videoRouteAdmin.POST("/policy/ecommerce/keywords", controller.SetEcommerceKeywordsAdmin)

			// 豆包链路的「最多接单数」（2026-11-13 老板定）：
			// 豆包链路上游给的是运营自己掌握的一小批额度，设成 3 就是
			// 「这批只接 3 单，接满自动关闭，我手动再打开时又来 3 单」。
			// 设置上限会把接单计数清零（= 给一批新额度）。
			videoRouteAdmin.POST("/policy/doubao-link-max-tasks", controller.SetDoubaoLinkMaxTasksAdmin)

			// 规则一的可选比例条件（后台勾选）：勾上 = 只有指定比例的请求
			// 才改派目标链路；取消 = 不限比例。与豆包链路开关是两个独立动作，
			// 前者决定「哪些请求有资格」，后者决定「这条路通不通」。
			videoRouteAdmin.POST("/policy/ratio-gate", controller.SetSecondsRuleRatioGateAdmin)

			// 规则五（故障转移）：源链路额度用完 / 渠道被关 → 改派到备胎。
			//
			// 与上面四个接口分开：规则五的触发源是**渠道状态**而不是请求特征，
			// 语义与运营动作都不同（需要额外的「人工标记源链路不可用」）。
			videoRouteAdmin.GET("/policy/failover", controller.GetFailoverPolicyAdmin)
			videoRouteAdmin.POST("/policy/failover/switch", controller.SetFailoverSwitchAdmin)
			videoRouteAdmin.POST("/policy/failover/models", controller.SetFailoverModelsAdmin)
			// 备选备胎（CapCut 2.5 Flash，480p 生成 + 超分 720P 交付）。
			// 与主开关分开：主开关决定「要不要改派」，这个决定「改派到哪一条」。
			videoRouteAdmin.POST("/policy/failover/alt", controller.SetFailoverAltAdmin)
			videoRouteAdmin.POST("/policy/failover/unavailable", controller.MarkFailoverSourceUnavailableAdmin)
			videoRouteAdmin.POST("/policy/failover/available", controller.ClearFailoverSourceUnavailableAdmin)
		}

		//apiRouter.GET("/midjourney", controller.GetMidjourney)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.HeaderNavModuleAuth("pricing"), controller.GetPricing)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
		{
			perfMetricsRoute.GET("/summary", controller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", controller.GetPerfMetrics)
		}
		apiRouter.GET("/rankings", middleware.HeaderNavModuleAuth("rankings"), controller.GetRankings)
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.ResetPassword)
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.GET("/oauth/state", middleware.CriticalRateLimit(), controller.GenerateOAuthCode)
		apiRouter.POST("/oauth/email/bind", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.EmailBind)
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", middleware.CriticalRateLimit(), controller.WeChatAuth)
		apiRouter.POST("/oauth/wechat/bind", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.WeChatBind)
		apiRouter.GET("/oauth/telegram/login", middleware.CriticalRateLimit(), controller.TelegramLogin)
		apiRouter.GET("/oauth/telegram/bind", middleware.CriticalRateLimit(), controller.TelegramBind)
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", middleware.CriticalRateLimit(), controller.HandleOAuth)
		apiRouter.GET("/ratio_config", middleware.CriticalRateLimit(), controller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.StripeWebhook)
		apiRouter.POST("/creem/webhook", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.CreemWebhook)
		apiRouter.POST("/waffo/webhook", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.WaffoWebhook)
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.WaffoPancakeWebhook)

		// Universal secure verification routes
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.UniversalVerify)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/register", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), controller.Register)
			userRoute.POST("/login", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), controller.Login)
			userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.Verify2FALogin)
			userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, controller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.CriticalRateLimit(), controller.TokenLog)
			userRoute.GET("/logout", controller.Logout)
			userRoute.POST("/epay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.EpayNotify)
			userRoute.GET("/epay/notify", middleware.PaymentNotifyRateLimit(), controller.EpayNotify)
			userRoute.POST("/alipay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.AlipayNotify)
			userRoute.GET("/alipay/notify", middleware.PaymentNotifyRateLimit(), controller.AlipayNotify)
			userRoute.GET("/alipay/return", controller.AlipayReturn)
			userRoute.POST("/alipay/return", anonymousRequestBodyLimit, controller.AlipayReturn)
			userRoute.POST("/vpn/epay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.VpnEpayNotify)
			// v2rayN 订阅是匿名 GET，靠随机令牌寻址；令牌不可枚举，且响应不缓存。
			userRoute.GET("/vpn/sub/:token", controller.GetVpnSubscription)
			userRoute.GET("/vpn/epay/notify", middleware.PaymentNotifyRateLimit(), controller.VpnEpayNotify)
			userRoute.POST("/vpn/alipay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.VpnAlipayNotify)
			userRoute.GET("/vpn/alipay/notify", middleware.PaymentNotifyRateLimit(), controller.VpnAlipayNotify)
			userRoute.GET("/groups", controller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.GET("/self/groups", controller.GetUserGroups)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.GET("/models", controller.GetUserModels)
				selfRoute.PUT("/self", middleware.CriticalRateLimit(), controller.UpdateSelf)
				selfRoute.DELETE("/self", controller.DeleteSelf)
				selfRoute.GET("/token", controller.GenerateAccessToken)
				selfRoute.GET("/passkey", controller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", controller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", controller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", controller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", controller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", controller.PasskeyDelete)
				selfRoute.GET("/aff", controller.GetAffCode)
				selfRoute.GET("/wallet", controller.GetUserWallet)
				selfRoute.GET("/topup/info", controller.GetTopUpInfo)
				selfRoute.GET("/topup/self", controller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.CriticalRateLimit(), controller.TopUp)
				selfRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestEpay)
				selfRoute.POST("/vpn/order", middleware.CriticalRateLimit(), controller.RequestVpnOrder)
				selfRoute.GET("/vpn/orders", controller.GetVpnMyOrders)
				selfRoute.GET("/vpn/key", controller.GetVpnMyKey)
				selfRoute.POST("/vpn/reset", middleware.CriticalRateLimit(), controller.ResetVpnSubscription)
				selfRoute.POST("/amount", controller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestStripePay)
				selfRoute.POST("/stripe/amount", controller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestCreemPay)
				selfRoute.POST("/waffo/amount", controller.RequestWaffoAmount)
				selfRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPay)
				selfRoute.POST("/waffo-pancake/amount", controller.RequestWaffoPancakeAmount)
				selfRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPancakePay)
				selfRoute.POST("/aff_transfer", controller.TransferAffQuota)
				selfRoute.PUT("/setting", controller.UpdateUserSetting)

				// 2FA routes
				selfRoute.GET("/2fa/status", controller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", controller.Setup2FA)
				selfRoute.POST("/2fa/enable", controller.Enable2FA)
				selfRoute.POST("/2fa/disable", controller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", controller.RegenerateBackupCodes)

				// Check-in routes
				selfRoute.GET("/checkin", controller.GetCheckinStatus)
				selfRoute.POST("/checkin", middleware.TurnstileCheck(), controller.DoCheckin)

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", controller.GetUserOAuthBindings)
				selfRoute.DELETE("/oauth/bindings/:provider_id", controller.UnbindCustomOAuth)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", controller.GetAllUsers)
				adminRoute.GET("/topup", controller.GetAllTopUps)
				adminRoute.POST("/topup/complete", controller.AdminCompleteTopUp)
				adminRoute.GET("/search", controller.SearchUsers)
				adminRoute.GET("/:id/oauth/bindings", controller.GetUserOAuthBindingsByAdmin)
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", controller.UnbindCustomOAuthByAdmin)
				adminRoute.DELETE("/:id/bindings/:binding_type", controller.AdminClearUserBinding)
				adminRoute.GET("/:id", controller.GetUser)
				adminRoute.POST("/", controller.CreateUser)
				adminRoute.POST("/manage", controller.ManageUser)
				adminRoute.PUT("/", controller.UpdateUser)
				adminRoute.DELETE("/:id", controller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", controller.AdminResetPasskey)

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", controller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", controller.AdminDisable2FA)
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(middleware.UserAuth())
		{
			subscriptionRoute.GET("/plans", controller.GetSubscriptionPlans)
			subscriptionRoute.GET("/self", controller.GetSubscriptionSelf)
			subscriptionRoute.PUT("/self/preference", controller.UpdateSubscriptionPreference)
			subscriptionRoute.POST("/balance/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestBalancePay)
			subscriptionRoute.POST("/epay/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestEpay)
			subscriptionRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestStripePay)
			subscriptionRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestCreemPay)
			subscriptionRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.SubscriptionRequestWaffoPancakePay)
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(middleware.AdminAuth())
		{
			subscriptionAdminRoute.GET("/plans", controller.AdminListSubscriptionPlans)
			subscriptionAdminRoute.POST("/plans", controller.AdminCreateSubscriptionPlan)
			subscriptionAdminRoute.PUT("/plans/:id", controller.AdminUpdateSubscriptionPlan)
			subscriptionAdminRoute.PATCH("/plans/:id", controller.AdminUpdateSubscriptionPlanStatus)
			subscriptionAdminRoute.POST("/bind", controller.AdminBindSubscription)

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", controller.AdminListUserSubscriptions)
			subscriptionAdminRoute.POST("/users/:id/subscriptions", controller.AdminCreateUserSubscription)
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", controller.AdminInvalidateUserSubscription)
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", controller.AdminDeleteUserSubscription)
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", middleware.PaymentNotifyRateLimit(), anonymousRequestBodyLimit, controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/notify", middleware.PaymentNotifyRateLimit(), controller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/return", controller.SubscriptionEpayReturn)
		apiRouter.POST("/subscription/epay/return", anonymousRequestBodyLimit, controller.SubscriptionEpayReturn)
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
			optionRoute.POST("/payment_compliance", controller.ConfirmPaymentCompliance)
			optionRoute.GET("/channel_affinity_cache", controller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", controller.ClearChannelAffinityCache)
			optionRoute.POST("/rest_model_ratio", controller.ResetModelRatio)
			optionRoute.POST("/migrate_console_setting", controller.MigrateConsoleSetting) // 用于迁移检测的旧键，下个版本会删除
			optionRoute.GET("/waffo-pancake/catalog", controller.ListWaffoPancakeCatalog)
			optionRoute.POST("/waffo-pancake/pair", controller.CreateWaffoPancakePair)
			optionRoute.POST("/waffo-pancake/save", controller.SaveWaffoPancake)
			optionRoute.POST("/waffo-pancake/subscription-product", controller.CreateWaffoPancakeSubscriptionProduct)
			optionRoute.GET("/waffo-pancake/subscription-product-options", controller.ListWaffoPancakeSubscriptionProductOptions)
		}

		// Custom OAuth provider management (root only)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(middleware.RootAuth())
		{
			customOAuthRoute.POST("/discovery", controller.FetchCustomOAuthDiscovery)
			customOAuthRoute.GET("/", controller.GetCustomOAuthProviders)
			customOAuthRoute.GET("/:id", controller.GetCustomOAuthProvider)
			customOAuthRoute.POST("/", controller.CreateCustomOAuthProvider)
			customOAuthRoute.PUT("/:id", controller.UpdateCustomOAuthProvider)
			customOAuthRoute.DELETE("/:id", controller.DeleteCustomOAuthProvider)
		}
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(middleware.RootAuth())
		{
			performanceRoute.GET("/stats", controller.GetPerformanceStats)
			performanceRoute.DELETE("/disk_cache", controller.ClearDiskCache)
			performanceRoute.POST("/reset_stats", controller.ResetPerformanceStats)
			performanceRoute.POST("/gc", controller.ForceGC)
			performanceRoute.GET("/logs", controller.GetLogFiles)
			performanceRoute.DELETE("/logs", controller.CleanupLogFiles)
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(middleware.RootAuth())
		{
			ratioSyncRoute.GET("/channels", controller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", controller.FetchUpstreamRatios)
		}
		registerChannelRoutes(apiRouter)
		registerAuthzRoutes(apiRouter)
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", middleware.SearchRateLimit(), controller.SearchTokens)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.POST("/:id/key", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKey)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
			tokenRoute.POST("/batch", controller.DeleteTokenBatch)
			tokenRoute.POST("/batch/keys", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKeysBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuthReadOnly())
			{
				tokenUsageRoute.GET("/", controller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", controller.GetAllRedemptions)
			redemptionRoute.GET("/search", controller.SearchRedemptions)
			redemptionRoute.GET("/:id", controller.GetRedemption)
			redemptionRoute.POST("/", controller.AddRedemption)
			redemptionRoute.PUT("/", controller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", controller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", controller.DeleteRedemption)
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		// Legacy synchronous direct-delete route used only by the classic frontend.
		// TODO: remove once the classic frontend is removed; the default frontend uses /system-task/log-cleanup.
		logRoute.DELETE("/", middleware.RootAuth(), controller.DeleteHistoryLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), controller.GetLogsSelfStat)
		logRoute.GET("/channel_affinity_usage_cache", middleware.AdminAuth(), controller.GetChannelAffinityUsageCacheStats)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), controller.GetUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), middleware.SearchRateLimit(), controller.SearchUserLogs)

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(middleware.RootAuth())
		{
			systemTaskRoute.POST("/log-cleanup", controller.CreateLogCleanupSystemTask)
			systemTaskRoute.GET("/list", controller.ListSystemTasks)
			systemTaskRoute.GET("/current", controller.GetCurrentSystemTask)
			systemTaskRoute.GET("/:task_id", controller.GetSystemTask)
		}
		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(middleware.RootAuth())
		{
			systemInfoRoute.GET("/instances", controller.ListSystemInstances)
		}

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
		dataRoute.GET("/users", middleware.AdminAuth(), controller.GetQuotaDatesByUser)
		dataRoute.GET("/self", middleware.UserAuth(), controller.GetUserQuotaDates)
		dataRoute.GET("/flow", middleware.AdminAuth(), controller.GetAllFlowQuotaDates)
		dataRoute.GET("/flow/self", middleware.UserAuth(), controller.GetUserFlowQuotaDates)

		logRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			logRoute.GET("/token", middleware.TokenAuthReadOnly(), controller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), controller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), controller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), controller.GetUserTask)
			taskRoute.GET("/", middleware.AdminAuth(), controller.GetAllTask)
			// 任务执行流水（提交快照/重试失败/状态迁移），排障用，仅管理员可见。
			taskRoute.GET("/logs", middleware.AdminAuth(), controller.GetTaskLogs)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.GET("/", controller.GetAllVendors)
			vendorRoute.GET("/search", controller.SearchVendors)
			vendorRoute.GET("/:id", controller.GetVendorMeta)
			vendorRoute.POST("/", controller.CreateVendorMeta)
			vendorRoute.PUT("/", controller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", controller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

		// Deployments (model deployment management)
		deploymentsRoute := apiRouter.Group("/deployments")
		deploymentsRoute.Use(middleware.AdminAuth())
		{
			deploymentsRoute.GET("/settings", controller.GetModelDeploymentSettings)
			deploymentsRoute.POST("/settings/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/", controller.GetAllDeployments)
			deploymentsRoute.GET("/search", controller.SearchDeployments)
			deploymentsRoute.POST("/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/hardware-types", controller.GetHardwareTypes)
			deploymentsRoute.GET("/locations", controller.GetLocations)
			deploymentsRoute.GET("/available-replicas", controller.GetAvailableReplicas)
			deploymentsRoute.POST("/price-estimation", controller.GetPriceEstimation)
			deploymentsRoute.GET("/check-name", controller.CheckClusterNameAvailability)
			deploymentsRoute.POST("/", controller.CreateDeployment)

			deploymentsRoute.GET("/:id", controller.GetDeployment)
			deploymentsRoute.GET("/:id/logs", controller.GetDeploymentLogs)
			deploymentsRoute.GET("/:id/containers", controller.ListDeploymentContainers)
			deploymentsRoute.GET("/:id/containers/:container_id", controller.GetContainerDetails)
			deploymentsRoute.PUT("/:id", controller.UpdateDeployment)
			deploymentsRoute.PUT("/:id/name", controller.UpdateDeploymentName)
			deploymentsRoute.POST("/:id/extend", controller.ExtendDeployment)
			deploymentsRoute.DELETE("/:id", controller.DeleteDeployment)
		}
	}
}
