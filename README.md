# Agent Beacon

Agent Beacon 将 Waveshare ESP32-S3-LCD-1.47B 用作本地桌面状态终端。当前第一轮
实现 Codex 全局实时 Token 速度与两个 Home 的一周额度、Herdr Agent 状态、
午饭/下班天气；状态变化通过蓝、黄、红、绿全屏通知展示。Agents 与天气页始终
轮播；只有 Herdr 存在 `working` 的 Codex session 时，速度仪表盘才会立即出现并
以 15 秒时长加入轮播。

通知、界面和天气的实现分别以 `docs/notify.md`、`docs/ui.md` 和
`docs/weather.md` 为准；本文负责用户操作说明。日常运行默认关闭 Mock，
Codex 配额通过各自 `CODEX_HOME` 的 app-server 读取；全局 Token 速度消费 patched
Codex daemon 的本地聚合状态文件；0-0 余额从真实 API 读取，Agents 使用 Herdr
socket，天气使用 QWeather。

## 环境

- macOS arm64
- Go 1.26.5
- ESP-IDF v5.5.4
- 使用数据线连接的 ESP32-S3-LCD-1.47B

默认 ESP-IDF 激活脚本为
`~/.espressif/tools/activate_idf_v5.5.4.sh`，可通过 `IDF_ACTIVATE` 覆盖。

## 首次准备

```bash
make doctor
make detect-device
make backup-factory PORT=/dev/cu.usbmodemXXXX
./scripts/configure-network.sh \
  --ssid YOUR_2G_SSID \
  --server YOUR_MAC_LAN_IP \
  --device-id agent-beacon-XXXX
```

配网脚本从 macOS Keychain 读取 Wi-Fi 密码，生成共享 bridge token，并写入两个
不提交 Git 的 0600 文件：`firmware/config.local.h` 和
`macos/configs/token.local`。脚本不会打印密码或 token。

烧录前必须已经保存并校验该开发板的完整 16 MB 出厂备份。

## 本地字体准备（必须自行授权）

本仓库及其 Git 历史**不包含、也不提供下载** Milan 字体。项目的 MIT 许可
**不适用于字体文件**，也不会授予任何字体的使用、修改、嵌入或再分发权利。
构建固件前，用户必须从自己有权使用的来源取得下列两个文件：

```text
MiLanPro-Medium-400.ttf
MiLanPro-SemiBold-540.ttf
```

请先确认字体许可至少允许本地使用和嵌入设备。若要向他人分发包含字体的已编译
固件，还必须另外确认许可允许字体随固件再分发；没有相应授权时，**不要发布
`firmware/build/` 中的固件文件**。

将两个文件放在同一目录，然后把该目录的绝对路径传给安装脚本：

```bash
./scripts/install-local-fonts.sh \
  --source-dir /absolute/path/to/your/milan-fonts
```

脚本只校验字体容器签名并复制文件，不会下载字体，也不能替用户判断授权范围。
目标目录为 `firmware/components/beacon_fonts/font_assets/`；其中的
`MiLanPro-*.ttf` 已被 `.gitignore` 明确排除，不会进入提交。固件界面的固定
文案为简体中文，动态 workspace、tab 和 Agent 名称保留上游原文。

## 开发与运行

```bash
make test
make firmware-build
make bridge-build

# 临时前台运行（两个终端）
make token-rate-run
make bridge-run

# 终端 2
make firmware-flash PORT=/dev/cu.usbmodemXXXX
make firmware-monitor PORT=/dev/cu.usbmodemXXXX

# 需要演示 fixture 时，临时把 providers.mock.enabled 改为 true
# make demo-events
```

`make bridge-run` 默认连接 `~/.config/herdr/herdr.sock`。Herdr Agents 按
`blocked > done > working > idle > unknown` 排序，只在 LCD 显示前四项；同一
workspace 有多个 tab 时显示为 `workspace · tab`。命名 session 或自定义 socket
可在 `macos/configs/config.example.yaml` 的 `providers.herdr` 中配置。
Bridge 还会下发 `agents.codex_active`：只要任意 Codex session 为 `working`，设备
就立即切到速度仪表盘；没有活跃 Codex session 或 Herdr 断连时，该页不参与轮播。

## Codex Token 速度、配额与 0-0 余额

`providers.codex.homes` 为每套帐号分别设置 `CODEX_HOME`。内置
`codex-adapter` 子命令通过当前 Codex CLI 的 `account/rateLimits/read` 读取
7 天窗口和重置卡；它只输出周额度，不下发或保留 5 小时窗口。

Token 速度依赖 `rust-v0.144.4` patched Codex 的 `codex-token-rate-daemon`。指标固定为
所有连接同一 socket 的本机 patched Codex 进程之
`visible_output_tokens_per_second`：只统计用户可见助手文本，使用 daemon 已聚合的
2 秒窗口 EMA，不包含输入、reasoning、工具参数或工具输出。daemon 状态文件必须为
0600；Bridge 每 200ms 读取一次，只在可见值变化时下发，2 秒没有新状态即显示过期。

前台调试时，先运行 `make token-rate-run`，再从另一个终端启动 patched Codex：

```bash
export CODEX_TOKEN_RATE_SOCKET="$HOME/Library/Application Support/AgentBeacon/codex-token-rate.sock"
~/.local/bin/codex-patched
```

0-0 API Key 存在 macOS 登录 Keychain，不写 YAML 或 plist：

```bash
cd macos
./bin/agent-beacon-bridge secret set zero-api-key --from-env ZERO_API_KEY
```

## LaunchAgent 常驻与自启

确认 `macos/configs/config.local.yaml`、`macos/configs/token.local` 和
`ZERO_API_KEY` 已就绪后安装：

```bash
make bridge-service-install
make bridge-service-status
make token-rate-service-status
```

当 `providers.codex.token_rate.enabled: true` 时，安装命令还会从
`~/.local/bin/codex-token-rate-daemon` 安装并启动 companion LaunchAgent，并向当前
GUI bootstrap domain 设置 `CODEX_TOKEN_RATE_SOCKET`。已经运行的 patched Codex 不会
追溯继承新环境变量，需重启；终端进程仍可按上节显式 `export`。

安装命令会把二进制、配置和设备 token 安装到
`~/Library/Application Support/AgentBeacon/`，生成并加载
`com.stepatero.agentbeacon.plist` 和 `com.stepatero.agentbeacon.tokenrate.plist`，先等待
daemon 状态文件出现，再等待 Bridge `/readyz` 通过。服务日志位于
`~/Library/Logs/AgentBeacon/`。

```bash
make bridge-service-restart
make bridge-service-uninstall  # 默认保留配置、token、日志和 Keychain secret
```

## 天气与带伞判断

复制 `macos/configs/config.example.yaml` 为不提交 Git 的本地配置，填写
`providers.weather` 中的帐号独立 API Host、项目 ID、凭据 ID、位置，以及
`~/.weather` 下 Ed25519 私钥的实际路径，再将 `enabled` 改为 `true`。API Host
必须来自和风天气控制台并以 `.qweatherapi.com` 结尾，不能使用旧公共 Host。

```bash
cd macos
go run ./cmd/agent-beacon-bridge weather doctor --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather fetch-now --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather fetch-hourly --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather fetch-radiation --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather snapshot --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather refresh --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather cache clear --config configs/config.local.yaml
```

Bridge 会从私钥动态生成 Ed25519 JWT，不需要手工生成或保存静态 JWT。配置、
鉴权、缓存、12:00/19:00 选择和带伞规则详见
[`docs/weather.md`](./docs/weather.md)。
实况和预报由 [QWeather](https://www.qweather.com/) 提供；遮阳判断使用
[Open-Meteo Satellite Radiation API](https://open-meteo.com/en/docs/satellite-radiation-api)，
在 CST 11:57（午饭）与 18:28（下班）各获取一次原生时间分辨率数据。

仅在 `providers.mock.enabled: true` 时可单独发送 fixture：

```bash
AGENT_BEACON_TOKEN="$(cat macos/configs/token.local)" \
  ./macos/bin/agent-beacon-bridge emit \
  --fixture herdr-blocked
```

可用 fixture：

```text
codex-normal              codex-one-home-stale   codex-critical
relay-14-16               relay-invalid           herdr-all-statuses
herdr-blocked             herdr-done              weather-no-umbrella
weather-lunch-umbrella    weather-leave-umbrella  weather-stale
bridge-offline
```

BOOT：短按下一页，双击重播最近未过期通知，长按 2 秒进入或退出诊断页。诊断页
会按 1 秒刷新 SoC 芯片温度和双核综合 CPU 占用率。长按 5 秒的配网手势已识别，
SoftAP 配网页属于正式 MVP 后续工作。

硬件记录与验收证据见 `docs/bringup.md` 和 `docs/test-report.md`。

## 许可

除 [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md) 中列出的第三方内容外，
Agent Beacon 的原创代码和文档采用 [`MIT License`](./LICENSE)。Milan 字体不属于
本项目、不会随仓库分发，也不受本项目 MIT 许可覆盖。
