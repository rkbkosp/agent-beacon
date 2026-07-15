# Agent Beacon

Agent Beacon 将 Waveshare ESP32-S3-LCD-1.47B 用作本地桌面状态终端。当前第一轮
只实现三个轮播页：Codex 两个 Home 的一周额度与重置卡、Herdr Agent 状态、
午饭/下班天气；状态变化通过蓝、黄、红、绿全屏通知展示。

通知、界面和天气的实现分别以 `docs/notify.md`、`docs/ui.md` 和
`docs/weather.md` 为准；本文负责用户操作说明。日常运行默认关闭 Mock，
Codex 通过各自 `CODEX_HOME` 的 Codex app-server 读取真实一周额度和重置卡，
0-0 余额从真实 API 读取，Agents 使用 Herdr socket，天气使用 QWeather。

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

# 临时前台运行
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

## Codex 与 0-0 余额

`providers.codex.homes` 为每套帐号分别设置 `CODEX_HOME`。内置
`codex-adapter` 子命令通过当前 Codex CLI 的 `account/rateLimits/read` 读取
7 天窗口和重置卡；它只输出周额度，不下发或保留 5 小时窗口。

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
```

安装命令会把二进制、配置和设备 token 安装到
`~/Library/Application Support/AgentBeacon/`，生成并加载
`~/Library/LaunchAgents/com.stepatero.agentbeacon.plist`，然后等待 `/readyz`
通过。服务日志位于 `~/Library/Logs/AgentBeacon/`。

```bash
make bridge-service-restart
make bridge-service-uninstall  # 默认保留配置、token、日志和 Keychain secret
```

## 和风天气

复制 `macos/configs/config.example.yaml` 为不提交 Git 的本地配置，填写
`providers.weather` 中的帐号独立 API Host、项目 ID、凭据 ID、位置，以及
`~/.weather` 下 Ed25519 私钥的实际路径，再将 `enabled` 改为 `true`。API Host
必须来自和风天气控制台并以 `.qweatherapi.com` 结尾，不能使用旧公共 Host。

```bash
cd macos
go run ./cmd/agent-beacon-bridge weather doctor --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather fetch-now --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather fetch-hourly --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather snapshot --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather refresh --config configs/config.local.yaml
go run ./cmd/agent-beacon-bridge weather cache clear --config configs/config.local.yaml
```

Bridge 会从私钥动态生成 Ed25519 JWT，不需要手工生成或保存静态 JWT。配置、
鉴权、缓存、12:00/19:00 选择和带伞规则详见
[`docs/weather.md`](./docs/weather.md)。
天气数据由 [QWeather](https://www.qweather.com/) 提供。

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
