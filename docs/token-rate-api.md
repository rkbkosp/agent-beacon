# Token 速率传输接口

本文档描述 Agent Beacon Bridge 向设备或其他客户端传输 Codex Token 速率的接口。
接口属于 Agent Beacon Protocol v2，不提供单独的 `/token-rate` 路由；速率数据固定放在
`codex.token_rate`，通过 HTTP 快照或 WebSocket 实时状态更新下发。

## 1. 指标定义

指标名称为 `completion_output_tokens_per_second`，表示连接到同一个本地
`codex-token-rate-daemon` 的全部 patched Codex 进程的全局 completion-output 速率。

- 包含用户可见的助手文本和流式工具调用参数；
- 不包含 input token、hidden reasoning、工具输出和认证信息；
- 使用 daemon 已聚合的滑动窗口 EMA，Bridge 不再二次平滑；
- 是本机全局值，不按 Codex Home、进程或 session 拆分；
- 是近似值，因此 `estimated` 固定为 `true`。

`raw_tokens_per_second` 和 `tool_active_streams` 只存在于 Bridge 上游的 daemon 状态文件，
不会通过本接口下发。

## 2. 接口概览

| 用途 | 方法与地址 | 鉴权 |
|---|---|---|
| 获取完整当前状态 | `GET http://<bridge-host>:8787/v2/snapshot` | `X-Agent-Beacon-Token` |
| 订阅实时状态 | `GET ws://<bridge-host>:8787/v2/ws` | 设备 ID、Token、协议版本请求头 |

默认端口为 `8787`，实际地址以 `server.listen` 配置为准。HTTP 和 WebSocket 当前均为
明文传输，只应在受信任的本地网络中使用。

## 3. 数据结构

HTTP `snapshot` 和 WebSocket `snapshot` 中的字段路径均为：

```text
payload.codex.token_rate
```

WebSocket `state_patch` 在速率可见值变化时也使用相同路径。当前 Bridge 会发送完整的
`payload.codex` domain，客户端应替换本地的整个 `codex` 状态，不要假设只收到
`token_rate` 子对象。

| 字段 | 类型 | 约束 | 说明 |
|---|---|---|---|
| `tokens_per_second` | `number \| null` | `0..10000` | 聚合后的 Token 速率，单位为 `token/s`；不可用时为 `null` |
| `active_sessions` | `integer` | `>= 0` | 当前滑动窗口或工具执行保持阶段内的活跃 session 数 |
| `active_streams` | `integer` | `>= 0` | 当前活跃响应流数，必须大于等于 `active_sessions` |
| `window_ms` | `integer` | `0..600000` | daemon 聚合窗口，当前默认值为 `2000` ms |
| `estimated` | `boolean` | 固定为 `true` | 表示速率是根据流式输出近似计算的 |
| `updated_at` | `string \| null` | RFC 3339 `date-time` | daemon 生成该状态的时间；无有效样本时可为 `null` |
| `freshness` | `string` | 见下表 | Bridge 对数据新鲜度的判断 |

`tokens_per_second: 0.0` 表示数据有效但当前没有输出，不能当成无数据；只有
`tokens_per_second: null` 才表示速率不可用。速率为 `null` 时，两个活动计数必须都是
`0`。

| `freshness` | 含义 | 客户端建议 |
|---|---|---|
| `fresh` | 状态文件读取成功且未过期 | 正常展示数值 |
| `cached` | 短暂读取失败，仍在容忍时间内 | 展示最后值，并标记为缓存数据 |
| `stale` | 超过过期时间仍无新状态 | 将速率视为不可用 |
| `unknown` | 尚未取得状态或 Provider 未启用 | 将速率视为不可用 |

默认情况下 Bridge 每 `200ms` 读取一次 daemon 状态，只在速率、活动计数、窗口或
`freshness` 等可见值变化时发送更新。连续 `2s` 没有有效状态后进入 `stale`。

## 4. HTTP 快照

### 请求

```http
GET /v2/snapshot HTTP/1.1
Host: <bridge-host>:8787
X-Agent-Beacon-Token: <device-token>
Accept: application/json
```

请求没有 query 参数和 body。命令行示例：

```bash
curl -sS 'http://127.0.0.1:8787/v2/snapshot' \
  -H "X-Agent-Beacon-Token: $(cat "$HOME/Library/Application Support/AgentBeacon/token")" \
  | jq '.payload.codex.token_rate'
```

### 成功响应

状态码为 `200 OK`，`Content-Type` 为 `application/json`。以上 `jq` 命令输出：

```json
{
  "tokens_per_second": 42.7,
  "active_sessions": 2,
  "active_streams": 3,
  "window_ms": 2000,
  "estimated": true,
  "updated_at": "2026-07-16T10:30:12.240+08:00",
  "freshness": "fresh"
}
```

未经筛选的响应是标准 v2 `snapshot` envelope：

| 字段 | 类型 | 说明 |
|---|---|---|
| `v` | `integer` | 固定为 `2` |
| `id` | `string` | 本次消息 ID |
| `type` | `string` | 固定为 `snapshot` |
| `ts` | `string` | Bridge 生成消息的 RFC 3339 时间 |
| `revision` | `integer` | 当前状态修订号 |
| `payload` | `object` | 完整状态，包含 `clock`、`codex`、`agents`、`weather`、`system` |

完整响应示例见 [`../protocol/examples/snapshot.json`](../protocol/examples/snapshot.json)。

### 错误响应

缺少 Token 或 Token 不匹配时返回：

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json
```

```json
{"error":"unauthorized"}
```

## 5. WebSocket 实时更新

### 建立连接

```http
GET /v2/ws HTTP/1.1
Host: <bridge-host>:8787
Upgrade: websocket
Connection: Upgrade
X-Agent-Beacon-Device-ID: <device-id>
X-Agent-Beacon-Token: <device-token>
X-Agent-Beacon-Protocol: 2
```

三个 `X-Agent-Beacon-*` 请求头都必须提供。鉴权成功后服务端返回
`101 Switching Protocols`；请求头缺失、协议版本不是 `2` 或 Token 错误时返回
`401 Unauthorized`：

```json
{"error":"invalid device credentials"}
```

### 握手顺序

1. 服务端发送 `hello`；
2. 客户端发送角色为 `device` 的 `hello`；
3. 服务端发送完整 `snapshot`；
4. 客户端开始接收 `state_patch`。

客户端 `hello` 示例：

```json
{
  "v": 2,
  "id": "hello-device-1",
  "type": "hello",
  "ts": "2026-07-16T10:30:12+08:00",
  "revision": 0,
  "payload": {
    "role": "device",
    "device_id": "beacon-desk-01",
    "protocol_version": 2,
    "firmware_version": "2.0.0"
  }
}
```

其中 `payload.device_id` 必须与握手请求头 `X-Agent-Beacon-Device-ID` 完全一致。服务端在
收到合法的 device `hello` 并发送初始快照前，不会向该连接广播状态更新。

### 速率更新消息

速率变化时服务端发送 `state_patch`。下面是当前 Bridge 发出的完整 `codex` domain
示例；其他顶层 domain 未变化时不会重复发送：

```json
{
  "v": 2,
  "id": "patch-1784169012240-18",
  "type": "state_patch",
  "ts": "2026-07-16T10:30:12.240+08:00",
  "revision": 302,
  "payload": {
    "codex": {
      "homes": [
        {
          "id": "main",
          "label": "MAIN",
          "weekly_remaining_percent": 18,
          "weekly_reset_at": "2026-07-17T11:39:00+08:00",
          "reset_cards_available": 2,
          "nearest_reset_card_expires_at": "2026-07-20T23:59:59+08:00",
          "updated_at": "2026-07-16T10:30:00+08:00",
          "freshness": "fresh"
        }
      ],
      "relay": {
        "remaining": 14.16,
        "unit": "USD",
        "is_valid": true,
        "updated_at": "2026-07-16T10:30:00+08:00",
        "freshness": "fresh"
      },
      "token_rate": {
        "tokens_per_second": 42.7,
        "active_sessions": 2,
        "active_streams": 3,
        "window_ms": 2000,
        "estimated": true,
        "updated_at": "2026-07-16T10:30:12.240+08:00",
        "freshness": "fresh"
      }
    },
    "system": {
      "bridge_online": true,
      "overall_freshness": "fresh"
    }
  }
}
```

`state_patch` 不需要客户端 ACK。客户端应记录 `revision`：

- `revision <= 当前 revision`：按重复或旧消息忽略；
- `revision == 当前 revision + 1`：应用更新；
- `revision > 当前 revision + 1`：发现缺口，发送 `get_snapshot` 获取完整状态。

`get_snapshot` 示例：

```json
{
  "v": 2,
  "id": "get-snapshot-1",
  "type": "get_snapshot",
  "ts": "2026-07-16T10:30:13+08:00",
  "revision": 301,
  "payload": {"reason":"revision_gap"}
}
```

服务端每 `20s` 发送 WebSocket Ping，客户端应正常回复 Pong。单条 WebSocket 消息上限为
`64 KiB`；连接中断后客户端应重新握手，并以新的完整 `snapshot` 覆盖本地状态。

## 6. 不可用状态示例

daemon 未启动、状态过期或尚未取得数据时，速率对象示例如下：

```json
{
  "tokens_per_second": null,
  "active_sessions": 0,
  "active_streams": 0,
  "window_ms": 2000,
  "estimated": true,
  "updated_at": "2026-07-16T10:30:10.000+08:00",
  "freshness": "stale"
}
```

客户端不要用本地计时把 `null` 自动改成 `0.0`，也不要根据
`agents.codex_active` 推算 Token 速率；`agents.codex_active` 只控制界面是否展示速度页，
`codex.token_rate` 才是速率的唯一事实源。

## 7. 兼容性与参考

- 协议版本：Agent Beacon Protocol v2；
- 字段是 `codex` domain 的必填成员，不新增顶层 domain；
- 消费端必须接受 `0.0` 和 `null`，并按 `freshness` 区分有效零值与无数据；
- JSON Schema：[`../protocol/schema/snapshot-v2.schema.json`](../protocol/schema/snapshot-v2.schema.json)；
- 协议总览：[`../protocol/PROTOCOL.md`](../protocol/PROTOCOL.md)；
- 安全约束：[`security.md`](security.md)。
