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
	"fmt"
	"io"
	"net"
	"net/http"
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
		// ⚠️ 这里**不能**只认 302。
		//
		// ArcReel 的产物有三种落法（2026-11-14 实测）：
		//   302 + Location → 产物还在 arcmedia CDN 未内联，跳转到公网地址；
		//   200 + 正文     → 产物已被内联到 arcreel，arcreel 直接把 mp4 吐出来
		//                    （带鉴权才能取，无鉴权 401）；
		//   206 + 正文     → 同上，只是上游按 Range 只给了一段。
		//
		// 之前只取 Location，于是 200/206 这两种落法被判成「拿不到地址」，
		// 超分静默跳过、客户拿到 480P 却按 720P 计费。
		//
		// 正确做法：能从**路径规律**推出 arcmedia 公网地址就用它 ——
		// MPS 拉不了带自定义头的地址，而 arcmedia（腾讯 COS）是公开的。
		if public := deriveArcmediaPublicURLFunc(raw); public != "" {
			return public
		}
		logger.LogError(ctx, fmt.Sprintf(
			"解析 CapCut 成片公开地址失败：上游返回 %d，但没有 Location，也推不出公开地址（带鉴权=%v）",
			resp.StatusCode, a.apiKey != ""))
		return ""
	}
	// 相对跳转（少见）按请求 URL 补全。
	if parsed, perr := url.Parse(loc); perr == nil && !parsed.IsAbs() {
		loc = resp.Request.URL.ResolveReference(parsed).String()
	}
	return loc
}

// deriveArcmediaPublicURLFunc 是「推导 arcmedia 公网地址」的注入点。
//
// 抽成变量是为了单测能替换它（单测里 httptest 监听的是 IP，
// 而 arcmedia 域名推导依赖真实域名形态）。
// 生产路径永远用 deriveArcmediaPublicURL。
var deriveArcmediaPublicURLFunc = deriveArcmediaPublicURL

// deriveArcmediaPublicURL 按路径规律推出 arcmedia 上的公网成片地址。
//
// ArcReel 的托管产物在两个域名下有两种路径形态（2026-11-14 实测）：
//
//	arcreel   /api/v1/remote-generation/jobs/{jobId}/outputs/{file}
//	arcmedia  /projects/remote_outputs/{jobId}/{file}
//
// arcreel 那个地址需要渠道密钥（无鉴权 401），arcmedia 那个是腾讯 COS
// 直链、公开可取。超分服务（MPS）从公网拉片、不能带自定义请求头，
// 所以必须交出 arcmedia 地址。
//
// ⚠️ 这里同时给出「带 ?v= 时间戳」与否两种形态：归档后的文件带 v 参数
// 是为了绕缓存，实测两种都能取到，优先用不带 v 的干净地址。
func deriveArcmediaPublicURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	// 找 /api/v1/remote-generation/jobs/{jobId}/outputs/{file} 这一段。
	marker := "/api/v1/remote-generation/jobs/"
	idx := strings.Index(parsed.Path, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimPrefix(parsed.Path[idx:], marker)
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	// 形如 {jobId}/outputs/{file}
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" || parts[1] != "outputs" {
		return ""
	}
	jobID, file := parts[0], parts[2]
	scheme := "https"
	if parsed.Scheme == "http" {
		scheme = "http"
	}
	// arcmedia 与 arcreel 同属一个主域，保留主域即可。
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	// IP 主机推不出 arcmedia 对应域（arcmedia.127.0.0.1 根本不存在）。
	if net.ParseIP(host) != nil {
		return ""
	}
	mediaHost := "arcmedia." + strings.TrimPrefix(host, "arcreel.")
	return fmt.Sprintf("%s://%s/projects/remote_outputs/%s/%s", scheme, mediaHost, jobID, file)
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
