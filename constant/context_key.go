package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"

	// ContextKeyRouteOriginModel 是「条件路由改派前」用户真正请求的模型名。
	//
	// 改派（seedance2.5-特惠 → seedance-2-5 / wan3.0-720p-特惠）会把请求体的
	// model 字段换成目标模型，于是 RelayInfo.OriginModelName 与 logs.model_name
	// 都变成了目标模型 —— 客户在自己的日志里就会看到「我调 A，怎么记成 B」。
	// 这个键把源模型名一路带到日志层。
	//
	// **计费与展示都用它**（老板 2026-10-30 定稿）：客户请求哪个模型，就按哪个
	// 模型的价格收 —— 改派只是内部调度，不改变对客价格。
	// 取值入口是 RelayInfo.BillingModelName / service.taskBillingModelName，
	// 不要直接读这个键去算钱。
	ContextKeyRouteOriginModel ContextKey = "route_origin_model"

	// ContextKeyTaskRequestParams 是「客户原始请求参数」的摘要（管理员日志可见）。
	//
	// 必须在条件路由改写 body 之前捕获：改派会把 model 换成内部目标模型，
	// 事后从 body 反推只会得到「客户根本没用过的参数」。
	// 写入方 middleware.CaptureTaskRequestParams，读取方是消费/错误日志落库处。
	ContextKeyTaskRequestParams ContextKey = "task_request_params"

	// ContextKeyRouteBillingSeconds 是条件路由改派时「源模型口径的计费秒数」。
	//
	// 为什么需要单独记：计费按源模型算，而源模型（seedance2.5-特惠）是按秒计价的；
	// 改派到豆包后 adaptor 换成 manwu，它对按次模型返回 nil 倍率 —— 少了 seconds
	// 这一项，30 秒的请求会只按 1 秒收钱。秒数在路由中间件里已经解析过（规则一
	// 就是靠它判定的），记进 context 由 relay_task.go 补进 OtherRatios。
	//
	// 只在「目标 adaptor 没给出 seconds」时生效，不覆盖 adaptor 自己的判断。
	ContextKeyRouteBillingSeconds ContextKey = "route_billing_seconds"

	// ContextKeyRouteBillingSizeRatio 是条件路由改派时「源模型口径的 size 倍率」。
	//
	// 与 ContextKeyRouteBillingSeconds 是同一个问题的两面：计费按源模型
	// （seedance2.5-特惠）算 —— 720P ¥0.20/秒、1080P ¥0.30/秒（1.5 倍）；
	// 而规则六的改派目标 wan3.0-video 三档同价，它的 adaptor 会给 size = 1.0。
	//
	// 不覆盖就会按 ¥0.20/秒 收 1080P（应收 ¥0.30），**少收 33% 且没有任何报错**。
	//
	// 语义是**覆盖**而不是相乘：adaptor 给的 1.0 是「目标模型的档位差价」，
	// 与源模型的档位差价是同一维度，不是两个可叠加的倍率。
	ContextKeyRouteBillingSizeRatio ContextKey = "route_billing_size_ratio"

	// ContextKeyRouteSuppressTaskRatios 是「屏蔽目标 adaptor 给出的计费倍率」。
	//
	// 为什么需要：计费口径 = 客户请求的源模型（见 ContextKeyRouteOriginModel）。
	// 当源模型是**一口价**（ModelPrice 按次，如 seedance-2-5 ¥3.50/次），
	// 而改派目标 adaptor 恰好按自己的习惯返回 {"seconds": N, "size": R} 时，
	// 这些倍率会把一口价乘成 ¥3.50×N —— 超收 N 倍，账单上却看不出异常。
	//
	// 典型案例（2026-11-14 加）：规则五把 seedance-2-5 降级到 CapCut
	// cp-seedance-2.5flash。CapCut adaptor 一律按秒给倍率（它自己就是按秒商品），
	// 而客户买的是 seedance-2-5 的一口价。不屏蔽就会 5 秒的单子收 ¥17.50。
	//
	// ⚠️ 与 constant.IsTaskPerCallModel 的分工：
	//   IsTaskPerCallModel 判「这个**模型**是不是按次商品」（判据是模型名）；
	//   本键表达「这条**请求**虽然跑在按秒链路上，但计费口径是一口价」——
	//   模型名判不出来（seedance-2-5 的按次性不在能力组里），只能由路由层标记。
	//
	// 语义是**清空**而不是把倍率设成 1：留着 {"seconds": 1} 会让日志与
	// BillingContext 快照看起来像「按秒计费」，排查时被带偏。
	ContextKeyRouteSuppressTaskRatios ContextKey = "route_suppress_task_ratios"

	// ContextKeyPromptCompressFrom / To 是提示词压缩的「原长度 → 压缩后长度」。
	//
	// 为什么要记下来：压缩改写了客户的创作描述，而任务照样成功、账单照收 ——
	// 客户反馈「成片和提示词不符」时，支持侧只能靠任务侧的证据回答
	// 「这条请求在生产上被压过没有」。只记管理员可见的 PrivateData，
	// 不进对客字段（客户不该看到「我们改过你的提示词」这类实现细节）。
	ContextKeyPromptCompressFrom ContextKey = "prompt_compress_from"
	ContextKeyPromptCompressTo   ContextKey = "prompt_compress_to"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId      ContextKey = "id"
	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyUserQuota   ContextKey = "user_quota"
	ContextKeyUserStatus  ContextKey = "user_status"
	ContextKeyUserEmail   ContextKey = "user_email"
	ContextKeyUserGroup   ContextKey = "user_group"
	ContextKeyUsingGroup  ContextKey = "group"
	ContextKeyUserName    ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"
	ContextKeyIsStream ContextKey = "is_stream"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// a manage/operation audit log inside the handler. When set, the admin-audit
	// fallback in authHelper (finishAdminAudit) skips its record to avoid
	// duplicate entries.
	ContextKeyAuditLogged ContextKey = "audit_logged"
)
