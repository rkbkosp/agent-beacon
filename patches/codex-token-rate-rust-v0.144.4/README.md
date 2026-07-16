# Codex Token-Rate Patch Series

这组 patch 把 Agent Beacon 所需的全局 completion-output Token 速度插桩移植到
OpenAI Codex `rust-v0.144.4`，并包含 daemon、测试、安装脚本和 AgentBeacon socket
复用逻辑。patch 存放在 agent-bacon 仓库中，避免更新或重建相邻 `../codex` 工作树时
丢失。

## 基线与来源

- 基线 tag：`rust-v0.144.4`
- 基线 commit：`8c68d4c87dc54d38861f5114e920c3de2efa5876`
- 导出分支：`patch/token-rate-rust-v0.144.4`
- 导出 HEAD：`18043f078a417ef3bacc12c016d8c7950f436668`
- 提交数量：3

`series` 保留原提交边界；第一份 patch 含完整插桩和 daemon，后两份分别处理 Herdr
进程身份与 AgentBeacon 单 socket 集成。第一份 patch 自带 `base-commit` 元数据。

## 推荐：一键跟随上游 release

在 agent-bacon 根目录运行：

```bash
./scripts/update-patched-codex.sh                 # 自动选择最新稳定 rust-vX.Y.Z
./scripts/update-patched-codex.sh rust-v0.145.0  # 或显式指定 tag
```

该脚本会拉取 `origin` tags、拒绝脏工作树和已有同名分支、从 tag 创建
`patch/token-rate-<tag>`，再调用本目录的 patch runner。若 `git am --3way` 冲突，脚本
保留冲突现场并退出，等待手工执行 `git add` 与 `git am --continue`。

## 手工应用、编译和安装

先在 Codex 仓库中基于新的上游版本创建干净分支：

```bash
cd /path/to/codex
git fetch origin
git switch -c patch/token-rate-rust-vNEXT <new-upstream-tag-or-commit>
```

再从 agent-bacon 运行：

```bash
patches/codex-token-rate-rust-v0.144.4/apply-build-install.sh \
  --repo ../codex \
  --refresh-agent-beacon
```

脚本会按顺序执行：

1. 要求目标 Codex worktree 干净；
2. 使用 `git am --3way` 应用 `series`；
3. 运行 `just fmt` 和两个 token-rate 针对性测试；
4. release 编译 `codex` 与 `codex-token-rate-daemon`；
5. 安装到 `~/.local/bin`；
6. 使用 `--refresh-agent-beacon` 时，重新安装并重启 Agent Beacon Bridge/daemon。

只验证 patch 能否应用：

```bash
patches/codex-token-rate-rust-v0.144.4/apply-build-install.sh \
  --repo /path/to/codex \
  --apply-only
```

若新版上游产生冲突，解决后执行 `git am --continue`，再用 `--build-only` 继续格式化、
测试、编译和安装。放弃本次移植则执行 `git am --abort`。

## 上游更新时重点检查

最可能冲突的文件是：

- `codex-rs/core/src/session/turn.rs`：流式文本、工具参数及工具生命周期插桩；
- `codex-rs/core/src/config/mod.rs`：socket 环境配置；
- `codex-rs/codex-api/src/sse/responses.rs`：响应事件透传；
- `codex-rs/Cargo.toml`、`Cargo.lock`：新增 daemon crate；
- `scripts/install-patched-codex.sh`：release payload 安装位置。

冲突解决后应确认以下不变量：

- 只统计 assistant text 与 streamed tool arguments；
- 不统计 input、hidden reasoning 或 tool output；
- 工具执行期间保持最后一次流速，结束后释放；
- 多个 patched Codex 进程聚合到同一个 Unix datagram socket；
- launcher payload 的进程 basename 仍为 `codex`，供 Herdr 识别；
- Agent Beacon 与 Codex 使用同一个 launchd socket，不再启动第二套聚合 daemon。
