# Agent Beacon Weather Provider — weather.md

> 本文是 Agent Beacon 天气模块的唯一实现规格。
> 数据源：和风天气（QWeather）API v7。
> 当前范围：实时天气、12:00 午饭天气、19:00 下班天气、下一次出门窗口的带伞判断及通知。

---

## 0. 原始天气架构的缺口

原始架构已写明：

```text
GET /v7/weather/now
GET /v7/weather/72h
```

但鉴权部分只有静态 `QWEATHER_JWT` 占位，没有覆盖：

- 独立 API Host；
- 项目 ID；
- 凭据 ID；
- Ed25519 私钥加载；
- JWT 动态签名和刷新；
- 401、403、429 等错误处理；
- 实况与逐小时数据的不同缓存周期；
- 12:00、19:00 预报选取和下一出门窗口计算。

因此天气实现以本文件为准。

---

## 1. 官方资料

- 身份认证：<https://dev.qweather.com/docs/configuration/authentication/>
- 项目和凭据：<https://dev.qweather.com/docs/configuration/project-and-key/>
- API Host：<https://dev.qweather.com/docs/configuration/api-host/>
- 实时天气：<https://dev.qweather.com/docs/api/weather/weather-now/>
- 逐小时天气预报：<https://dev.qweather.com/docs/api/weather/weather-hourly-forecast/>
- 天气图标代码：<https://dev.qweather.com/docs/resource/icons/>
- 错误码：<https://dev.qweather.com/docs/resource/error-code/>
- 缓存建议：<https://dev.qweather.com/docs/best-practices/cache/>
- 来源标识：<https://dev.qweather.com/docs/terms/attribution/>

不得使用旧公共 Host：

```text
api.qweather.com
devapi.qweather.com
geoapi.qweather.com
```

必须使用控制台中属于当前帐号的独立 API Host，例如：

```text
abc1234xyz.def.qweatherapi.com
```

---

## 2. 运行所需凭据

JWT 请求需要四项配置：

| 配置 | JWT 字段/用途 | 是否敏感 |
|---|---|---|
| API Host | 请求域名 | 否，但不要公开传播 |
| 项目 ID | Payload `sub` | 否 |
| 凭据 ID | Header `kid` | 否 |
| Ed25519 私钥 | 生成签名 | **是** |

用户已经具备：

- 和风天气凭据 ID；
- `~/.weather` 下按官方文档生成的 Ed25519 公钥和私钥。

仍需从和风天气控制台确认并配置：

- **项目 ID**；
- **独立 API Host**；
- 私钥文件的实际文件名。

公钥只用于上传到和风天气控制台和本地诊断，运行时请求 API 只需要私钥。

### 2.1 建议目录和权限

不强制重命名用户现有文件；路径必须可配置。推荐布局：

```text
~/.weather/
├── ed25519-private.pem
└── ed25519-public.pem
```

权限：

```bash
chmod 700 ~/.weather
chmod 600 ~/.weather/ed25519-private.pem
chmod 644 ~/.weather/ed25519-public.pem
```

要求：

- 私钥不得复制到仓库；
- 私钥不得进入 macOS 服务日志、错误日志或崩溃报告；
- 私钥不得发送给 ESP32；
- 不生成并长期保存静态 JWT 文件；
- LaunchAgent 必须直接读取配置指定的私钥路径；
- 配置中的 `~` 必须由程序显式展开，不能依赖 shell。

---

## 3. 配置

`config.yaml`：

```yaml
providers:
  weather:
    enabled: true
    provider: qweather

    # 控制台“设置”中的独立 Host，不包含路径。
    api_host: "abc1234xyz.def.qweatherapi.com"

    # 控制台项目 ID，对应 JWT Payload.sub。
    project_id: "YOUR_PROJECT_ID"

    # JWT 凭据 ID，对应 JWT Header.kid。
    credential_id: "YOUR_CREDENTIAL_ID"

    private_key_path: "~/.weather/ed25519-private.pem"
    public_key_path: "~/.weather/ed25519-public.pem" # 仅 doctor 可选使用

    # LocationID 或“经度,纬度”。坐标最多保留两位小数。
    location: "120.16,30.27"
    location_label: "杭州"
    timezone: "Asia/Shanghai"
    lang: "zh"

    schedule:
      lunch: "12:00"
      leave: "19:00"
      active_weekdays: [1, 2, 3, 4, 5] # ISO weekday：周一至周五

    refresh:
      now: 10m
      hourly: 30m
      request_timeout: 8s
      force_before_outing: [60m, 30m]

    cache:
      now_stale_after: 45m
      hourly_stale_after: 90m
      persist_last_good: true

    umbrella:
      window_before: 60m
      window_after: 60m
      pop_threshold: 40
      repeat_before_outing: 30m
```

配置校验失败时，Weather Provider 必须保持禁用并给出可行动错误，不得阻止 Codex、Herdr 和设备连接模块启动。

---

## 4. JWT 鉴权

### 4.1 Header

只设置：

```json
{
  "alg": "EdDSA",
  "kid": "YOUR_CREDENTIAL_ID"
}
```

可以省略 `typ`。禁止加入自定义敏感字段。

### 4.2 Payload

```json
{
  "sub": "YOUR_PROJECT_ID",
  "iat": 1710000000,
  "exp": 1710000900
}
```

规则：

- `sub`：项目 ID，不是凭据 ID；
- `iat`：当前 UNIX 时间减 30 秒，容忍时钟微小偏差；
- `exp`：当前时间加 15 分钟；
- 和风天气允许的 JWT 最长有效期为 24 小时，但本项目使用 15 分钟；
- 本地缓存 JWT，在距离过期 2 分钟时刷新；
- Mac 系统时间明显错误时停止请求并在 doctor 中报错；
- 不加入 `iss`、`aud`、`nbf`；
- 使用 Base64URL 无填充编码；
- 使用 Ed25519/EdDSA 签名。

### 4.3 Go 标准库实现

不要为了 JWT 引入大型依赖；Go 标准库已经支持 Ed25519 和 PKCS#8 私钥。

```go
package qweather

import (
    "crypto/ed25519"
    "crypto/x509"
    "encoding/base64"
    "encoding/json"
    "encoding/pem"
    "errors"
    "fmt"
    "os"
    "sync"
    "time"
)

type jwtHeader struct {
    Alg string `json:"alg"`
    Kid string `json:"kid"`
}

type jwtPayload struct {
    Sub string `json:"sub"`
    Iat int64  `json:"iat"`
    Exp int64  `json:"exp"`
}

type JWTSigner struct {
    credentialID string
    projectID    string
    privateKey   ed25519.PrivateKey

    mu        sync.Mutex
    cached    string
    expiresAt time.Time
}

func LoadJWTSigner(privateKeyPath, credentialID, projectID string) (*JWTSigner, error) {
    if credentialID == "" || projectID == "" {
        return nil, errors.New("qweather credential_id and project_id are required")
    }

    data, err := os.ReadFile(privateKeyPath)
    if err != nil {
        return nil, fmt.Errorf("read qweather private key: %w", err)
    }

    block, _ := pem.Decode(data)
    if block == nil || block.Type != "PRIVATE KEY" {
        return nil, errors.New("qweather private key must be an unencrypted PKCS#8 PEM PRIVATE KEY")
    }

    parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
    if err != nil {
        return nil, fmt.Errorf("parse qweather PKCS#8 private key: %w", err)
    }

    key, ok := parsed.(ed25519.PrivateKey)
    if !ok {
        return nil, errors.New("qweather private key is not Ed25519")
    }

    return &JWTSigner{
        credentialID: credentialID,
        projectID:    projectID,
        privateKey:   key,
    }, nil
}

func (s *JWTSigner) Token(now time.Time) (string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.cached != "" && now.Before(s.expiresAt.Add(-2*time.Minute)) {
        return s.cached, nil
    }

    iat := now.Unix() - 30
    exp := now.Add(15 * time.Minute).Unix()

    headerJSON, err := json.Marshal(jwtHeader{
        Alg: "EdDSA",
        Kid: s.credentialID,
    })
    if err != nil {
        return "", err
    }

    payloadJSON, err := json.Marshal(jwtPayload{
        Sub: s.projectID,
        Iat: iat,
        Exp: exp,
    })
    if err != nil {
        return "", err
    }

    enc := base64.RawURLEncoding
    header := enc.EncodeToString(headerJSON)
    payload := enc.EncodeToString(payloadJSON)
    signingInput := header + "." + payload

    signature := ed25519.Sign(s.privateKey, []byte(signingInput))
    token := signingInput + "." + enc.EncodeToString(signature)

    s.cached = token
    s.expiresAt = time.Unix(exp, 0)
    return token, nil
}

func (s *JWTSigner) Invalidate() {
    s.mu.Lock()
    s.cached = ""
    s.expiresAt = time.Time{}
    s.mu.Unlock()
}
```

不要在测试失败输出中打印完整 token。测试只验证三段结构、Header/Payload 字段和本地公钥验签。

---

## 5. HTTP 请求

### 5.1 实时天气

```http
GET https://<API_HOST>/v7/weather/now?location=<LOCATION>&lang=zh
Authorization: Bearer <JWT>
Accept: application/json
```

用于：

- 当前温度；
- 当前天气图标和文字；
- 观测时间；
- 当前降水量作为辅助信息。

读取字段：

```text
code
updateTime
now.obsTime
now.temp
now.icon
now.text
now.precip
refer.sources
refer.license
```

实况数据可能相对物理世界延迟 5～20 分钟，UI 必须以 `obsTime` 表示数据时间，不能把请求完成时间伪装成观测时间。

### 5.2 逐小时预报

根据需要覆盖的最远展示/出门目标动态选择：

```text
最远目标距离当前时间 <= 24 小时：/v7/weather/24h
最远目标距离当前时间 > 24 小时：/v7/weather/72h
```

启用工作日过滤时，周五晚到周末可能需要 `72h` 才能覆盖下一个工作日；禁止错误地假设 `24h` 永远足够。

```http
GET https://<API_HOST>/v7/weather/24h?location=<LOCATION>&lang=zh
GET https://<API_HOST>/v7/weather/72h?location=<LOCATION>&lang=zh
Authorization: Bearer <JWT>
Accept: application/json
```

读取字段：

```text
code
updateTime
hourly[].fxTime
hourly[].temp
hourly[].icon
hourly[].text
hourly[].pop
hourly[].precip
refer.sources
refer.license
```

### 5.3 Go HTTP 客户端要求

- 使用共享 `http.Client`；
- 总超时默认 8 秒；
- 使用 `url.Values` 生成查询参数；
- 限制响应体最大 1 MiB；
- 检查 HTTP Status；
- 对 API v1 风格响应继续检查 JSON `code == "200"`；
- 设置可识别的 `User-Agent`，例如 `AgentBeacon/0.1`；
- 不在 URL 查询参数中传 JWT；
- 不记录 Authorization Header；
- 接受 gzip，Go 默认 Transport 可自动处理；
- 仅允许 HTTPS；
- `api_host` 只允许合法主机名，禁止包含用户信息、路径和查询参数。

示意：

```go
func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
    token, err := c.signer.Token(time.Now())
    if err != nil {
        return nil, err
    }

    u := url.URL{
        Scheme:   "https",
        Host:     c.apiHost,
        Path:     path,
        RawQuery: query.Encode(),
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Accept", "application/json")
    req.Header.Set("User-Agent", "AgentBeacon/0.1")
    return req, nil
}
```

---

## 6. 响应模型

```go
type Refer struct {
    Sources []string `json:"sources"`
    License []string `json:"license"`
}

type NowResponse struct {
    Code       string `json:"code"`
    UpdateTime string `json:"updateTime"`
    Now        struct {
        ObsTime string `json:"obsTime"`
        Temp    string `json:"temp"`
        Icon    string `json:"icon"`
        Text    string `json:"text"`
        Precip  string `json:"precip"`
    } `json:"now"`
    Refer Refer `json:"refer"`
}

type HourlyResponse struct {
    Code       string `json:"code"`
    UpdateTime string `json:"updateTime"`
    Hourly     []struct {
        FxTime string `json:"fxTime"`
        Temp   string `json:"temp"`
        Icon   string `json:"icon"`
        Text   string `json:"text"`
        Pop    string `json:"pop"`
        Precip string `json:"precip"`
    } `json:"hourly"`
    Refer Refer `json:"refer"`
}
```

所有数字字段在 QWeather JSON 中按字符串返回：

- 必须显式解析；
- 解析失败标记该字段无效；
- 单个字段无效不得导致整个 Provider panic；
- `pop` 缺失或空字符串不等于 0；
- `precip` 缺失或空字符串不等于 0。

---

## 7. 时间和出门窗口

统一使用配置时区 `Asia/Shanghai`，不能使用 Mac 当前系统时区进行隐式推断。

### 7.1 页面目标日期

```text
当前本地时间 < 当日 19:00：选择当日 12:00 和 19:00
当前本地时间 >= 当日 19:00：选择下一个 active weekday 的 12:00 和 19:00
非 active weekday：选择下一个 active weekday
```

这样夜间和周末不会继续显示已经过期的午饭/下班预报。
设备页面只显示“午饭”和“下班”标签，不展示目标的具体时间；12:00 和 19:00 仍保留在页面输出模型中用于取数和判断。

### 7.2 下一出门窗口

```text
当前时间 < 今日 12:00：下一窗口 = 今日午饭
今日 12:00 <= 当前时间 < 今日 19:00：下一窗口 = 今日下班
当前时间 >= 今日 19:00：下一窗口 = 下一个 active weekday 午饭
非 active weekday：下一窗口 = 下一个 active weekday 午饭
```

### 7.3 小时记录选择

- 解析 `fxTime` 为 RFC3339；
- 转换到配置时区；
- 优先选择与 12:00 或 19:00 完全相等的记录；
- 如果上游缺少整点记录，允许选择距离目标不超过 59 分钟的最近记录；
- 超出 59 分钟视为目标预报不可用；
- 不跨日期错误匹配；
- 响应数组顺序不可信，必须按时间排序。

---

## 8. 页面输出模型

Weather Provider 向桥接状态层输出：

```json
{
  "current": {
    "temperature_c": 29,
    "icon": "101",
    "text": "多云",
    "observed_at": "2026-07-14T14:10:00+08:00",
    "stale": false
  },
  "lunch": {
    "date": "2026-07-14",
    "time": "12:00",
    "temperature_c": 31,
    "icon": "300",
    "text": "阵雨",
    "pop": 55,
    "precip_mm": 0.4,
    "available": true
  },
  "leave": {
    "date": "2026-07-14",
    "time": "19:00",
    "temperature_c": 27,
    "icon": "104",
    "text": "阴",
    "pop": 20,
    "precip_mm": 0,
    "available": true
  },
  "next_outing": {
    "target": "lunch",
    "target_at": "2026-07-14T12:00:00+08:00",
    "umbrella": "required",
    "confidence": "high",
    "reason": "12:00 前后有阵雨"
  },
  "source": {
    "label": "QWeather",
    "update_time": "2026-07-14T14:00:00+08:00"
  }
}
```

`umbrella` 枚举：

```text
required
not_required
unknown
```

数据不足或过期时必须输出 `unknown`，禁止乐观地输出 `not_required`。

---

## 9. 带伞判断

判断窗口：

```text
target - 60 分钟 ... target + 60 分钟
```

收集窗口内所有逐小时记录。

### 9.1 直接判定 required

任一记录满足以下条件即为 `required`：

1. `precip > 0`；
2. `pop >= 40`；
3. 图标属于湿性降水：

```text
300-318
350-351
399
404-406
456
```

其中包含阵雨、雷阵雨、冰雹伴随雷雨、各种雨、冻雨和雨夹雪。

天气图标可能新增或调整，因此还要保留文字兜底：

```text
雨
雷
冰雹
雨夹雪
冻雨
```

不能只依赖硬编码图标表。

### 9.2 confidence

```text
high:
  precip > 0，或图标/文字明确为降水

medium:
  仅 pop >= threshold

unknown:
  没有目标窗口数据、关键字段全部缺失，或 hourly 数据 stale
```

### 9.3 reason

生成适合小屏的短句，优先级：

```text
“19:00 前后有中雨”
“12:00 降水概率 60%”
“下班窗口可能有雨”
“天气数据暂不可用”
```

不要显示内部规则、图标码或原始 JSON。

---

## 10. 通知

### 10.1 需要带伞

事件：

```text
weather.umbrella_required
```

表现：

```text
颜色：红色
紧急度：attention
优先级：72
标题：午饭记得带伞 / 下班记得带伞
详情：12:00 前后有阵雨
```

触发：

```text
上一状态为 not_required 或 unknown
当前状态首次变为 required
```

去重键：

```text
weather:umbrella:<YYYY-MM-DD>:<lunch|leave>
```

TTL：目标窗口结束后失效。

### 10.2 临近再次提醒

目标前 30 分钟仍为 `required` 时允许再次提醒一次：

```text
weather:umbrella-reminder:<YYYY-MM-DD>:<lunch|leave>
```

必须以最新一次成功预报为准，不得仅使用早先缓存。

### 10.3 数据过期

事件：

```text
weather.data_stale
```

表现：黄色，不使用红色。

- 实况超过 45 分钟未成功更新；
- 逐小时预报超过 90 分钟未成功更新；
- 页面显示最后成功数据和 `STALE`；
- 不清零、不伪造晴天、不输出“不用带伞”。

其他队列、打断和 ACK 规则见 `notify.md`。

---

## 11. 刷新和缓存

参考和风天气官方缓存建议：

- 实时天气：10～30 分钟；
- 逐小时预报：30～60 分钟。

本项目默认：

```text
/v7/weather/now：每 10 分钟
/v7/weather/24h：每 30 分钟
```

额外刷新：

```text
Provider 启动时
每天 CST 11:55、18:20（同时强制刷新实况和逐小时预报）
日期/目标出门窗口变化时
目标前 60 分钟
目标前 30 分钟
用户执行 weather refresh 时
```

请求合并：

- 同一路径同一 location 同时只允许一个 in-flight 请求；
- 多个调用者等待同一个结果；
- 不因多个 ESP32 连接而重复请求 QWeather；
- Mac 服务是唯一 QWeather 客户端。

持久化最后成功结果：

```text
provider
endpoint
location
fetched_at
update_time
payload_json
```

重启后可立即显示缓存，但必须按 `fetched_at` 和上游时间标记 fresh/stale。

提供：

```bash
agent-beacon-bridge weather cache clear
```

---

## 12. 错误和重试

### 12.1 401 Unauthorized

可能原因：JWT、凭据 ID、项目 ID、私钥、系统时间错误。

处理：

1. 立即使缓存 JWT 失效；
2. 重新生成 JWT；
3. 只重试一次；
4. 仍失败则停止快速重试，Provider 进入 degraded；
5. doctor 输出检查项，但不打印 token 和私钥。

### 12.2 403

包括：余额不足、欠费、安全限制、错误 Host、无权限等。

处理：

- 不进行自动高频重试；
- 保留最后成功数据；
- 记录 HTTP 状态及非敏感错误类型；
- 提示检查控制台；
- `INVALID HOST` 必须明确指向 `api_host` 配置。

### 12.3 429

- 遵循 `Retry-After`（存在时）；
- 否则指数退避；
- 不允许固定短周期持续请求；
- 记录被限流计数；
- 不因限流清空页面。

### 12.4 5xx 和网络错误

退避：

```text
1m, 2m, 4m, 8m, 15m，上限 15m，加入抖动
```

成功一次后恢复正常刷新周期。

### 12.5 解析错误

- 保存有限长度的响应诊断摘要，不保存 Authorization；
- 不把整个 HTML 错误页写入日志；
- 单个字段错误不 panic；
- 整体结构错误则拒绝替换 last-good cache。

---

## 13. 来源标识

天气页面必须清晰显示：

```text
QWeather
```

Mac 端状态页、README 或诊断页面提供指向和风天气官网的可点击来源链接。

同时保存响应中的：

```text
refer.sources
refer.license
```

不得删除或伪造来源信息。后续若接入天气预警，必须按官方要求完整展示预警响应中的所有 `refer.sources`。

---

## 14. CLI 与诊断

实现：

```bash
agent-beacon-bridge weather doctor
agent-beacon-bridge weather fetch-now
agent-beacon-bridge weather fetch-hourly
agent-beacon-bridge weather snapshot
agent-beacon-bridge weather refresh
agent-beacon-bridge weather cache clear
```

### 14.1 doctor 检查

- `api_host` 存在且为合法 qweatherapi.com 主机；
- `project_id` 存在；
- `credential_id` 存在；
- 私钥文件存在；
- 私钥权限不宽于 0600；
- PEM 是 PKCS#8 `PRIVATE KEY`；
- 私钥算法为 Ed25519；
- 可生成 JWT；
- JWT Header 中 `kid` 正确；
- JWT Payload 中 `sub/iat/exp` 正确；
- 系统时间合理；
- API Host DNS 可解析；
- HTTPS 可建立；
- `/v7/weather/now` 可成功请求；
- `/v7/weather/24h` 可成功请求；
- 需要跨越 24 小时时可请求 `/v7/weather/72h`；
- 可找到目标 12:00、19:00 数据；
- 来源字段已保存。

默认输出不得包含：

- 私钥内容；
- 完整 JWT；
- Authorization Header。

---

## 15. 单元测试

必须覆盖：

### JWT

- 正确 PKCS#8 Ed25519 私钥；
- 非 Ed25519 私钥拒绝；
- 无效 PEM 拒绝；
- Header 为 `alg=EdDSA`、`kid=<credential>`；
- Payload 为 `sub=<project>`；
- `iat=now-30s`；
- `exp` 在允许范围内；
- 使用公钥验证签名；
- token 缓存和提前刷新；
- 并发调用只刷新一次。

### API

- now 正常响应；
- 24h 正常响应；
- 72h 正常响应及动态 horizon 选择；
- HTTP 200 但 `code != 200`；
- 401 重签只重试一次；
- 403 不高频重试；
- 429 退避；
- 5xx 保留 last-good；
- 超大响应被拒绝；
- 字符串数字解析失败。

### 时间

- 12:00 前选午饭；
- 12:00～19:00 选下班；
- 19:00 后选下一工作日午饭；
- 周五 19:00 后选周一；
- 非工作日；
- 跨日期；
- 响应乱序；
- 缺失目标小时。

### 带伞

- `precip > 0`；
- `pop >= 40`；
- 300～318 雨图标；
- 夜间 350/351；
- 399；
- 雨夹雪；
- 文字兜底；
- 数据 stale 输出 unknown；
- false/unknown -> true 触发通知；
- 同一窗口去重；
- 目标前 30 分钟最多重播一次。

---

## 16. 集成验收

完成条件：

- [ ] 使用 `~/.weather` 私钥动态生成 JWT；
- [ ] 不再要求手工生成或保存 `QWEATHER_JWT`；
- [ ] 凭据 ID 正确写入 Header `kid`；
- [ ] 项目 ID 正确写入 Payload `sub`；
- [ ] 使用用户独立 API Host；
- [ ] 实况请求成功；
- [ ] 24 小时预报请求成功；
- [ ] 跨周末等场景能自动选择 72 小时预报；
- [ ] 当前、午饭和下班天气在同一横排显示；
- [ ] 午饭和下班标签不显示 12:00、19:00；
- [ ] 无需带伞时推荐区不显示背景色；
- [ ] 需要带伞时推荐区使用放大的红色提醒；
- [ ] 下一出门窗口选择正确；
- [ ] 带伞判断可解释；
- [ ] 首次需要带伞显示红色全屏通知；
- [ ] 30 分钟前最多再提醒一次；
- [ ] API 故障时保留最后成功数据；
- [ ] stale 时不错误显示“不用带伞”；
- [ ] 页面可见显示 `QWeather`；
- [ ] 私钥、JWT 和 Authorization 不出现在日志或设备消息中。

---

## 17. Codex 第一轮实施顺序

1. 从配置读取 API Host、项目 ID、凭据 ID和私钥路径；
2. 加载并验证 Ed25519 PKCS#8 私钥；
3. 实现标准库 JWTSigner；
4. 为 JWTSigner 编写公钥验签测试；
5. 实现 `weather doctor`；
6. 实现 `/v7/weather/now`；
7. 实现 `/v7/weather/24h`、`/v7/weather/72h` 及动态 horizon 选择；
8. 实现响应转换和 last-good cache；
9. 实现 12:00/19:00 选择；
10. 实现下一出门窗口；
11. 实现带伞判断；
12. 接入 `notify.md`；
13. 接入 `ui.md`；
14. 做网络失败、401、403、429 和 stale 回归测试。

第一轮不实现：

- 分钟降水；
- 空气质量；
- 天气指数；
- 日预报；
- 天气预警 API；
- ESP32 直连 QWeather；
- API KEY 鉴权。
