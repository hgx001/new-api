# 首页改造设计：heibaidao 清新薄荷风格

**日期**：2026-07-04  
**背景**：参考 https://4sapi.com 首页内容与结构，将当前 new-api 默认首页替换为相同内容，品牌改为 `heibaidao`，UI 风格改为日式小清新（清新薄荷方向）。

## 1. 设计目标

- 复刻 4sapi.com 首页的所有内容区块与信息层级。
- 将品牌标识从 `4SAPI` 替换为 `heibaidao`。
- 将视觉风格从深色企业感改为清新薄荷日式小清新。
- 保留项目对 `New API` / `QuantumNous` 的必要引用。

## 2. 页面结构

| 区块 | 来源 | heibaidao 版处理 |
|---|---|---|
| 导航栏 | 4sapi.com | 首页 / 控制台 / 模型广场 / 文档 / 接入教程 / 关于 / 博客 + 登录/注册；移除当前默认的“排行榜” |
| Hero 主视觉 | 4sapi.com | Badge“企业级 SLA 协议保障”+ 标题 + 描述 + CTA + 信任企业 Logo |
| 实时数据卡 | 4sapi.com | 可用性 / 吞吐量 / 延迟 / 加密状态 |
| 企业版核心基石 | 4sapi.com | Speed / Stability / Security / Scalability 四卡片 |
| 核心优势 | 4sapi.com | 稳定可靠 / 极致性能 / 开放兼容 / 透明计费 / 全时服务 + 微信二维码 |
| 系统公告 | 4sapi.com 弹窗 | 改为顶部导航栏通知铃铛图标入口 |
| 页脚 | 4sapi.com | © 2026 heibaidao. 版权所有 · 设计与开发由 New API |

## 3. 视觉风格规范（清新薄荷）

- **主色**：`#7dd3c0`（薄荷绿）
- **深色文字**：`#1a4a3f`
- **次要文字**：`#5a7a72`
- **辅助背景**：`#f5f9f7`、`#e8f4f0`
- **卡片**：大圆角 16–24px，柔和阴影 `0 4px 24px rgba(120,180,160,0.1)`
- **按钮**：圆角胶囊形，主按钮填充薄荷绿，次按钮白底绿边
- **图标**：lucide-react，圆润线条，同色系配色
- **排版**：保留当前无衬线字体，标题加粗、行高宽松、大量留白
- **深色模式**：首页强制使用浅色主题，或在 `PublicLayout` 层级为首页单独禁用深色模式切换，确保清新薄荷风格不被破坏。

## 4. 文案映射

### Hero

- Badge：`企业级 SLA 协议保障`
- 标题：`支撑上市企业 AI 规模化落地`
- 描述：`heibaidao 为企业提供安全、高性能、可合规配置的大模型集成网关。支持私有化部署、高并发处理及细粒度权限管控。`
- 按钮：`立即体验`、`技术文档`
- 信任企业：OPENAI / ANTHROPIC / GOOGLE / GROK

### 实时数据卡

- `system_status: active`
- `API 响应可用性 99.99%`
- `并发吞吐量 1.2M+ RPM`
- `平均延迟 24ms`
- `端到端加密已启用 AES-256 Enterprise Standard`
- `ISO 27001`

### 企业版核心基石

| 标题 | 描述 |
|---|---|
| Speed | 毫秒级全球接入点调度，确保业务零感响应。 |
| Stability | 独家多通道容灾技术，故障自动切换，业务永不掉线。 |
| Security | 符合上市公司审计要求的日志溯源与权限审计系统。 |
| Scalability | 支持私有云、混合云部署，随业务规模无限水平扩展。 |

### 核心优势

| 标题 | 要点 |
|---|---|
| 稳定可靠 | 100% 官方企业级通道；稳定运行1年+，服务5万+客户；承诺永久运营 |
| 极致性能 | CN2 线路，毫秒级低延迟；智能负载均衡；超高并发，不限速 |
| 开放兼容 | 兼容 OpenAI 接口协议；支持 GPT、Claude、Gemini |
| 透明计费 | 按量付费，无封号风险；支持公对公开票 |
| 全时服务，安心托付 | 7x24 小时自助充值，专业技术团队实时响应；微信二维码 |

### 页脚

- `© 2026 heibaidao. 版权所有`
- `设计与开发由 New API`

## 5. 数据接入方案

Hero 右侧实时数据卡需要真实数据：

- `/api/status` 目前不返回可用性、吞吐量、延迟等指标。
- 建议新增轻量接口 `GET /api/home/metrics`，返回字段：
  ```json
  {
    "system_status": "active",
    "availability": "99.99%",
    "throughput": "1.2M+",
    "throughput_unit": "RPM",
    "latency": "24ms",
    "encryption": "AES-256 Enterprise Standard",
    "certification": "ISO 27001"
  }
  ```
- 后端从现有 performance / perf_metrics / uptime_kuma 或 channel 测试数据中聚合；若暂无可用数据，第一版可返回静态占位值，并在代码中标注“待接入真实数据源”。
- 微信二维码使用后端 `status.wechat_qrcode` 配置。

## 6. 导航与公告

- 顶部导航由 `useTopNavLinks` 根据后端 `HeaderNavModules` 动态生成。
- 为实现参考站导航（首页 / 控制台 / 模型广场 / 文档 / 接入教程 / 关于 / 博客），需要：
  - **移除“排行榜”**：参考站无此导航项，从首页默认导航中移除（`/rankings` 路由保留，用户仍可通过直接访问进入）。
  - **新增“接入教程”**：复用 `docs_link`（与“文档”指向同一文档站点）。
  - **新增“博客”**：指向 `/blog`；由于当前项目无博客路由，需要新增一个简单博客占位页面，显示“博客内容即将上线”或跳转外部博客。
- `PublicHeader` 已集成 `NotificationPopover` 通知铃铛图标，公告入口无需新增组件。公告内容由后端 `announcements_enabled` 与 `status.announcements` 驱动。

## 7. i18n 处理

- 项目约定 i18n key 为英文源字符串/描述性英文。
- 新增 key 到 `web/default/src/i18n/locales/zh.json` 和 `en.json`。
- key 命名示例：`"Enterprise SLA Guarantee"`、`"Support Listed Enterprises AI Scale"`、`"Speed"`、`"Stable and Reliable"`。
- 中文 locale 值使用参考站中文文案；英文 locale 值提供对应英文翻译。
- 品牌名 `heibaidao` 在 Logo、页脚版权等位置作为静态文本硬编码。

## 8. 文件修改清单

### 前端

- `web/default/src/features/home/components/sections/hero.tsx` — 重写 Hero
- `web/default/src/features/home/components/sections/features.tsx` — 重写 Features（核心基石 + 核心优势）
- `web/default/src/features/home/components/sections/stats.tsx` — 重写为实时数据卡
- `web/default/src/features/home/components/sections/cta.tsx` — 删除（参考站无独立底部 CTA）
- `web/default/src/features/home/components/sections/how-it-works.tsx` — 删除
- `web/default/src/features/home/index.tsx` — 调整引用，移除 HowItWorks/CTA
- `web/default/src/features/home/api.ts` — 新增 `/api/home/metrics` 调用
- `web/default/src/components/layout/components/footer.tsx` — 默认版权显示名改为 `heibaidao`，保留 New API 来源声明
- `web/default/src/hooks/use-top-nav-links.ts` — 移除“排行榜”默认显示，新增“接入教程”与“博客”
- `web/default/src/routes/blog/index.tsx` — 新增博客占位页面
- `web/default/src/i18n/locales/zh.json`、`en.json` — 新增文案键值

### 后端

- 新增 `GET /api/home/metrics` 接口（可选，真实数据）。
- 在 `router` 中注册新路由。

## 9. 受保护标识处理

按项目政策，`New API` 与 `QuantumNous` 的引用不可删除、替换或移除：

- 源代码文件头的 Copyright 与许可证声明保持不变。
- 页脚保留 `设计与开发由 New API`。
- 不修改 Go module path、package name、README、Docker 等处的项目标识。
- 仅将首页营销品牌从参考站的 `4SAPI` 替换为部署品牌 `heibaidao`。

## 10. 实现路径

采用 **直接重写默认首页组件** 方案：

1. 修改现有 `features/home` 组件，替换内容与视觉。
2. 同步更新 i18n 文案。
3. 调整导航与公告入口。
4. 后端可选新增 `/api/home/metrics` 接口接入真实数据。
5. 本地启动前端服务验证视觉效果。

## 11. 验收标准

- [ ] 首页内容与 4sapi.com 结构一致，品牌为 heibaidao。
- [ ] 视觉风格为清新薄荷日式小清新，无明显企业感残留。
- [ ] 导航栏项目与参考站一致，且不显示“排行榜”。
- [ ] “博客”导航项指向 `/blog`，页面可用（至少为占位页）。
- [ ] 公告入口改为顶部通知图标。
- [ ] 微信二维码从后端 `wechat_qrcode` 读取。
- [ ] 页脚默认显示 heibaidao，保留 New API 来源声明。
- [ ] 首页在深色模式下保持浅色清新薄荷风格。
- [ ] 前端构建通过，无 TypeScript 错误。
- [ ] i18n 中文/英文文案完整。
- [ ] 真实数据接口已接入或有明确占位方案。
