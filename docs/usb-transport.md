# USB 主通道

Agent Beacon 默认把开发板 Type-C 口上的 ESP32-S3 USB-Serial/JTAG CDC 用作业务主通道，
Wi-Fi WebSocket 作为自动兜底。无需修改硬件或更换接口板。

## 运行方式

Bridge 在 macOS 上扫描 `/dev/cu.usbmodem*`，打开串口并发送 server `hello`。设备收到一帧
合法且校验通过的 USB 消息后切换到 USB，同时暂停 Wi-Fi WebSocket；USB 每 2 秒收到一次
Bridge heartbeat。拔线、串口关闭或连续 12 秒没有合法帧时，设备恢复 WebSocket，并通过
正常的 hello/snapshot 流程重新同步完整状态。

Bridge 默认配置为：

```yaml
transports:
  usb:
    enabled: true
    port: "/dev/cu.usbmodem*"
    scan_interval: 1s
```

只有一块设备时保持自动发现即可；需要固定端口时可把 `port` 改成完整设备路径。USB device
hello 携带同一份 Bridge Token，Bridge 验证 Token 后才注册设备并下发 snapshot。
排查兜底链路时，可用 `serve --disable-usb` 仅对本次进程关闭 USB，不修改 YAML。

## 帧格式

JSON v2 envelope 外包一层二进制帧。原始帧使用网络字节序：

| 偏移 | 长度 | 内容 |
|---:|---:|---|
| 0 | 2 | ASCII `AB` |
| 2 | 1 | USB 帧版本，当前为 `1` |
| 3 | 1 | flags，当前必须为 `0` |
| 4 | 4 | JSON payload 字节数，最大 65536 |
| 8 | N | UTF-8 JSON v2 消息 |
| 8+N | 4 | CRC32/IEEE，覆盖 header 与 payload |

原始帧整体经过 COBS 编码，最后追加单字节 `0x00` 分隔符。接收端遇到超长帧、错误 magic、
未知版本、长度不符或 CRC 错误时丢弃当前帧，并在下一个 `0x00` 自动重新同步。

## 日志与维护

业务帧独占 USB-Serial/JTAG CDC；应用日志只输出到 UART0，避免日志字节混入 JSON 帧。
因此 `make firmware-monitor PORT=/dev/cu.usbmodem...` 不再用于观察应用日志，需要日志时应接
UART0 调试器。Type-C 口仍可进入 ROM download mode 并烧录。

常驻 Bridge 会持有串口。仓库内的检测、烧录、备份和恢复脚本会在操作真实字符设备时
自动暂停已加载的 Bridge LaunchAgent，并在退出时恢复；可设置
`AGENT_BEACON_MANAGE_BRIDGE_SERVICE=0` 禁用这个行为。手工调用 `esptool` 时则需要自行执行：

```bash
launchctl bootout "gui/$(id -u)/com.stepatero.agentbeacon"
make firmware-flash PORT=/dev/cu.usbmodemXXXX
launchctl bootstrap "gui/$(id -u)" \
  "$HOME/Library/LaunchAgents/com.stepatero.agentbeacon.plist"
```

USB 只取代设备业务链路；Bridge 的本机 HTTP 接口、Provider、通知去重和 revision store
保持不变。
