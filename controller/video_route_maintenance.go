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
// 用 `<=` 的直觉写法在恰逢 0 点整时会把截止时间算成「现在」，等于没暂停；
// 这里一律取次日 0 点（nextBeijingMidnightStrictlyAfter 同口径）。
func nextBeijingMidnight(now time.Time) time.Time {
	local := now.In(beijingZone)
	// time.Date 会自己处理月末 / 年末进位（如 10-31 + 1 天 = 11-01）。
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, beijingZone)
}

// pauseDoubaoRouteRequest 是暂停请求体。整个 body 可以省略。
type pauseDoubaoRouteRequest struct {
	// Until 暂停截止时间（Unix 秒）。0 或缺省 = 北京时间次日 0 点。
	Until int64 `json:"until"`
}

// PauseDoubaoRoute 暂停「按时长改派到豆包」这条规则。
//
// 幂等：重复调用只是把截止时间往后推，不会出错。
func PauseDoubaoRoute(c *gin.Context) {
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

	now := time.Now()
	until := req.Until
	if until <= 0 {
		until = nextBeijingMidnight(now).Unix()
	}
	// 拒绝过去的时间戳：说明调用方时钟或单位搞错了（常见是把毫秒当秒传）。
	// 静默接受会得到「看起来成功但完全没暂停」的结果，比报错更难排查。
	if until <= now.Unix() {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "until must be in the future",
		})
		return
	}
	if time.Duration(until-now.Unix())*time.Second > maxDoubaoRoutePauseDuration {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "until is too far in the future (max 30 days)",
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
		"source":       "maintenance_key",
	})
	common.ApiSuccess(c, doubaoRouteState())
}

// ResumeDoubaoRoute 取消暂停，立刻恢复「按时长改派到豆包」。
func ResumeDoubaoRoute(c *gin.Context) {
	if err := saveDoubaoRoutePause(0); err != nil {
		common.SysLog("resume doubao route failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.doubao_resume", map[string]interface{}{
		"source": "maintenance_key",
	})
	common.ApiSuccess(c, doubaoRouteState())
}

// GetDoubaoRouteMaintenance 查询当前状态（暂停到什么时候、两条规则各自是否生效）。
func GetDoubaoRouteMaintenance(c *gin.Context) {
	common.ApiSuccess(c, doubaoRouteState())
}

// saveDoubaoRoutePause 把暂停截止时间写进 option 并落库。
//
// 走 model.UpdateOption 而不是直接改 setting 包的内存状态：UpdateOption 会
// 先写库、再经 updateOptionMap 分发到 UpdateFromJSONString。少任何一步都会
// 出现「接口返回成功但重启后失效」或「内存改了库没改」的不一致。
//
// until=0 表示恢复。
func saveDoubaoRoutePause(until int64) error {
	policy := seedanceroute_setting.WithSecondsRulePausedUntil(until)
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
	}
	if until > 0 {
		state["paused_until_text"] = time.Unix(until, 0).In(beijingZone).Format("2006-01-02 15:04:05")
	} else {
		state["paused_until_text"] = ""
	}
	return state
}
