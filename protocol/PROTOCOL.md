# Agent Beacon Protocol v2

协议权威约束见 [`docs/ui.md`](../docs/ui.md)、
[`docs/notify.md`](../docs/notify.md) 和
[`docs/token-rate-api.md`](../docs/token-rate-api.md)。本目录保存可执行的 JSON Schema
与示例。

WebSocket 地址为 `ws://<bridge-host>:8787/v2/ws`。握手必须携带：

```text
X-Agent-Beacon-Device-ID
X-Agent-Beacon-Token
X-Agent-Beacon-Protocol: 2
```

连接顺序为 server `hello`、device `hello`、server `snapshot`。之后使用
`state_patch`、`notification` 和 heartbeat；revision gap 由设备发送
`get_snapshot` 恢复。

Snapshot 只包含 `clock`、`codex`、`agents`、`weather`、`system`；实时全局 Token
速度位于 `codex.token_rate`，不新增顶层 domain。`agents.codex_active` 由 Herdr
会话状态派生：仅当至少一个 Codex session 为 `working` 时为 `true`，断连时为
`false`。通知只允许
`agent`、`quota`、`weather`、`system` 四类。ACK 是平铺 v2 对象，状态集合为：

```text
received queued shown completed interrupted superseded
expired dropped invalid duplicate
```

WebSocket 单消息上限 64 KiB，HTTP 请求体上限 256 KiB；无效 UTF-8、未知 enum
和旧业务字段均拒绝。`macos/internal/protocol/schema_test.go` 会校验本目录全部
v2 示例。
