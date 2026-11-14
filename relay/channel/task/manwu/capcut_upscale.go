package manwu

// CapCut 备选链路的「交付前超分」钩子（2026-11-14 加）。
//
// ───────────────────────── 什么时候触发 ─────────────────────────
//
//	只对**规则五降级过来的 CapCut 任务**触发：豆包链路（seedance-2-5）不可用时，
//	条件路由把请求改派到 cp-seedance-2.5flash，并按 480p 生成；
//	成片要超分到 720P 再交付，才算兑现「客户买的 720P」。
//
//	判定用「执行模型 + 计费原点」两个事实，不新增状态位：
//	  Properties.OriginModelName == cp-seedance-2.5flash（实际跑的模型）
//	  PrivateData.RouteOriginModel == seedance-2-5（客户原本买的模型）
//
//	这对组合**只有**规则五的备选链路能产生：
//	  - 客户直接买 cp-seedance-2.5flash → RouteOriginModel 为空 → 不触发；
//	  - 客户直接买 seedance-2-5（豆包正常）→ 执行模型不是 CapCut → 不触发；
//	  - 规则七（电商内容 → cp-seedance-2.5flash）→ RouteOriginModel 是促销族成员
//	    → 不触发（规则七是另一套定价，与本链路无关）。
//
//	⚠️ 为什么不给 CapCut 全档做超分：CapCut 自己就是按档卖的商品，
//	720p 单子已经是 720p，再超分一次是白花钱且会糊。
//	超分只服务「生成档位低于交付档位」这一种情况。
//
// ───────────────────────── 为什么用「注入」而不是直接调用 ─────────────────────────
//
//	超分实现住在 relay/channel/task/wan3（SubmitSuperResolution /
//	PollSuperResolution，与规则六心笔内链共用同一套 MPS 链路）。
//	但 wan3 包**已经 import 了 manwu**（media_relocate.go 复用素材转存），
//	manwu 反向 import wan3 会构成 import 环，编译期直接失败。
//
//	所以钩子在装配处（relay/relay_adaptor.go 的 init）注入：
//	那里同时 import 了 manwu 与 wan3，是天然的 composition root。
//	本文件只声明「我需要这两个能力」，不关心实现来自哪个包。
//
//	⚠️ 钩子未注入时**不能假装成功**：说明服务装配漏了（或测试环境没装），
//	按「超分不可用」处理 —— 回落交付原片并打错误日志，不把任务卡在处理中。

import (
	"context"
	"io"
	"net/http"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// capcutFailoverUpscaleModel 是走「480p 生成 + 超分 720P 交付」的执行模型。
const capcutFailoverUpscaleModel = ModelCapcut25Flash

// capcutAuthedMediaPath 是 ArcReel「需要渠道密钥才能取片」的产物接口路径片段。
//
// 为什么单独判它：ArcReel 的成片有两种形态（见 ParseTaskResult）——
// 公网直链（可直接给第二跳服务）与自有托管路径（必须带 Authorization）。
// 后者对 MPS 不可用：MPS 从公网拉片，不能带自定义请求头。
const capcutAuthedMediaPath = "/api/v1/remote-generation/"

// capcutMediaResolveTimeout 是「换公开地址」这一跳的超时。
//
// 只用来读一个 302 响应头，正常在百毫秒级；给 20 秒是防代理层抽风，
// 不能让一次网络抖动把成片交付拖成「处理中」。
const capcutMediaResolveTimeout = 20 * time.Second

// superResolutionSubmitter / superResolutionPoller 是注入的超分实现。
//
// 签名与 wan3.SubmitSuperResolution / wan3.PollSuperResolution 完全一致，
// 见 SetSuperResolutionHooks 的说明。
var (
	superResolutionSubmitter func(ctx context.Context, task *model.Task, upstreamURL string) (bool, error)
	superResolutionPoller    func(ctx context.Context, task *model.Task) (bool, error)
)

// SetSuperResolutionHooks 注入成片超分的提交/轮询实现。
//
// 调用方：relay/relay_adaptor.go 的 init（那里能同时看到 manwu 与 wan3）。
// 传 nil 表示「本进程没有超分能力」（测试环境常见）。
func SetSuperResolutionHooks(
	submit func(ctx context.Context, task *model.Task, upstreamURL string) (bool, error),
	poll func(ctx context.Context, task *model.Task) (bool, error),
) {
	superResolutionSubmitter = submit
	superResolutionPoller = poll
}

// needsCapcutFailoverUpscale 判断某个任务是否需要在交付前超分。
//
// 两个条件必须同时成立（缺一不可）：
//  1. 实际执行的是 CapCut 2.5 Flash；
//  2. 计费原点是豆包 seedance-2-5（即这条请求是规则五降级来的）。
func needsCapcutFailoverUpscale(task *model.Task) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.Properties.OriginModelName) != capcutFailoverUpscaleModel {
		return false
	}
	return strings.TrimSpace(task.PrivateData.RouteOriginModel) == constant.Seedance25DoubaoModel
}

// AfterUpstreamSuccess 在上游第一跳成功后接管「超分」这一步。
//
// 返回 handled=true 表示已提交超分任务，service 层会保持任务「处理中」，
// 等轮询推进完成后才置 SUCCESS（此时 ResultURL 已是 720P 产物）。
//
// 其它模型一律返回 (false, nil)：本 adaptor 承载豆包/即梦/CapCut 等多个平台，
// 只有上面那一种组合需要二次加工，其余保持原有的一跳交付行为。
func (a *TaskAdaptor) AfterUpstreamSuccess(
	ctx context.Context,
	task *model.Task,
	upstreamURL string,
) (bool, error) {
	if !needsCapcutFailoverUpscale(task) {
		return false, nil
	}
	if superResolutionSubmitter == nil {
		// 装配缺失：宁可交付 480P 原片也不要把任务挂在「处理中」，
		// 但必须留一条错误日志 —— 这是配置问题，不是业务失败。
		logger.LogError(ctx, "manwu 超分钩子未注入，CapCut 降级任务按原片交付（请检查 relay_adaptor 的装配）")
		model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
			model.TaskLogLevelError, "super resolution hook is not registered", nil)
		return false, nil
	}

	// ⚠️ 成片地址必须自己解析，不能只信 upstreamURL。
	//
	// ArcReel 走自有托管时，ParseTaskResult **刻意不吐地址**（交付由本站的
	// content 代理回源，见那里的注释），于是 service 层传下来的 upstreamURL
	// 是空串 —— 第二跳拿不到输入，只能静默跳过超分。2026-11-14 实测：
	// 开关全开、日志也显示已改派，客户拿到的仍是 854×480。
	//
	// 这里补两道：
	//  1. upstreamURL 为空 → 从任务的原始响应（task.Data）里取 sourceUrl；
	//  2. 该地址若需要渠道密钥，用一次带头 302 换成公开 CDN 地址
	//     （MPS 只能拉无鉴权地址）。
	media := strings.TrimSpace(upstreamURL)
	if media == "" {
		media = capcutSourceURLFromTask(task)
	}
	media = a.resolveCapcutMediaURL(ctx, media)
	if media == "" {
		logger.LogError(ctx, "CapCut 降级任务拿不到可抓取的成片地址，按原片交付（"+task.TaskID+"）")
		model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
			model.TaskLogLevelError, "upstream media url is empty or unreachable", nil)
		return false, nil
	}
	return superResolutionSubmitter(ctx, task, media)
}

// capcutSourceURLFromTask 从任务保存的上游原始响应里取成片地址。
//
// 用 jobResponse 而不是自己 define 一个匿名结构，是为了与 ParseTaskResult
// 共用同一份字段口径（sourceUrl / source_url 两个别名都认）。
func capcutSourceURLFromTask(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var res jobResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return ""
	}
	return strings.TrimSpace(res.resultURL())
}

// resolveCapcutMediaURL 把「需要渠道密钥才能取」的成片地址换成公开地址。
//
// ArcReel 的 job 产物接口对无鉴权请求返回 401，但它会 302 到
// arcmedia CDN —— 那个地址是公开的，MPS 可以直接拉。
// 所以只要用渠道密钥发一次请求、把 Location 读出来即可（不跟随，
// 避免整片进了我们的内存）。
func (a *TaskAdaptor) resolveCapcutMediaURL(ctx context.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 已经是公网直链 → 原样交出去。
	if !strings.Contains(raw, capcutAuthedMediaPath) {
		return raw
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	// 只取响应头：Range 让上游别把整片推过来（不支持 Range 的服务端也只会
	// 多传一点，我们下面用 LimitReader 兜住）。
	req.Header.Set("Range", "bytes=0-0")
	client := &http.Client{
		Timeout: capcutMediaResolveTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, "解析 CapCut 成片公开地址失败："+err.Error())
		return ""
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))

	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		logger.LogError(ctx, fmt.Sprintf(
			"解析 CapCut 成片公开地址失败：上游返回 %d，但没有 Location（带鉴权=%v）",
			resp.StatusCode, a.apiKey != ""))
		return ""
	}
	// 相对跳转（少见）按请求 URL 补全。
	if parsed, perr := url.Parse(loc); perr == nil && !parsed.IsAbs() {
		loc = resp.Request.URL.ResolveReference(parsed).String()
	}
	return loc
}

// PollPostProcess 推进超分任务；done=true 表示已到终态。
//
// 只有 needsCapcutFailoverUpscale 为真时才推进 —— 与本 adaptor 其它任务的
// 「无后处理」语义保持一致（返回 true 表示「没有后处理要等」，service 层照常收尾）。
func (a *TaskAdaptor) PollPostProcess(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	if !needsCapcutFailoverUpscale(task) {
		return true, nil
	}
	if superResolutionPoller == nil {
		return true, nil
	}
	return superResolutionPoller(ctx, task)
}
