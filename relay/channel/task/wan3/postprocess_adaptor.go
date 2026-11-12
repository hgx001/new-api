package wan3

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

// ─────────────────────────────────────────────────────────────────────────────
// TaskPostProcessor 实现：把 wan3 adaptor 接入 service 层的成片后处理钩子。
//
// 这个文件只做「适配」：把 service 层的通用钩子协议翻译成 wan3 包内的
// 具体动作（提交超分 / 轮询超分）。真正的业务逻辑在 postprocess.go。
//
// 2026-11-01：把「提交 / 轮询」抽成包级导出函数 SubmitSuperResolution /
// PollSuperResolution，供**别的渠道**（心笔 Model Router 的 480P→720P 档）复用。
// 超分本身与上游是谁无关：只要拿到一条可抓取的 480P 成片地址就能跑。
// 模型名白名单仍留在各渠道自己的适配器里判，避免跨渠道串味。
// ─────────────────────────────────────────────────────────────────────────────

// AfterUpstreamSuccess 在上游第一跳成功后接管后处理。
//
// 分派规则：
//   · wan3.0-720p-特惠 → MPS 超分链路（承诺的是画质提升，直存不改变分辨率，
//     不能抢走这条语义；MPS 未配置时回落 480P 原片，维持改造前行为）；
//   · 其余模型 → COS 直存转存（配置了才接管，保证成片长期可取；
//     未配置时行为与改造前完全一致——上游原片直接交付）。
func (a *TaskAdaptor) AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil {
		return false, nil
	}
	if needsSuperResolution(task.Properties.OriginModelName) {
		return SubmitSuperResolution(ctx, task, upstreamURL)
	}
	return SubmitDirectRelocate(ctx, task, upstreamURL)
}

// PollPostProcess 按 PostProcess.Provider 分派到对应链路。
//
// 分派依据是 Provider 而不是模型名：直存任务与 MPS 任务的 Provider 不同，
// 且 Provider 缺失的历史数据仍按模型名回落到超分链路（兼容在途任务）。
func (a *TaskAdaptor) PollPostProcess(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	if task.PrivateData.PostProcess.Provider == cosDirectProvider {
		return PollDirectRelocate(ctx, task)
	}
	// 只有 wan3.0-720p-特惠 才走超分这条路；模型名对不上（历史数据/改配置）就放行。
	if !needsSuperResolution(task.Properties.OriginModelName) {
		return true, nil
	}
	return PollSuperResolution(ctx, task)
}

// SubmitSuperResolution 提交成片提升任务，并在 task 上记下后处理状态。
//
// 调用方负责先判定「这个模型名是否需要超分」——本函数不看模型名。
//
// 返回 handled=true 表示已接管：service 层会保持任务为「处理中」，
// 等 PollSuperResolution 收敛后再置 SUCCESS。
func SubmitSuperResolution(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil {
		return false, nil
	}
	// 没配 MPS 就不接管 —— 行为与改造前完全一致（用户拿 480P 原片）。
	if !tencentmps_setting.IsConfigured() {
		return false, nil
	}
	if strings.TrimSpace(upstreamURL) == "" {
		return false, nil
	}

	// 成片的**目标输出档位**固定 720P（输入恒为 480P，见 official720PGenerationResolution）。
	// 第三个参数是产物文件名种子：必须按任务唯一，否则不同任务的 720P 会写到同一个
	// COS 对象上互相覆盖（心笔渠道的成片 basename 恒为 content，实测已撞车）。
	pp, err := submitPostProcess(ctx, upstreamURL, official720POutputResolution, task.TaskID)
	if err != nil {
		// 提交失败：回落原片，**不让用户任务失败**。
		//
		// 这里返回 (false, nil) 而不是 error：service 层收到 error 会保持「处理中」
		// 并无限重试，而提交失败往往是配置问题（密钥无效、桶不存在），
		// 重试也不会好，只会拖到超时。回落原片让用户至少拿到片。
		LogPostProcessFallback(ctx, task.TaskID, err.Error())
		return false, nil
	}
	if pp == nil {
		return false, nil
	}

	task.PrivateData.PostProcess = pp
	task.PrivateData.ResultURL = upstreamURL
	logger.LogInfo(ctx, "Task "+task.TaskID+" 成片提升任务已提交")
	return true, nil
}

// PollSuperResolution 推进成片提升任务；done=true 表示已到终态。
//
// 调用方负责判定模型名是否走超分链路——本函数不看模型名。
func PollSuperResolution(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	return pollPostProcess(ctx, task)
}

// 编译期断言：TaskAdaptor 必须实现 service 层的后处理钩子协议。
//
// service 层用类型断言探测（见 service.TaskPostProcessor），
// 这里在编译期锁死，避免有人改了方法签名却没人发现钩子失效了。
var _ interface {
	AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error)
	PollPostProcess(ctx context.Context, task *model.Task) (bool, error)
} = (*TaskAdaptor)(nil)
