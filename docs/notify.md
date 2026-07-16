# Agent Beacon 通知模块设计（notify.md）

> 本文是通知事件、全屏呈现和设备端通知队列的唯一权威规格。
> 当前只承载：**Herdr Agent 状态、Codex 一周配额、下一出门窗口降水、系统连接与 Provider 故障**。
> UI 页面和字段见 `ui.md`。

---

## 1. 当前范围

### 1.1 必须支持

```text
agent    Herdr blocked / done 状态变化
quota    两个 CODEX_HOME 的一周额度阈值与数据异常
weather  下一出门窗口需要带伞
system   Bridge、Herdr、QWeather、中转站和协议故障/恢复
```

### 1.2 明确不做

```text
task / todo
email / inbox
chat / message
review / evaluation / benchmark
5-hour Codex rolling window
```

不得为这些领域创建页面、Provider、通知 kind 或队列特判。

### 1.3 呈现通道

第一阶段只依赖 LCD：

1. 暂停普通页面轮播；
2. 保存当前页和剩余停留时间；
3. 整屏切换为主题色；
4. 中央显示图标、标题、详情；
5. 停留数秒；
6. 自动恢复原页面；
7. 未解决状态继续留在对应页面。

板载 RGB 默认为关闭，不参与 MVP 验收。

---

## 2. 核心模型

### 2.1 State、Transition、Notification

```text
State
  Herdr agent=working
  MAIN weekly_remaining=18%
  next_outing umbrella=false
        ↓
Meaningful transition
  working -> done
  16% -> 14%
  umbrella false -> true
        ↓
Notification policy
  是否值得打断、颜色、优先级、TTL、去重键
        ↓
Device queue and presentation
```

Provider 不得直接指定动画步骤；Mac 策略层生成规范化通知，ESP32 只负责可靠调度和展示。

### 2.2 颜色与优先级分离

视觉主题：

| `theme` | 语义 |
|---|---|
| `blue` | 一般变化、信息、解除 |
| `yellow` | 需要关注、数据异常或阻塞 |
| `red` | 高显著、强时效、即将出门需带伞、额度极低 |
| `green` | 完成、恢复 |

紧急度：

```text
passive    不进全屏队列，只更新页面
normal     普通排队
attention  可打断 normal
urgent     可打断 normal/attention
```

颜色不是优先级。例如带伞提醒可以是红色，但 Herdr/协议严重故障仍可拥有更高优先级。

---

## 3. 通知对象

```json
{
  "v": 2,
  "id": "evt_01K...",
  "type": "notification",
  "ts": "2026-07-14T14:30:01+08:00",
  "revision": 301,
  "payload": {
    "category": "agent",
    "kind": "agent.done",
    "source": "herdr",
    "subject_id": "w1:p1",
    "theme": "green",
    "urgency": "normal",
    "priority": 50,
    "dedupe_key": "agent:w1:p1:session-abc:done:42",
    "supersede_key": "agent:w1:p1",
    "group_key": "agent:done",
    "title": "Agent 已完成",
    "detail": "Chrome Plugin · 等待查看",
    "source_label": "Herdr",
    "display_ms": 4000,
    "expires_at": "2026-07-14T14:31:01+08:00",
    "sticky_badge": false,
    "replay_after_interrupt": false,
    "max_replays": 0
  }
}
```

### 3.1 必填字段

```text
v
id
type
ts
revision
category
kind
source
subject_id
theme
urgency
priority
dedupe_key
title
display_ms
expires_at
```

### 3.2 文本限制

```text
title         最多 14 个中文字符，最多两行
detail        最多 32 个中文字符，最多两行
source_label  最多 12 个字符
```

Mac 负责语义压缩；固件负责像素宽度截断和缺字降级。

机读标识按 UTF-8 字节数限制，生产者应使用稳定的 ASCII key：

```text
id              64 bytes
kind            64 bytes
source          32 bytes
subject_id      96 bytes
dedupe_key     160 bytes
supersede_key  128 bytes
group_key       96 bytes
```

### 3.3 合法值

```text
category: agent | quota | weather | system
theme: blue | yellow | red | green
urgency: normal | attention | urgent
priority: 0..100
display_ms: 1500..12000
```

`passive` 状态不发送 notification 对象，只通过 snapshot/patch 更新页面。

### 3.4 Bridge 进程内投递

Bridge 内部只传递 `protocol.Notification` 业务对象，不能由 Provider 自行创建
`id`、`ts` 或 `revision`。常规 Provider 应通过 `Start` 收到的有界 channel 发送
`providers.Update`：

```go
now := time.Now()
notification := &protocol.Notification{
	Category:    protocol.CategorySystem,
	Kind:        "system.provider_error",
	Source:      "example-provider",
	SubjectID:   "primary",
	Theme:       protocol.ThemeYellow,
	Urgency:     protocol.UrgencyNormal,
	Priority:    44,
	DedupeKey:   "system:provider:example:stale",
	Title:       "Example 数据过期",
	Detail:      "等待下一次刷新",
	SourceLabel: "Example",
	DisplayMS:   5000,
	ExpiresAt:   now.Add(15 * time.Minute),
}

select {
case out <- providers.Update{Notification: notification}:
	return nil
case <-ctx.Done():
	return ctx.Err()
}
```

若一次事实变化同时更新页面状态并产生通知，应放进同一个 `Update`：

```go
providers.Update{
	Patch:        protocol.StatePatch{System: &systemState},
	Notification: notification,
}
```

Bridge 会先校验并广播 patch，再校验、去重和广播 notification，保证设备收到全屏通知时
底层持续状态已经更新。Provider 还必须遵守以下所有权规则：

- `Snapshot` 只建立事实基线，不补发历史通知；
- channel 是有界的，发送时必须同时监听 `ctx.Done()`，不得另起 goroutine 绕过背压；
- 发出 `Update` 后不得继续修改其中的指针、slice 或 map；
- `dedupe_key` 表示业务事实，不能使用当前时间或随机数逃避去重；
- `expires_at` 必须按事件价值设置，不能用零值或无限 TTL；
- Provider 不得直接操作 store、WebSocket hub 或固件队列。

已经持有 `*api.Server` 且不属于 Provider 生命周期的同进程组件，可以直接调用：

```go
receipt, err := bridge.PublishNotification(*notification)
```

该方法负责生成 envelope 元数据，并返回 `accepted`、`deduplicated` 或 `expired`。
只有 `accepted` 会分配连续 revision、写入事件历史并广播。调用成功只表示 Bridge 已接收，
设备是否显示仍以设备 ACK 为准。

### 3.5 HTTP 通知监听入口

进程外生产者使用 Bridge 已有 HTTP 服务，不单独启动第二个端口：

```text
POST http://<bridge-host>:8787/v2/notifications
```

请求必须携带 `X-Agent-Beacon-Token` 和 `Content-Type: application/json`，body 是完整的
v2 notification envelope。生产者负责 `id` 和 `ts`；请求 `revision` 必须为 `0`，成功
接收后由 Bridge 改写为全局连续 revision。`payload` 与本节定义的进程内对象完全相同。

macOS 上可直接发送：

```bash
EVENT_ID="evt-http-$(date +%s)"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
EXPIRES="$(date -u -v+5M +%Y-%m-%dT%H:%M:%SZ)"
TOKEN="$(cat "$HOME/Library/Application Support/AgentBeacon/token")"

curl -sS -X POST 'http://127.0.0.1:8787/v2/notifications' \
  -H "X-Agent-Beacon-Token: $TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @- <<JSON
{
  "v": 2,
  "id": "$EVENT_ID",
  "type": "notification",
  "ts": "$NOW",
  "revision": 0,
  "payload": {
    "category": "system",
    "kind": "system.provider_error",
    "source": "build-monitor",
    "subject_id": "agent-beacon",
    "theme": "yellow",
    "urgency": "normal",
    "priority": 44,
    "dedupe_key": "system:build-monitor:agent-beacon:failed",
    "title": "构建需要关注",
    "detail": "agent-beacon · 测试失败",
    "source_label": "Build",
    "display_ms": 5000,
    "expires_at": "$EXPIRES",
    "sticky_badge": false,
    "replay_after_interrupt": false,
    "max_replays": 0
  }
}
JSON
```

首次接收返回 `202 Accepted`：

```json
{"status":"accepted","event_id":"evt-http-1784170800","revision":302}
```

| HTTP 状态 | `status` / 含义 |
|---:|---|
| `202` | `accepted`：已写入历史，并广播给当前已完成 hello 的设备 |
| `200` | `deduplicated`：`id` 或 `dedupe_key` 已在去重窗口内处理，不会重播 |
| `410` | `expired`：`expires_at` 已到，不写入、不广播 |
| `400` | JSON、UTF-8、envelope 或 payload 不合法，包含未知字段时也会拒绝 |
| `401` | Token 缺失或不匹配 |
| `413` | body 超过 `server.max_request_bytes` |
| `415` | `Content-Type` 不是 `application/json` |
| `422` | envelope 不是 notification，或请求 revision 不为 `0` |

相同请求可以安全重试；应复用原 `id` 和 `dedupe_key`，不要为重试生成新的业务键。
`accepted` 不等于 `shown`：当时没有 ready 设备时，事件仍会进入 Bridge 内存历史，但
当前版本不会在设备稍后连接时补播；实际展示结果通过 `shown` / `completed` ACK 查询。
HTTP 与 WebSocket 当前都是明文传输，只允许在可信本机或受信任局域网使用。

---

## 4. Herdr Agent 通知

### 4.1 状态事实源

只接受 Herdr 对外 `AgentStatus`：

```text
working
blocked
done
idle
unknown
```

禁止创建：

```text
agent.failed
agent.cancelled
agent.stalled
agent.waiting_user
agent.started
```

Herdr 的 `blocked` 已承载需要输入、批准、问题和部分集成错误阻塞；Agent Beacon 不做二次推断。

### 4.2 事件映射

| `kind` | 状态转移 | theme | urgency | priority | 展示 |
|---|---|---|---|---:|---:|
| `agent.blocked` | 非 blocked -> blocked | yellow | attention | 75 | 7000 ms |
| `agent.done` | working/blocked -> done | green | normal | 50 | 4000 ms |
| `agent.herdr_offline` | Herdr 断开超过阈值 | yellow | attention | 72 | 6000 ms |
| `agent.herdr_restored` | Herdr 恢复 | green | normal | 35 | 3500 ms |

`working / idle / unknown` 只更新 Agent 页面。

### 4.3 blocked 文案

```text
标题：Agent 需要关注
详情：<display_name> · <custom_status 或 Herdr 可见状态标签>
```

若没有 secondary text：

```text
详情：<display_name> · BLOCKED
```

### 4.4 done 文案

```text
标题：Agent 已完成
详情：<display_name> · 等待查看
```

如果 Herdr 提供自定义 done label，可使用：

```text
详情：<display_name> · <state_label.done>
```

### 4.5 初始快照与重连

- 首次 `session.snapshot` 只建立状态，不批量播放 done/blocked；
- WebSocket/Herdr 重连后的 snapshot 不补播历史 done；
- 若重连后 blocked 状态仍存在且用户从未收到该 `pane_id + session` 的 blocked，可生成一次；
- done 只在在线增量状态转移时通知；
- Herdr provider 保存最近状态，以 pane/session 为键；
- pane 关闭后删除状态和排队通知。

### 4.6 supersede

同一 pane：

```text
blocked -> done
```

处理：

- 未展示的 blocked 从队列删除；
- 正在显示 blocked 时，done 可在当前最短展示时间 1500 ms 后替换；
- 不允许 done 先显示后又播放旧 blocked。

### 4.7 Agent 聚合

2 秒聚合窗口内允许聚合同 kind 的普通 Agent 通知：

```text
3 个 Agent 已完成
Chrome Plugin · Docs · +1
```

blocked 默认不聚合，以免隐藏需要处理的对象；当 4 个以上同时 blocked 时可显示：

```text
4 个 Agent 需要关注
优先查看 Chrome Plugin
```

并在 Agent 页完整列出。

---

## 5. Codex 配额通知

### 5.1 数据范围

每个 `CODEX_HOME` 独立维护：

```text
weekly_remaining_percent
weekly_reset_at
reset_cards_available
nearest_reset_card_expires_at
freshness
```

没有 5 小时窗口。

中转站余额默认只显示，不产生低余额通知；当前只对凭证无效或数据长期失败产生系统通知。

### 5.2 一周额度阈值

每个 Home 独立触发：

| kind | 向下跨越 | theme | urgency | priority | 展示 |
|---|---:|---|---|---:|---:|
| `quota.weekly_30` | 30% | yellow | normal | 45 | 5000 ms |
| `quota.weekly_15` | 15% | yellow | attention | 65 | 6000 ms |
| `quota.weekly_5` | 5% | red | urgent | 90 | 8000 ms |
| `quota.weekly_reset` | 检测到新周且额度恢复 | green | normal | 42 | 4000 ms |
| `quota.home_stale` | 单 Home 数据 stale | yellow | attention | 58 | 6000 ms |

文案：

```text
Codex 一周额度偏低
MAIN · 剩余 28%

Codex 一周额度需要关注
VS · 剩余 14%

Codex 一周额度即将用尽
MAIN · 剩余 4%

Codex 一周额度已重置
VS · 当前 100%
```

### 5.3 阈值重武装

同一 Home、同一周：

- 只在向下跨过阈值时触发；
- 轮询抖动不得重复触发；
- 额度回升到阈值以上至少 5 个百分点后可重新武装；
- 检测到 `weekly_reset_at` 或周标识变化时全部重新武装；
- 使用 Home id 参与去重。

去重键：

```text
quota:<home_id>:weekly:<window_id>:threshold:<value>
quota:<home_id>:weekly:<window_id>:reset
quota:<home_id>:stale
```

### 5.4 重置卡

重置卡张数和最近过期时间在 Codex 页面持续显示。

第一阶段不默认弹出：

```text
卡新增
卡减少
卡即将过期
```

理由：当前没有足够产品规则判断卡的使用/过期是否值得打断。协议保留页面字段，但通知 kind 不预埋。

异常：

- `available > 0` 且最近过期时间已在过去：只在页面标红并记录 provider error；
- 数据无法获取：显示 `—`，不得生成虚假 0；
- adapter schema 错误归入 `system.provider_error`。

### 5.5 中转站异常

| kind | 条件 | theme | urgency | priority |
|---|---|---|---|---:|
| `system.relay_key_invalid` | `isValid=false` | yellow | attention | 70 |
| `system.relay_stale` | 超过 stale_after 无成功请求 | yellow | normal | 42 |
| `system.relay_restored` | 恢复 | green | normal | 30 |

同一异常只提醒一次，恢复后才可再次触发。

---

## 6. 天气与带伞通知

### 6.1 触发对象

不是“任何时候会下雨”，而是：

> **下一次出门窗口是否需要带伞。**

出门窗口：

```text
午饭 12:00
下班 19:00
```

目标选择和带伞算法见 `ui.md`。

### 6.2 事件映射

| kind | 条件 | theme | urgency | priority | 展示 |
|---|---|---|---|---:|---:|
| `weather.umbrella_required` | next_outing false/unknown -> true | red | attention | 72 | 6500 ms |
| `weather.umbrella_reminder` | T-30，仍需带伞且未提醒 | red | attention | 74 | 6500 ms |
| `weather.umbrella_cleared` | true -> false | blue | normal | 28 | 3500 ms，默认关闭 |
| `system.weather_stale` | 无法可靠判断 | yellow | normal | 40 | 5000 ms，默认只显示一次 |
| `system.weather_restored` | 数据恢复 | green | normal | 25 | 3000 ms，默认关闭 |

### 6.3 文案

```text
下班记得带伞
有雨

午饭记得带伞
遮阳

下班记得带伞
有雨
```

降雨通知来源为 QWeather；遮阳通知来源为 Open-Meteo。

数据 stale 时：

```text
天气数据暂不可用
无法判断下次是否带伞
```

不得在数据 stale 时显示“不用带伞”。

### 6.4 episode 和去重

同一出门目标生成：

```text
weather:<location>:<slot>:<target_at>:umbrella
weather:<location>:<slot>:<target_at>:t-30
```

同一 target 最多：

1. 结论首次变为需要带伞；
2. T-30 提醒一次。

如果结论在此前后反复抖动：

- 连续两次天气刷新均为 true 后才触发首次提醒；或
- 存在 `precip>0` / 明确降水 icon 时允许立即触发；
- true -> false 需要连续两次刷新确认后清除；
- episode 过了目标时间 + 60 分钟即失效。

### 6.5 与 Agent 通知的关系

默认优先级：

```text
agent.blocked             75
weather.umbrella_reminder 74
weather.umbrella_required 72
agent.done                50
```

因此：

- blocked 可以打断首次带伞提醒；
- T-30 带伞提醒不打断 blocked；
- 带伞提醒可打断 done；
- 被 blocked 打断的带伞提醒可在未过期时重播一次。

---

## 7. 系统通知

| kind | 条件 | theme | urgency | priority | 默认 |
|---|---|---|---|---:|---|
| `system.bridge_offline` | ESP32 与 Mac 失联超过 45 秒 | yellow | attention | 78 | 显示 |
| `system.bridge_restored` | 连接恢复 | green | normal | 35 | 可关闭 |
| `system.protocol_error` | 消息版本/结构不可处理 | red | urgent | 96 | 显示 |
| `system.provider_error` | 单 Provider 持续失败 | yellow | normal | 44 | 去重显示 |
| `system.provider_restored` | Provider 恢复 | green | normal | 28 | 默认关闭 |
| `system.clock_invalid` | 设备时间不可用且无法校准 | yellow | attention | 62 | 显示 |

系统通知必须标明来源：

```text
Herdr 连接中断
QWeather 数据过期
MAIN 配额采集失败
0-0 API Key 无效
```

不要统一写成模糊的“系统错误”。

---

## 8. 设备端队列

### 8.1 数据结构

```text
current_notification
pending_priority_queue
reserved_urgent_slot
dedupe_index
supersede_index
recent_history
persistent_badges
```

建议容量：

```text
pending 普通容量     12
urgent 保留槽         2
recent_history       64 IDs
persistent_badges    16
```

### 8.2 排序键

```text
urgency_rank DESC
priority DESC
created_at ASC
id ASC
```

紧急度排名：

```text
urgent=3
attention=2
normal=1
```

### 8.3 接收流程

```text
收到消息
  ↓
验证 envelope / version / size / UTF-8
  ↓
检查 expires_at
  ↓
检查 id 是否处理过
  ↓
检查 dedupe_key / supersede_key
  ↓
更新或删除旧通知
  ↓
判断是否打断 current
  ↓
显示或入队
  ↓
发送 ACK
```

### 8.4 打断规则

新通知可打断当前通知，当：

```text
new.urgency_rank > current.urgency_rank
```

或：

```text
new.urgency_rank == current.urgency_rank
AND new.priority >= current.priority + 10
```

保护：

- 当前通知至少显示 1000 ms 后才允许普通打断；
- `system.protocol_error` 可立即打断；
- 被打断通知是否重播由 `replay_after_interrupt` 控制；
- 同一通知不超过 `max_replays`；
- 过期通知不重播。

### 8.5 默认重播

| kind | replay | max |
|---|---:|---:|
| agent.blocked | 是 | 1 |
| agent.done | 否 | 0 |
| quota.weekly_30 | 否 | 0 |
| quota.weekly_15 | 是 | 1 |
| quota.weekly_5 | 是 | 1 |
| weather.umbrella_required | 是 | 1 |
| weather.umbrella_reminder | 是 | 1 |
| system.protocol_error | 是 | 1 |

### 8.6 队列满

淘汰顺序：

1. 已过期；
2. 被 supersede；
3. 最低优先级 normal；
4. 最旧 normal；
5. 最低优先级 attention。

不得淘汰：

- urgent 保留槽中的有效通知；
- 当前正在展示的通知；
- 协议错误和桥接离线告警，除非被更高优先级同类替换。

被丢弃消息返回 ACK：

```json
{
  "type": "ack",
  "id": "evt_...",
  "status": "dropped",
  "reason": "queue_full_lower_priority"
}
```

---

## 9. 去重、替换和历史

### 9.1 三种键

```text
id             传输幂等；同一消息只处理一次
dedupe_key     同一业务事实只提示一次
supersede_key  新状态淘汰同一对象的旧状态
group_key      短时间聚合多个相似对象
```

### 9.2 典型键

```text
agent:<pane_id>:<session>:blocked:<transition_revision>
agent:<pane_id>:<session>:done:<transition_revision>
quota:<home_id>:weekly:<window_id>:threshold:15
weather:<location>:leave:<target_at>:umbrella
system:provider:qweather:stale
```

`transition_revision` 优先使用递增的 Herdr revision；Herdr 未提供可区分的
revision 时，由 Mac Bridge 为每次真实状态转移生成本地标识。

### 9.3 recent_history

至少保存：

```text
id
dedupe_key
result: shown | expired | dropped | superseded
shown_at
```

设备重启后可只持久化最近 32 个 ID 和最新 revision，完整历史由 Mac 保存。

---

## 10. TTL

| kind | TTL |
|---|---|
| agent.blocked | 状态变化前，最长 30 分钟 |
| agent.done | 60 秒 |
| quota.weekly_30 | 60 秒 |
| quota.weekly_15 | 5 分钟 |
| quota.weekly_5 | 10 分钟 |
| quota.home_stale | 数据恢复前，最长 15 分钟 |
| weather.umbrella_required | target + 60 分钟 |
| weather.umbrella_reminder | target + 30 分钟 |
| system.bridge_offline | 恢复前，最长 30 分钟 |
| system.protocol_error | 10 分钟或新版本修复前 |

设备收到已过期通知：

- 不展示；
- 返回 `expired` ACK；
- 不写 persistent badge。

---

## 11. ACK

状态：

```text
received
queued
shown
completed
interrupted
superseded
expired
dropped
invalid
duplicate
```

示例：

```json
{
  "v": 2,
  "type": "ack",
  "id": "evt_01K...",
  "device_id": "agent-beacon-01",
  "status": "shown",
  "at": "2026-07-14T14:30:02+08:00"
}
```

Mac 用 ACK 计算：

- 事件到显示 P50/P95；
- 队列等待时间；
- 被打断比例；
- 丢弃数量；
- 重复/过期数量。

---

## 12. 持续状态

全屏通知消失不代表事实消失：

| 状态 | 页面保留 |
|---|---|
| Herdr blocked | Agent 行持续黄色 `!` |
| Herdr done | 保持 Herdr 的 done 展示，直到 Herdr 变更 |
| Codex <15% | 对应 Home 行黄色 |
| Codex <5% | 对应 Home 行红色 |
| Home stale | 对应 Home 行 `!` |
| 下一出门需带伞 | Weather 推荐条红色 |
| 天气判断未知 | 推荐条黄色 |
| Provider stale | 右上角 `◐`，对应页标记 |

固件不得自己清除业务持续状态；只响应 snapshot/patch。

---

## 13. 静默策略

当前设备主要放在桌面，不需要完全屏蔽通知，但允许降低打扰：

```yaml
notifications:
  quiet_hours:
    enabled: true
    start: "23:00"
    end: "08:00"
  dim_normal_during_quiet: true
  suppress_green_during_quiet: true
```

静默时段：

- normal 仅显示 2500 ms；
- green 可只更新页面；
- attention/urgent 仍显示；
- 屏幕亮度遵循夜间上限；
- 不影响事件 ACK 和持续状态。

---

## 14. 固件任务边界

```text
transport tasks
  校验 USB COBS/CRC 或重组 WebSocket 帧，投递原始消息

protocol_task
  解析、校验、生成内部 notification struct

notification_task
  去重、supersede、队列、打断、TTL、ACK

ui_task
  唯一允许调用 LVGL；执行全屏展示与页面恢复

storage_task
  持久化 revision、最近 ID 和必要配置
```

禁止：

- USB/WebSocket 接收回调直接创建/删除 LVGL 对象；
- UI 动画回调修改通知堆；
- 多任务无锁访问 pending queue；
- 固件依据 category 猜测主题和优先级。

---

## 15. 测试场景

### 15.1 Agent

1. 初始 snapshot 有 3 个 done，不弹历史通知；
2. working -> blocked，黄色全屏；
3. blocked -> done，绿色 supersede 黄色；
4. working -> idle，不弹；
5. unknown -> working，不弹；
6. 10 个 done 在 2 秒内聚合；
7. Herdr 断开 5 秒不提醒，超过阈值提醒一次；
8. Herdr 恢复后状态完整 resync。

### 15.2 Codex

1. MAIN 31% -> 29%，黄色一次；
2. MAIN 在 29/30 抖动不重复；
3. VS 16% -> 14%，不影响 MAIN；
4. MAIN 6% -> 4%，红色 urgent；
5. 检测新周，绿色 reset；
6. 一个 Home stale，另一个保持正常；
7. adapter 无重置卡字段时显示未知，不显示 0；
8. 协议和 UI 不存在 5 小时字段。

### 15.3 Weather

1. 午饭窗口 false -> true，红色；
2. 下班窗口首次 true，红色；
3. T-30 仍为 true，再提醒一次；
4. pop 在 39/41 抖动，双采样去抖；
5. `precip>0` 时立即 true；
6. QWeather stale 时显示判断未知；
7. stale 时不得出现“不用带伞”；
8. 带伞提醒被 blocked 打断，未过期时重播一次。

### 15.4 队列

1. done 正在显示，umbrella 打断；
2. umbrella 正在显示，blocked 打断；
3. blocked 正在显示，done 同一 Agent supersede；
4. 100 条 normal 灌入，urgent 保留槽仍可用；
5. 重复 ID 不重复显示；
6. revision gap 后请求 snapshot；
7. 已过期通知返回 expired；
8. 连续 1000 条通知无内存泄漏。

### 15.5 Bridge 接入

1. 进程内合法 notification 取得 accepted receipt，并广播一次；
2. 同一 `dedupe_key` 经进程内和 HTTP 两个入口仍只接收一次；
3. HTTP 缺少或使用错误 Token 时返回 401；
4. HTTP 未知字段、错误 enum、非 notification envelope 和非零 revision 均被拒绝；
5. HTTP body 超限返回 413，且事件历史和 revision 不变化；
6. 已过期 HTTP 通知返回 410，不广播；
7. ready 设备收到的 envelope 保留生产者 ID，并使用 Bridge 新分配的 revision；
8. 没有 ready 设备时 accepted 不伪造 shown/completed ACK。

---

## 16. 完成定义

- [ ] 只存在 agent、quota、weather、system 四类通知；
- [ ] Agent 只使用 Herdr `blocked` 和 `done` 触发全屏；
- [ ] 没有 agent.failed 等自定义状态；
- [ ] 两个 CODEX_HOME 独立阈值和去重；
- [ ] 没有 5 小时窗口通知；
- [ ] 重置卡只做页面信息，不擅自打扰；
- [ ] 带伞通知只针对下一出门窗口；
- [ ] 支持优先队列、打断、supersede、聚合和 TTL；
- [ ] 支持 ACK 和端到端延迟统计；
- [ ] 当前没有任务、邮件、消息和评测通知。
