# VPN 节点 agent

把网站签发的「每订单独立凭据」同步进日本节点的 sing-box，并把真实连接来源 IP
回报网站，用于检测同一条订阅被多人共用。

## 为什么需要它

网站和节点是两条独立链路：

```
浏览器下单 ──> new-api 生成订单 + 独立 UUID/订阅令牌 ──> 数据库
                                                          │
节点 agent ── 每分钟拉 /api/vpn/node/manifest ─────────────┘
   └── 只重建 name 形如 v\d+ 的用户，写进 8443 入站的 users
   └── 读 sing-box 日志里的真实连接 IP，POST /api/vpn/node/connections
```

只改网站没有任何用：vless 链接一旦复制出去就脱离网站控制。真正能「单独作废某一条」
的只有节点侧的 UUID 表，所以凭据必须由节点 agent 落地。

## 三条硬约束

1. **老用户不动**。8443 入站里两个早期共享 UUID 没有 `name` 字段，agent 只替换
   `^v\d+$` 的用户，其余原样保留。因此接入本 agent 不会影响已经付款的两位客户。
2. **不校验通过就不落地**。候选配置先写 `.new`，过 `sing-box check`，通过后备份旧配置
   再原子替换；重启后核对全部入站端口是否仍在监听，异常自动回滚。
   `systemctl restart` 会有约 1 秒的连接中断，客户端会自动重连。
3. **先看后写**。`--dry-run` 只打印变更规模，不写文件、不重启；`install.sh` 末尾默认
   跑一次 dry-run。

## 目录

| 文件 | 作用 |
| --- | --- |
| `agent.py` | 主程序：拉清单 → 改配置 → 重启校验 → 采集日志 → 上报事件 |
| `test_agent.py` | 纯逻辑单测（配对、用户合并、日志游标），`python3 -m unittest discover -s deploy/vpn-node-agent -p 'test_*.py'` |
| `vpn-node-agent.service` / `.timer` | oneshot + 每分钟触发 |
| `install.sh` | 节点侧安装、日志级别提升、首次 dry-run |

## 安装

在节点上以 root 执行（先 `scp` 整个目录过去）：

```bash
scp -P 877 -r deploy/vpn-node-agent ubuntu@43.167.248.149:/tmp/
ssh -p 877 ubuntu@43.167.248.149
sudo bash /tmp/vpn-node-agent/install.sh --token '<与网站侧一致的令牌>'
```

令牌在网站侧的容器环境变量 `VPN_NODE_AGENT_TOKEN` 里，两边必须一致；节点侧存在
`/etc/vpn-node-agent/agent.env`（权限 600）。

## 验证

```bash
systemctl list-timers vpn-node-agent.timer     # 下一轮时间
journalctl -u vpn-node-agent.service -n 30     # 最近一轮输出
curl -s -H "Authorization: Bearer $TOKEN" \
  https://api.heibaidao.cn/api/vpn/node/manifest   # 网站当前下发的清单
sing-box check -c /etc/sing-box/config.json
```

日志里应看到 `清单: 托管用户 N 条，保留非托管用户 2 条，变更=否`。

## 日志与隐私

- 需要把 sing-box 日志级别从 `warning` 提到 `info` 才能看到
  `inbound connection from <ip>`；`install.sh` 会自动做这一步（`--skip-log-level` 可跳过）。
- 日志按连接 ID 配对两行：源 IP 一行、用户名一行。agent 只上报 `^v\d+$` 用户的
  `(name, ip)`，**不采内容、不采域名、不采流量**。
- `/var/log/sing-box` 已由系统 logrotate 按天轮转、保留 30 天；agent 记录 inode+偏移量，
  轮转后会把旧文件尾巴读完再切到新文件。
- 首次运行从文件末尾开始，不回放历史，避免把陈年 IP 当成今天的共享证据。

## 回滚

```bash
systemctl disable --now vpn-node-agent.timer
# 恢复最近一次备份（agent 每次改配置前都会备份到 /etc/sing-box/backup/）
sudo cp /etc/sing-box/backup/config-<timestamp>.json /etc/sing-box/config.json
sudo sing-box check -c /etc/sing-box/config.json && sudo systemctl restart sing-box
```

停掉 agent 后，已下发的独立 UUID 会停留在节点配置里继续可用（不会自动失效），
但不再有新凭据同步、也不再有共享检测。要彻底回到共享密钥模式，需要网站侧同时停用
订阅下发（`GetVpnMyKey` 的 `mode=subscription` 分支）。

## 与网站侧的契约

| 接口 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /api/vpn/node/manifest` | `Authorization: Bearer <VPN_NODE_AGENT_TOKEN>` | `{"users":[{"name","uuid"}],"frozen":[...]}`，只含已支付且未过期订单 |
| `POST /api/vpn/node/connections` | 同上 | `{"events":[{"name","ip","ts"}]}`，返回 `{"frozen":[...]}` |

阈值在 `controller/vpn_subscription.go` 的 `VpnMaxDistinctIPs`（默认 3）。
超过阈值时网站把凭据置为 `frozen`，下一轮清单里就会消失，节点侧立即无法认证；
用户在页面点「重置密钥」换新 UUID 即可恢复，订阅地址不变。
