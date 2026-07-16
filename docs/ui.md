# Agent Beacon UI 与数据视图规格（ui.md）

> 本文是 `320 × 172` 横屏 UI、页面数据模型和数据源适配的唯一权威规格。
> 第一阶段只实现：**Codex 全局 Token 速度与配额、Herdr Agent 状态、天气、全屏通知、诊断页**。
> 当前明确不实现：任务、邮件、聊天消息、评测页面及其 Provider。

---

## 1. 页面结构

### 1.1 页面列表

普通轮播有三种页面，其中 Agents、Weather 始终参与，Codex 按活跃状态动态参与：

```text
PAGE_CODEX
PAGE_AGENTS
PAGE_WEATHER
```

另有两个非轮播层：

```text
OVERLAY_NOTIFICATION   全屏彩色通知
PAGE_DIAGNOSTICS       手动进入的诊断页
```

默认轮播时长：

| 页面 | 默认停留 |
|---|---:|
| Codex | 15 秒，仅 `agents.codex_active=true` 时参与 |
| Agents | 6 秒 |
| Weather | 8 秒 |

通知出现时暂停轮播；通知结束后恢复原页面以及原页面剩余停留时间。

Herdr 首次报告任意 Codex session 从非活跃变为 `working` 时，立即切换到 Codex
速度仪表盘并开始完整 15 秒倒计时；保持 `working` 的后续 snapshot/patch 不得重复
续时。所有 Codex session 都不再是 `working` 或 Herdr 断连时，Codex 页退出轮播；
若当时正显示 Codex 页则立即切到 Agents，否则不打断当前 Agents/Weather 页。
全屏通知和手动诊断页保持更高显示优先级，此时只更新退出后的目标页面。

### 1.2 全局区域

逻辑分辨率：`320 × 172`，横屏。

```text
┌──────────────────────────────────┐
│ 标题/页面名             时间/连接 │ 22 px
├──────────────────────────────────┤
│                                  │
│             页面主体              │ 124 px
│                                  │
├──────────────────────────────────┤
│ 数据新鲜度 / 页码 / 状态           │ 26 px
└──────────────────────────────────┘
```

约束：

- 页面中不要同时强调超过 3 个结论；
- 不使用触摸手势；
- BOOT 短按切下一页，连按三下立即固定显示 Codex Token 速度页并暂停轮播；
- 固定显示期间再短按一次恢复轮播，并从 Codex 页的完整 15 秒停留开始；
- 页面使用固定网格，数据变化不得导致整体跳动；
- 所有动态字符串先由 Mac 服务压缩，固件仍按像素宽度截断；
- 时间统一按设备配置时区显示，默认 `Asia/Shanghai`；
- 右上角连接状态只显示一个小图标，不占用主体空间。

### 1.3 连接状态图标

```text
●  WebSocket 在线且数据新鲜
◐  在线但部分 Provider stale
○  设备离线，正在显示缓存
×  协议或配置错误
```

颜色只作为辅助信息，图标形状必须不同。

---

## 2. Codex 页面

### 2.1 产品目标

同一页同时显示：

1. 所有本机 patched Codex 进程的实时全局 completion-output Token 速度；
2. 两个独立 `CODEX_HOME` 的一周配额；
3. 每个 `CODEX_HOME` 的重置卡张数和最近过期时间；
4. `0-0.pro` 中转站余额。

Codex 当前产品要求中不再显示 5 小时滑动窗口。代码、协议、Mock 和 UI 中均不得保留 `five_hour`、`rolling_5h`、`primary_window` 等字段。

### 2.2 配置

Mac 服务支持恰好 1～2 个 Codex Home，当前部署配置两个：

```yaml
providers:
  codex:
    enabled: true
    refresh_interval: 60s
    homes:
      - id: main
        label: MAIN
        path: ~/.codex
      - id: vs
        label: VS
        path: ~/.codex-vs
    adapter:
      command:
        - "@bridge"
        - codex-adapter
    token_rate:
      enabled: true
      socket_path: "~/Library/Application Support/AgentBeacon/codex-token-rate.sock"
      state_file: "~/Library/Application Support/AgentBeacon/codex-token-rate.json"
      refresh_interval: 200ms
      stale_after: 2s
```

要求：

- `id` 稳定，用于去重、缓存和通知；
- `label` 最多 8 个 ASCII 字符或 4 个中文字符；
- `path` 展开 `~` 后转为绝对路径；
- 每个 Home 独立采集、独立超时、独立 stale；
- 一个 Home 失败不得清空另一个 Home；
- Mac 服务调用 adapter 时必须设置对应的 `CODEX_HOME` 环境变量；
- 不允许把两个 Home 的 session、账号或额度混合统计。

Token 速度与 Home 无关，只允许使用 daemon 已聚合的全局指标；Bridge 不得把两个
Home 的周配额相加、换算成速度，或自行扫描进程推算速度。

### 2.3 Codex adapter 输出

每次调用只返回一个 Home：

```json
{
  "schema_version": 1,
  "home_id": "main",
  "weekly": {
    "remaining_percent": 18,
    "reset_at": "2026-07-15T11:39:00+08:00"
  },
  "reset_cards": {
    "available": 2,
    "nearest_expires_at": "2026-07-20T23:59:59+08:00"
  },
  "observed_at": "2026-07-14T14:30:00+08:00"
}
```

字段规则：

- `weekly.remaining_percent`：`0～100`，代表剩余比例；
- `weekly.reset_at`：可空；无法可靠获得时显示 `—`，不得推测；
- `reset_cards.available`：非负整数；
- `nearest_expires_at`：当 `available=0` 时必须为 `null`；
- `observed_at`：数据采集时间；
- adapter 不得输出 5 小时窗口字段；
- adapter stdout 只允许一个 JSON 对象，stderr 用于诊断日志。

### 2.4 实时 Token 速度 Provider

事实源是 `rust-v0.144.4` patched Codex 配套的 `codex-token-rate-daemon` 状态文件。
指标名称必须精确为：

```text
completion_output_tokens_per_second
```

该指标是所有连接同一 Unix Datagram socket 的本机 patched Codex 进程之近似
completion-output 速率，包含用户可见助手文本与流式工具调用参数。它不包含 input
token、hidden reasoning、工具输出、认证信息或工作目录。daemon 以本地接收时间维护
2 秒滑动窗口并输出 EMA；响应进入工具执行后维持该流最后一次速度，工具完成后释放。
Bridge 不得对它再次平滑或按 Home 拆分。

daemon v1 状态文件示例：

```json
{
  "version": 1,
  "metric": "completion_output_tokens_per_second",
  "estimated": true,
  "tokens_per_second": 42.7,
  "raw_tokens_per_second": 48.0,
  "active_sessions": 2,
  "active_streams": 3,
  "tool_active_streams": 1,
  "window_ms": 2000,
  "updated_at_unix_ms": 1784082660000
}
```

Bridge 规则：

1. 状态文件必须是普通文件，权限不得宽于 `0600`；
2. 严格校验 version、metric、estimated、非负有限速率、活动计数（包括
   `tool_active_streams`）和时间戳；`tool_active_streams` 仅用于 daemon 合约校验，
   当前不下发到设备；
3. 默认每 200ms 读取，只有速度、活动计数或 freshness 的可见值变化才下发；
4. 短暂读失败保留上次值并标记 `cached`，超过 2 秒后清空数值并标记 `stale`；
5. daemon 正常运行但没有活跃输出时，`0.0` 是真实值，不得显示成“无数据”；
6. daemon 未启动或状态过期时显示 `--`，不得把缺失数据伪装成 `0.0`。

### 2.5 中转站余额 Provider

接口：

```bash
curl -sS 'https://api.0-0.pro/v1/usage' \
  -H "Authorization: Bearer $ZERO_API_KEY" \
  -H 'Accept: application/json'
```

已知响应示例：

```json
{
  "balance": 14.16,
  "isValid": true,
  "mode": "unrestricted",
  "planName": "wallet balance",
  "remaining": 14.16,
  "unit": "USD"
}
```

Mac Provider 规范：

```yaml
providers:
  relay_balance:
    enabled: true
    endpoint: https://api.0-0.pro/v1/usage
    secret_name: zero-api-key
    refresh_interval: 5m
    timeout: 5s
    stale_after: 20m
```

解析规则：

1. HTTP 必须成功且 JSON 可解析；
2. `isValid` 必须为 `true`；
3. 显示值优先使用数值型 `remaining`；缺失时回退到 `balance`；
4. `unit=USD` 显示 `$`；其他单位显示原单位；
5. `mode`、`planName` 只保留在诊断数据，不占用主 UI；
6. API Key 只存在 Mac 端，不发送到 ESP32；
7. 日志不得输出 Authorization 头或完整响应中的敏感字段；
8. 请求失败时保留上次成功值并标记 stale，不立即显示 `$0`；
9. `isValid=false` 时显示 `KEY INVALID`，并产生系统黄色通知一次。

建议通过桥接服务的 secret 命令写入 macOS Keychain：

```bash
agent-beacon-bridge secret set zero-api-key --from-env ZERO_API_KEY
```

不要依赖交互式 shell 环境变量自动传入 LaunchAgent。

### 2.6 Codex 页面布局

```text
┌──────────────────────────────────┐
│ TOKEN 速度                ● 在线 │
│       0   60       │ 油量·周配额 │
│    ╭────────╮      │ MAIN    18% │
│  240  42.7  120    │ ██░░░░░░░░  │
│    ╰──╱─────╯      │ 重07/15 卡2 │
│   估算 tok/s       │ VS      64% │
│ 2 会话 · 3 流      │ ██████░░░░  │
│                    │ 0-0   $14.16│
└──────────────────────────────────┘
```

具体网格：

```text
Header                  y=0..22
Token meter (left)      x=9..159, y=24..170
Vertical divider        x=169, y=29..164
Quota title (right)     x=178..312, y=27..44
Home A fuel row         x=178..312, y=43..89
Home B fuel row         x=178..312, y=91..137
Relay balance footer    x=176..312, y=141..165
```

左侧速度表：

- 0～240 的 240 度汽车仪表盘刻度、进度弧与指针；
- 中央大号数字显示实际 `tokens_per_second`，保留一位小数；超过 240 时指针封顶，
  数字仍显示真实值；
- 有效速度从非零归零时，数字立即显示 `0.0`，指针在 1.5 秒内缓降至零；
- 下方显示 `估算 tok/s` 与活动会话/stream 数；
- `cached` 使用黄色，`stale/unknown` 使用灰色并显示 `--` 和文字原因；
- 颜色只表达 freshness，不把高 Token 速度误标成危险状态。

右侧每个 Home 行：

- 左上：Home label；
- 中上：一周剩余百分比，大号数字；
- 下方：进度条；
- 最下方左侧：一周重置日期；右侧：`卡 N · MM/DD`；
- Home stale 时整行降低饱和度并显示 `!`，但保留上次值。

进度条颜色：

```text
>30%     常规蓝/主题前景色
15～30%  黄色
5～15%   橙红
<5%      红色
stale    灰色斜线或感叹号
```

显示格式：

```text
一周剩余          18%
一周重置          三 11:39 / 07-15
重置卡            卡 2
最近卡过期        14h 后 / 3天后 / 07-20
余额              $14.16
```

最近过期时间格式：

- `<24h`：`14h 后`；
- `<7d`：`3天后`；
- 其他：`MM/DD`；
- 已过期但卡数仍大于 0：显示红色 `数据异常`，不得静默修正；
- 卡数为 0：显示 `卡 0 · —`。

### 2.7 Codex 页面协议

```json
{
  "codex": {
    "homes": [
      {
        "id": "main",
        "label": "MAIN",
        "weekly_remaining_percent": 18,
        "weekly_reset_at": "2026-07-15T11:39:00+08:00",
        "reset_cards_available": 2,
        "nearest_reset_card_expires_at": "2026-07-20T23:59:59+08:00",
        "updated_at": "2026-07-14T14:30:00+08:00",
        "freshness": "fresh"
      },
      {
        "id": "vs",
        "label": "VS",
        "weekly_remaining_percent": 64,
        "weekly_reset_at": "2026-07-18T09:00:00+08:00",
        "reset_cards_available": 1,
        "nearest_reset_card_expires_at": "2026-07-18T23:59:59+08:00",
        "updated_at": "2026-07-14T14:30:00+08:00",
        "freshness": "fresh"
      }
    ],
    "relay": {
      "remaining": 14.16,
      "unit": "USD",
      "is_valid": true,
      "updated_at": "2026-07-14T14:30:00+08:00",
      "freshness": "fresh"
    },
    "token_rate": {
      "tokens_per_second": 42.7,
      "active_sessions": 2,
      "active_streams": 3,
      "window_ms": 2000,
      "estimated": true,
      "updated_at": "2026-07-14T14:31:00+08:00",
      "freshness": "fresh"
    }
  }
}
```

固件最多渲染两个 Home；收到三个及以上视为协议错误并拒绝该条消息。

---

## 3. Agent 页面与 Herdr 集成

### 3.1 唯一状态来源

Agent 页面以 Herdr 为唯一状态事实源：

```text
https://github.com/ogulcancelik/herdr
```

桥接服务不得：

- 自己扫描 Codex/Claude/OpenCode 进程；
- 自己读取终端屏幕并重新做状态识别；
- 把 Agent 状态扩展成 Herdr 不存在的 `failed/cancelled/stalled`；
- 把工具调用、日志关键词或进程退出擅自映射成新状态。

Herdr 对外状态必须原样保留：

```text
working
blocked
done
idle
unknown
```

其中 Herdr 的底层语义状态是 `working / blocked / idle / unknown`；未查看的 `idle` 会在展示层表现为 `done`。Agent Beacon 直接消费 Herdr 对外的 `AgentStatus`，不自行重建 `done`。

### 3.2 Herdr API 接入

Herdr 使用本地 Unix Domain Socket 上的 NDJSON 协议。

默认 socket：

```text
~/.config/herdr/herdr.sock
```

命名 session：

```text
~/.config/herdr/sessions/<name>/herdr.sock
```

桥接服务配置：

```yaml
providers:
  herdr:
    enabled: true
    session: default
    socket_path: ""
    reconnect_max: 30s
    full_resync_interval: 60s
```

解析优先级：

1. 显式 `socket_path`；
2. 配置的 Herdr session；
3. 默认 socket。

启动流程：

1. 连接 socket；
2. 调用 `session.snapshot` 获取完整初始状态；
3. 建立 `events.subscribe` 长连接；
4. 至少订阅：
   - `pane.agent_status_changed`；
   - `pane.agent_detected`；
   - `pane.created`；
   - `pane.closed`；
   - `pane.exited`；
5. 对 presentation/custom status 无法从增量事件完整恢复时，重新请求 `session.snapshot`；
6. 每 60 秒执行一次完整 resync；
7. socket 重连后必须重新 snapshot，再重新订阅。

允许使用 `herdr api snapshot` 作为诊断或兼容回退，但实时模式不得仅靠 1 秒轮询 CLI。

### 3.3 Agent 数据模型

```json
{
  "agents": {
    "provider": "herdr",
    "connected": true,
    "codex_active": false,
    "updated_at": "2026-07-14T14:31:00+08:00",
    "items": [
      {
        "pane_id": "w1:p1",
        "terminal_id": "term_abc123",
        "workspace_id": "w1",
        "tab_id": "w1:t1",
        "agent": "codex",
        "display_name": "Chrome Plugin",
        "status": "blocked",
        "custom_status": "waiting approval",
        "title": "Fix extension auth",
        "focused": false,
        "revision": 42,
        "agent_session": {
          "source": "herdr:codex",
          "kind": "id",
          "value": "..."
        }
      }
    ]
  }
}
```

`codex_active` 由 Mac Bridge 统一派生，固件不从显示名猜测：Herdr 连接正常，且
至少一个 `status=working` 的会话通过 `agent_session.source=herdr:codex`、
`agent_session.agent=codex` 或顶层 `agent=codex` 识别为 Codex 时才为 `true`。
`blocked / done / idle / unknown` 均不算活跃；Herdr 断连时强制为 `false`。

字段优先级：

```text
display_name:
  Herdr workspace label
  -> workspace label + tab label（同 workspace 多 tab）
  -> Herdr display_agent / custom label
  -> Herdr agent label
  -> pane title
  -> pane_id

secondary text:
  Herdr custom_status
  -> Herdr agent label
  -> Herdr state label
  -> title
```

不得显示完整 cwd、session 路径或长终端内容。

### 3.4 Agent 页面排序

保持 Herdr 的注意力语义：

```text
blocked  > done > working > idle > unknown
```

同状态内：

1. 最近 revision/更新时间优先；
2. focused 优先只作为次级排序；
3. 最后按 display_name 稳定排序。

最多显示 4 行；更多 Agent 在标题中显示 `+N`。

### 3.5 Agent 页面布局

```text
┌──────────────────────────────┐
│ AGENTS  2W · 1B · 1D    14:42│
│ ! Chrome Plugin      BLOCKED │
│   waiting approval           │
│ ● CaseForge          WORKING │
│ ✓ Docs Agent            DONE │
│ · Review Bot            IDLE │
└──────────────────────────────┘
```

状态视觉：

| Herdr status | 图标 | 颜色 | 全屏通知 |
|---|---|---|---|
| `blocked` | `!` | 黄色 | 是，黄色 |
| `done` | `✓` | 绿色 | 是，绿色 |
| `working` | `●` | 蓝色 | 默认否 |
| `idle` | `·` | 灰色 | 否 |
| `unknown` | `?` | 暗灰 | 否 |

标题计数使用 Herdr 原状态缩写：

```text
W working
B blocked
D done
```

`idle` 和 `unknown` 在空间不足时不显示计数。

### 3.6 Agent 通知触发

只根据 Herdr 对外状态转移：

```text
任意非 blocked -> blocked
  黄色全屏：Agent 需要关注

working/blocked -> done
  绿色全屏：Agent 已完成
```

规则：

- 初次 snapshot 不批量播放所有 done；
- 重连后的 snapshot 只用于同步，不补播旧 done；
- 每个通知 key 至少包含 `pane_id + agent_session + target_status + transition_revision`；
- Herdr revision 未递增时，`transition_revision` 使用 Mac Bridge 产生的本地状态转移标识；
- 同一 Agent 从 blocked 转 done 时，done supersede 尚未展示的 blocked；
- `working`、`idle`、`unknown` 只更新页面；
- Herdr 离线超过 10 秒后生成一次黄色系统通知；
- 恢复连接生成绿色系统恢复通知，可配置关闭。

### 3.7 Herdr 兼容性

启动时执行：

```bash
herdr --version
herdr api schema --json
```

记录 Herdr 版本和 schema hash。若 schema 不兼容：

- Agent 页显示 `HERDR API 不兼容`；
- 不回退到自研状态识别；
- 诊断页展示版本和错误；
- 其他 Codex、天气页面继续工作。

---

## 4. 天气页面

### 4.1 数据源

使用和风天气开发者服务提供实况/预报，并由 Open-Meteo 卫星辐射提供直晒判断；
两者都由 Mac Provider 请求，ESP32 不持有天气凭证，也不接收卫星原始数组。

接口：

```text
GET /v7/weather/now
GET /v7/weather/72h
GET https://satellite-api.open-meteo.com/v1/archive
```

选择 `72h` 而不是只取 `24h`，用于当前时间已经晚于 19:00 时仍能得到下一工作日午饭和下班窗口。

官方文档：

- 实时天气：<https://dev.qweather.com/docs/api/weather/weather-now/>
- 逐小时天气：<https://dev.qweather.com/docs/api/weather/weather-hourly-forecast/>
- 卫星辐射：<https://open-meteo.com/en/docs/satellite-radiation-api>

配置：

```yaml
providers:
  weather:
    enabled: true
    provider: qweather
    api_host: ${QWEATHER_API_HOST}
    credential_secret: QWEATHER_JWT
    location: "120.16,30.27"
    location_label: "杭州"
    timezone: Asia/Shanghai
    refresh_interval: 10m
    current_stale_after: 30m
    hourly_stale_after: 45m
    lunch_time: "12:00"
    leave_time: "19:00"
    umbrella_window_before: 60m
    umbrella_window_after: 60m
    umbrella_pop_threshold: 40
    satellite_radiation:
      enabled: true
      latitude: 30.2163
      longitude: 120.1734
      lunch_refresh: "11:57"
      leave_refresh: "18:28"
      stale_after: 75m
```

`location` 可使用 LocationID 或经纬度。不要通过公网 IP 猜位置。

### 4.2 使用字段

实况：

```text
now.obsTime
now.temp
now.icon
now.text
now.precip
```

逐小时：

```text
hourly[].fxTime
hourly[].temp
hourly[].icon
hourly[].text
hourly[].pop
hourly[].precip
```

当前天气、午饭和下班天气在同一横排显示。午饭和下班仍分别取 12:00 和 19:00 的目标小时数据，但页面标签不显示具体时间。

### 4.3 工作日目标时段

页面固定存在两个 slot：

```text
LUNCH_SLOT  12:00
LEAVE_SLOT  19:00
```

日期选择：

```text
当前时间 < 当日 12:00
  午饭 = 今日 12:00
  下班 = 今日 19:00
  下一出门 = 午饭

当日 12:00 <= 当前时间 < 当日 19:00
  午饭 = 今日 12:00 的最后缓存值，标记“已过”并降低亮度
  下班 = 今日 19:00
  下一出门 = 下班

当前时间 >= 当日 19:00
  午饭 = 下一工作日 12:00，标签“明午”或日期
  下班 = 下一工作日 19:00，标签“明晚”或日期
  下一出门 = 下一工作日午饭
```

第一阶段“下一工作日”可简单使用下一自然日；周末/节假日工作日历为后续增强。配置允许 `roll_after_leave_to_next_day: true`。

12:00 已过去但无缓存时显示 `—`，不得拿 13:00 冒充 12:00。

### 4.4 带伞判断

显示值取目标整点；带伞判断使用目标时间周围窗口：

```text
[target - umbrella_window_before, target + umbrella_window_after]
```

默认：目标前 1 小时到目标后 1 小时。

对窗口内任一小时，满足以下任一条件即 `umbrella_required=true`：

1. `precip` 可解析为数值且 `> 0`；
2. `pop >= umbrella_pop_threshold`，默认 40%；
3. `icon` 属于配置维护的降水/雷暴/雨夹雪图标集合；
4. `text` 经标准化后属于降水状态集合，用作 icon 缺失时的回退。

禁止仅通过字符串包含一个“雨”字做唯一判断。图标集合和天气状态映射必须集中在 QWeather adapter 中并有测试。

在 QWeather 未要求雨伞时，使用对应时段最近一次 Open-Meteo 卫星结果补充遮阳判断：

```text
最后 3 个完整 10 分钟点分别取 GHI、Direct 中位数
Direct >= 300，或 GHI >= 550 且 Direct/GHI >= 35%：需要带伞·遮阳（high）
Direct >= 150，或 GHI >= 400 且 Direct/GHI >= 25%：需要带伞·遮阳（medium）
最新有效卫星点超过 75 分钟：不参与判断
```

有雨优先于遮阳；卫星“无需遮阳”不能覆盖未知的降雨判断。

建议模型：

```json
{
  "target": "leave",
  "target_at": "2026-07-14T19:00:00+08:00",
  "umbrella_required": true,
  "confidence": "high",
  "reason": "有雨",
  "max_pop": 70,
  "total_precip_mm": 1.2
}
```

置信度：

```text
high     precip > 0 或明确降水 icon
medium   pop 达阈值
unknown  数据缺失或 stale
```

数据 stale 时不得输出“不用带伞”，必须显示 `判断未知`。

### 4.5 天气页面布局

```text
┌──────────────────────────────┐
│ 杭州 · QWeather · 14:30 更新 │
│ 当前         午饭        下班 │
│ 31°          29°         27° │
│ 多云         阵雨        小雨 │
│                              │
│      下班·需要带伞·遮阳      │
└──────────────────────────────┘
```

区域：

```text
Header                y=0..23
Weather row           y=30..106
Current card          x=8..104
Lunch card            x=112..208
Leave card            x=216..312
Recommendation area   y=113..167
```

天气横排：

- 标题行显示位置、`QWeather`、实况观测时间和连接状态，观测时间格式为 `HH:MM 更新`；
- 当前、午饭、下班使用等宽卡片并保持在同一行；
- 每张卡依次显示标签、温度和天气文字；
- 午饭、下班标签不附加 `12:00`、`19:00` 等具体时间；
- 可在小字显示实况观测时间，但不显示湿度、风速等次要信息。

午饭/下班卡：

- 标签；
- 温度；
- 天气 icon 或最多 4 个中文字符；
- 已过去的午饭 slot 降低亮度并标记“已过”。

推荐条：

```text
需要带伞      54px 高红色背景 + 24px 强调文字
无需带伞      无背景色，仅显示中性文字
判断未知      黄色 + 问号
```

文字固定使用 `时段·需要/无需带伞·原因`，例如 `午饭·需要带伞·有雨`、
`下班·需要带伞·遮阳`、`下班·无需带伞·无雨`。未知时显示
`时段·判断未知·数据不足`。

推荐区只针对下一次出门窗口，不同时给两个建议；使用“午饭”或“下班”标签，不显示具体时间。

### 4.6 天气通知触发

当下一出门窗口的结论发生以下变化：

```text
false/unknown -> true
  生成红色全屏通知
  标题：下次出门记得带伞
  详情：19:00 · 小雨 70%

true -> false
  默认只更新天气页
  可配置生成蓝色“降水风险解除”通知
```

额外提醒：

- 到目标时间前 30 分钟，如果仍需带伞且该 episode 尚未提醒，可再提醒一次；
- 同一目标窗口同一降水 episode 最多两次：状态首次转为 true、T-30；
- 当前正在下雨但下一个窗口无降水，不自动生成“带伞”通知；页面仍如实显示当前天气；
- 极端天气预警不在本轮 UI 必做范围，后续接 QWeather Warning API 时再扩展。

### 4.7 天气页面协议

```json
{
  "weather": {
    "location": "杭州",
    "provider": "qweather",
    "current": {
      "observed_at": "2026-07-14T14:30:00+08:00",
      "temp_c": 31,
      "icon": "101",
      "text": "多云",
      "precip_mm": 0.0,
      "freshness": "fresh"
    },
    "lunch": {
      "target_at": "2026-07-14T12:00:00+08:00",
      "is_past": true,
      "temp_c": 29,
      "icon": "305",
      "text": "小雨",
      "pop": 60,
      "precip_mm": 0.5,
      "freshness": "cached"
    },
    "leave": {
      "target_at": "2026-07-14T19:00:00+08:00",
      "is_past": false,
      "temp_c": 27,
      "icon": "305",
      "text": "小雨",
      "pop": 70,
      "precip_mm": 0.7,
      "freshness": "fresh"
    },
    "next_outing": {
      "slot": "leave",
      "target_at": "2026-07-14T19:00:00+08:00",
      "umbrella_required": true,
      "confidence": "high",
      "reason": "有雨"
    },
    "updated_at": "2026-07-14T14:31:00+08:00"
  }
}
```

---

## 5. 总体 Snapshot

```json
{
  "v": 2,
  "type": "snapshot",
  "revision": 301,
  "ts": "2026-07-14T14:31:00+08:00",
  "payload": {
    "clock": {
      "timezone": "Asia/Shanghai",
      "server_time": "2026-07-14T14:31:00+08:00"
    },
    "codex": {},
    "agents": {},
    "weather": {},
    "system": {
      "bridge_online": true,
      "overall_freshness": "fresh"
    }
  }
}
```

协议 v2 明确删除：

```text
tasks
inbox
messages
evaluations
five_hour_window
```

固件收到旧字段应忽略，不渲染隐藏页面。

---

## 6. 刷新与推送

### 6.1 Mac 采集频率

| 数据 | 采集/同步 |
|---|---:|
| Codex Home | 60 秒 |
| 0-0 余额 | 5 分钟 |
| Herdr | socket 实时事件 + 60 秒全量校准 |
| QWeather 实况/逐小时 | 10 分钟 |
| 系统时钟 | WebSocket snapshot 校准 + ESP32 本地计时 |

### 6.2 推送规则

- 页面数据变化后发送 `state_patch`；
- 重要边界变化另发 `notification`；
- 多个 Provider 在 200 ms 内同时更新时合并 patch；
- ESP32 重连后只收完整 snapshot，不依赖旧 patch；
- 页面动画和网络接收解耦；
- `state_patch` 始终先更新内存状态，只对受影响的当前轮播页执行无淡入硬刷新；
- 非当前页的数据后台静默更新，切换到该页时再用最新状态渲染；
- 通知显示期间不刷新底层轮播页。

---

## 7. 诊断页

BOOT 长按或串口命令进入，显示：

```text
Firmware version
Bridge version
Protocol version
Wi-Fi RSSI
WebSocket state
Snapshot revision
SoC temperature
CPU usage (one-second window, aggregate across both cores)
Codex MAIN/VS freshness
Relay freshness
Herdr version/socket state
QWeather last success
Heap / PSRAM
```

诊断页不显示：

- CODEX_HOME 完整路径；
- API Key；
- Herdr socket 完整用户名路径；
- Agent session ID；
- 天气 JWT。

---

## 8. Mock 与截图验收

Mac Mock 必须提供固定样例：

1. 两个 Codex Home 都正常；
2. 一个 Home stale；
3. Home 额度 4%、卡 0；
4. 中转余额 `$14.16`；
5. 中转凭证无效；
6. Herdr working/blocked/done/idle/unknown 各一个；
7. Agent 超过 4 个；
8. 当前晴、午饭阵雨、下班小雨；
9. 下一出门带伞；
10. 天气数据 stale，判断未知。

固件需支持截图或 framebuffer dump。对每个样例做 golden image/人工截图验收：

- 无文字溢出；
- 两个 Home 对齐；
- 状态颜色一致；
- 当前、午饭和下班三列位置固定；
- 当前温度与午饭、下班天气在同一横排；
- 带伞建议只对应下一次出门窗口；
- 任务、邮件、消息、评测页面不存在。

---

## 9. 当前阶段完成定义

- [ ] 轮播只有 Codex、Agents、Weather 三页；
- [ ] Codex 页同时显示两个 `CODEX_HOME`；
- [ ] 每个 Home 显示一周剩余、一周重置、重置卡数量、最近卡过期时间；
- [ ] 无任何 5 小时窗口字段或 UI；
- [ ] 0-0 API 余额正确显示，密钥不下发设备；
- [ ] Agent 状态完全使用 Herdr `working/blocked/done/idle/unknown`；
- [ ] Herdr blocked 黄色通知，done 绿色通知；
- [ ] 当前、午饭和下班天气在同一横排显示；
- [ ] 午饭和下班标签不显示具体时间；
- [ ] 无需带伞时推荐区无背景色；
- [ ] 推荐条正确判断下一次出门是否带伞；
- [ ] 推荐条按“时段·需要/无需带伞·原因”显示有雨/遮阳原因；
- [ ] 降水风险首次出现时触发红色全屏通知；
- [ ] Provider 部分失败时其他页面仍正常；
- [ ] 当前阶段没有任务、邮件、消息和评测实现。
