package controller

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 条件路由的运维开关：暂停 / 恢复「按时长改派到豆包」这条规则。
//
// 谁在用：客户端（cron_video）账号管理面板里的「标记今日额度已用完」按钮。
// 点一下 = 暂停到**北京时间次日 0 点**，到点自动恢复 —— 不需要有人记得回来打开。
//
// 为什么不用「关渠道」实现：目标链路所在的渠道（ch20 漫屋ArcReel）上还挂着
// 另外 7 个与豆包无关的模型（gemini-web-video / Nano Banana Pro / wan2.7-图生 …）。
// 渠道级禁用会把它们一起停掉 —— 客户点了「豆包没额度」，结果连 Nano Banana 都不能用。
// 停路由规则则只影响「30 秒 → 豆包」这一条，其余模型完全不受影响。
//
// 为什么不用 Enabled=false 实现：Enabled 是两条规则的总开关，
// 关掉会把「9:16 → wan3」也一起停。所以用独立的 SecondsRulePausedUntil。

// beijingZone 固定 UTC+8。
//
// 用 FixedZone 而不是 LoadLocation("Asia/Shanghai")：容器镜像里不一定装了 tzdata，
// LoadLocation 会返回错误，届时时间会退化成 UTC，暂停就变成「到 UTC 0 点恢复」
// —— 比北京时间晚 8 小时，等于多停了半天。中国不实行夏令时，固定偏移是准确的。
var beijingZone = time.FixedZone("CST", 8*3600)

// maxDoubaoRoutePauseDuration 单次暂停的上限。
//
// 防手滑：客户端传一个离谱的时间戳（比如把毫秒当秒传）会把路由停几十年，
// 而这种故障**没有任何报错**，只会悄悄长期跑在更贵的链路上。
const maxDoubaoRoutePauseDuration = 30 * 24 * time.Hour

// nextBeijingMidnight 返回「严格晚于 now 的下一个北京时间 0 点」。
//
// 真正的计算在 setting 包（额度探针也要用同一份，两处各写一份必然会漂）。
// 这里只做 time.Time 包装，供 controller 内部与既有测试使用。
func nextBeijingMidnight(now time.Time) time.Time {
	return time.Unix(seedanceroute_setting.NextBeijingMidnight(now.Unix()), 0).In(beijingZone)
}

// parsePauseUntil 解析暂停请求，返回最终的截止时间。
//
// 抽成纯函数是为了能直接测边界：过去的时间戳必须拒绝（静默接受会得到
// 「看起来成功但完全没暂停」的结果），超 30 天的也必须拒绝（把毫秒当秒
// 传会把路由停几十年，而且没有任何报错）。
func parsePauseUntil(req pauseDoubaoRouteRequest, now time.Time) (int64, error) {
	until := req.Until
	if until <= 0 {
		// 不传 = 北京时间次日 0 点，对应「今天额度用完了」这个最常见的场景。
		until = nextBeijingMidnight(now).Unix()
	}
	if until <= now.Unix() {
		return 0, errors.New("until must be in the future")
	}
	if time.Duration(until-now.Unix())*time.Second > maxDoubaoRoutePauseDuration {
		return 0, errors.New("until is too far in the future (max 30 days)")
	}
	return until, nil
}

// pauseDoubaoRouteRequest 是暂停请求体。整个 body 可以省略。
type pauseDoubaoRouteRequest struct {
	// Until 暂停截止时间（Unix 秒）。0 或缺省 = 北京时间次日 0 点。
	Until int64 `json:"until"`
}

// PauseDoubaoRoute 暂停「按时长改派到豆包」这条规则（共享密钥通道，给客户端用）。
//
// 幂等：重复调用只是把截止时间往后推，不会出错。
func PauseDoubaoRoute(c *gin.Context) {
	pauseDoubaoRoute(c, "maintenance_key", doubaoRouteState)
}

// PauseDoubaoRouteAdmin 与 PauseDoubaoRoute 同一实现，只是鉴权与审计来源不同。
//
// 为什么要两个入口：客户端安装包里只有单用途共享密钥（不可能塞管理员凭据），
// 而管理后台是登录态。逻辑只有一份，鉴权和审计来源由包装层区分。
func PauseDoubaoRouteAdmin(c *gin.Context) {
	pauseDoubaoRoute(c, "admin", doubaoRouteAdminState)
}

func pauseDoubaoRoute(c *gin.Context, source string, state func() gin.H) {
	var req pauseDoubaoRouteRequest
	// body 允许为空（客户端通常什么都不传，让服务端算次日 0 点）。
	// io.EOF 表示「没有 body」，不是错误；其它解析错误要明确报出来，不能猜。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}

	until, err := parsePauseUntil(req, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if err := saveDoubaoRoutePause(until); err != nil {
		common.SysLog("pause doubao route failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.doubao_pause", map[string]interface{}{
		"paused_until": until,
		"source":       source,
	})
	common.ApiSuccess(c, state())
}

// ResumeDoubaoRoute 取消暂停，立刻恢复「按时长改派到豆包」（共享密钥通道）。
func ResumeDoubaoRoute(c *gin.Context) {
	resumeDoubaoRoute(c, "maintenance_key", doubaoRouteState)
}

// ResumeDoubaoRouteAdmin 是管理后台用的恢复入口（登录态）。
func ResumeDoubaoRouteAdmin(c *gin.Context) {
	resumeDoubaoRoute(c, "admin", doubaoRouteAdminState)
}

func resumeDoubaoRoute(c *gin.Context, source string, state func() gin.H) {
	if err := saveDoubaoRoutePause(0); err != nil {
		common.SysLog("resume doubao route failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.doubao_resume", map[string]interface{}{
		"source": source,
	})
	common.ApiSuccess(c, state())
}

// GetDoubaoRouteMaintenance 查询当前状态（暂停到什么时候、三条规则各自是否生效）。
func GetDoubaoRouteMaintenance(c *gin.Context) {
	common.ApiSuccess(c, doubaoRouteState())
}

// GetDoubaoRouteAdmin 是管理后台用的状态查询：在客户端那份字段之上，
// 补上「谁停的、为什么停、改派规则本身长什么样」。
func GetDoubaoRouteAdmin(c *gin.Context) {
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// saveDoubaoRoutePause 把暂停截止时间写进 option 并落库。
//
// 走 model.UpdateOption 而不是直接改 setting 包的内存状态：UpdateOption 会
// 先写库、再经 updateOptionMap 分发到 UpdateFromJSONString。少任何一步都会
// 出现「接口返回成功但重启后失效」或「内存改了库没改」的不一致。
//
// until=0 表示恢复。
func saveDoubaoRoutePause(until int64) error {
	// 用 WithManualPause 而不是 WithSecondsRulePausedUntil：要把来源标成
	// manual，自动探针才不会覆盖管理员手动设的截止时间（见 quota.go）。
	policy := seedanceroute_setting.WithManualPause(until)
	raw, err := common.Marshal(policy)
	if err != nil {
		return err
	}
	return model.UpdateOption(seedanceroute_setting.OptionKey, string(raw))
}

// doubaoRouteState 是给客户端看的当前状态。
//
// 字段命名刻意直白：客户端要用 paused 决定按钮显示「已暂停/暂停」，
// 用 paused_until_text 直接展示给人看（不让客户端自己算时区）。
func doubaoRouteState() gin.H {
	until := seedanceroute_setting.SecondsRulePausedUntil()
	state := gin.H{
		"paused":               seedanceroute_setting.IsSecondsRulePaused(),
		"paused_until":         until,
		"seconds_rule_enabled": seedanceroute_setting.IsEnabled(),
		"ratio_rule_enabled":   seedanceroute_setting.IsRatioRouteEnabled(),
		"frames_rule_enabled":  seedanceroute_setting.IsFramesRouteEnabled(),
	}
	if until > 0 {
		state["paused_until_text"] = time.Unix(until, 0).In(beijingZone).Format("2006-01-02 15:04:05")
	} else {
		state["paused_until_text"] = ""
	}
	return state
}

// doubaoRouteAdminState 是管理后台用的状态：客户端那份字段 + 暂停来源/原因 +
// 改派规则本身。
//
// 为什么要带上规则配置：后台按钮旁边必须能读出「改派的是什么」
// （谁 → 谁、几秒触发）。只给一个 paused 布尔值，运营看到的是一个
// 没有主语的开关 —— 不敢按，也不知道按了影响哪些请求。
func doubaoRouteAdminState() gin.H {
	policy := seedanceroute_setting.Get()
	state := doubaoRouteState()
	state["pause_source"] = policy.PauseSource
	state["pause_reason"] = policy.PauseReason
	state["source_model"] = policy.SourceModel
	state["target_model"] = policy.TargetModel
	state["trigger_seconds"] = policy.TriggerSeconds
	state["ratio_target_model"] = policy.RatioTargetModel
	state["trigger_ratio"] = policy.TriggerRatio
	state["frames_target_model"] = policy.FramesTargetModel
	state["updated_at"] = policy.UpdatedAt
	return state
}
