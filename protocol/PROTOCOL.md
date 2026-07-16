# Agent Beacon Protocol v2

协议权威约束见 [`docs/ui.md`](../docs/ui.md)、
[`docs/notify.md`](../docs/notify.md) 和
[`docs/token-rate-api.md`](../docs/token-rate-api.md)。本目录保存可执行的 JSON Schema
与示例。

设备首选 USB CDC，帧格式见 [`docs/usb-transport.md`](../docs/usb-transport.md)；
WebSocket 兜底地址为 `ws://<bridge-host>:8787/v2/ws`。WebSocket 握手必须携带：

```text
X-Agent-Beacon-Device-ID
X-Agent-Beacon-Token
X-Agent-Beacon-Protocol: 2
```

两种传输的连接顺序均为 server `hello`、device `hello`、server `snapshot`。USB 的
device hello 还必须携带 `payload.auth_token`；WebSocket 已在 HTTP upgrade 时鉴权。
之后使用
`state_patch`、`notification` 和 heartbeat；revision gap 由设备发送
`get_snapshot` 恢复。

进程外通知生产者通过
`POST http://<bridge-host>:8787/v2/notifications` 投递完整 v2 notification envelope，
并携带 `X-Agent-Beacon-Token` 与 `Content-Type: application/json`。请求 revision 固定为
`0`；Bridge 完成校验、TTL 和去重后分配连续 revision，再通过设备 Hub 广播。
进程内 Provider 的 channel 用法、HTTP 示例和响应语义见
[`docs/notify.md`](../docs/notify.md#34-bridge-进程内投递)。

Snapshot 只包含 `clock`、`codex`、`agents`、`weather`、`system`；实时全局 Token
速度位于 `codex.token_rate`，不新增顶层 domain。`agents.codex_active` 由 Herdr
会话状态派生：仅当至少一个 Codex session 为 `working` 时为 `true`，断连时为
`false`。通知只允许
`agent`、`quota`、`weather`、`system` 四类。ACK 是平铺 v2 对象，状态集合为：

```text
received queued shown completed interrupted superseded
expired dropped invalid duplicate
```

WebSocket 单消息、USB payload 和 v2 envelope 上限均为 64 KiB，HTTP 请求体上限默认 256 KiB；
无效 UTF-8、未知字段、未知 enum 和旧业务字段均拒绝。
`macos/internal/protocol/schema_test.go` 会校验本目录全部 v2 示例。
