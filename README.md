# SII Link

[English](README_en.md) | 中文

SII Link 是一个面向 SII aTrust 场景的轻量命令行客户端。它直接以 Go
二进制运行，提供本地 SOCKS5/HTTP 代理、无人值守 CAS、macOS Keychain 与
Linux kernel keyring 凭据读取、休眠唤醒后的事件驱动健康检查，以及有界日志轮转。它适合不想为
单个 VPN 长期运行完整 Docker 环境的个人用户。

> **非官方项目。** SII Link 由社区独立维护，与 SII、ZJU、深信服及其关联方
> 没有从属、授权或背书关系。请遵守所在机构的网络和账号政策；软件按原样
> 提供，使用风险由使用者承担。

SII Link 是 [ZJU Connect](https://github.com/Mythologyli/zju-connect) 的修改版，
后者基于 [EasierConnect](https://github.com/lyc8503/EasierConnect)。感谢原项目
作者和所有贡献者。派生关系、修改范围和第三方许可见 [NOTICE.md](NOTICE.md)。

## 设计目标

- 原生运行：无需 Docker daemon、特权容器或 TUN 权限即可提供本地代理。
- 无人值守：会话失效时从 macOS Keychain 重新完成 SII CAS，静态密码不进入
  TOML、状态文件或日志。
- Linux 原生：CAS 凭据可从内核 keyring 读取；授权后每台 Linux 独立承载
  VPN 流量，无需 Mac relay。
- 快速恢复：macOS 唤醒事件立即触发检查；正常空闲检查保持低频，避免 15 秒
  轮询带来的额外唤醒。
- 稳定延迟：可选的 aTrust 短隧道连接池与首包/建链重叠减少重复 TLS 建链。
- 有界存储：默认单日志 5 MiB、保留 3 份备份，日志总量约不超过 20 MiB。
- 保留通用能力：EasyConnect/aTrust、SOCKS5、HTTP、DNS、端口转发和 TUN
  等上游功能仍在；完整历史用法可参考上游文档和 `sii-link -h`。

## macOS 快速开始

从 [Releases](https://github.com/hopecommon/sii-link/releases) 下载对应架构，或运行：

```bash
curl -fsSL https://raw.githubusercontent.com/hopecommon/sii-link/main/scripts/install-macos.sh | sh
```

把 CAS 凭据存入当前用户 Keychain。下面命令不会把密码写入仓库；执行时会由
`security` 交互读取：

```bash
security add-generic-password -U -a "$USER" -s atrust.username -w
security add-generic-password -U -a "$USER" -s atrust.password -w
```

复制 [configs/sii-local.toml.example](configs/sii-local.toml.example) 为
`~/.config/sii-link/config.toml`，替换其中的绝对路径和 macOS 账号。推荐正式路径：

```text
~/.local/bin/sii-link
~/.config/sii-link/config.toml
~/.local/state/sii-link/client-data.json
~/Library/Logs/sii-link/sii-link.log
~/Library/LaunchAgents/dev.hopecommon.sii-link.plist
```

将 [LaunchAgent 模板](deploy/dev.hopecommon.sii-link.plist.example) 中的路径替换为
绝对路径后安装：

```bash
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/dev.hopecommon.sii-link.plist"
launchctl enable "gui/$(id -u)/dev.hopecommon.sii-link"
launchctl kickstart -k "gui/$(id -u)/dev.hopecommon.sii-link"
```

服务会在图形登录后自动启动。FileVault 登录前不会启动；若 CAS cookie 已过期，
首次后台访问 Keychain 可能需要用户确认。

## Linux 快速开始

复制 [Linux 配置示例](configs/sii-linux.toml.example)，设置实际状态与日志路径。
Linux 正式配置只声明 credential source，不包含用户名或密码：

```toml
sii_unattended_cas = true
sii_credential_source = "kernel-keyring"
```

凭据导入接口只从 stdin 接收一个 JSON 对象。生产使用应由不经 shell 展开的
授权 helper 从可信凭据源写入 stdin；不要把密码放进 argv、环境变量或命令历史：

```text
sii-link credentials put [--ttl 8h]
sii-link credentials status
sii-link credentials revoke
```

`credentials status` 在凭据可用时退出 0，不可用时仍输出状态 JSON 并退出 3，
便于 systemd `ExecCondition` 等调用方安全跳过启动。

默认没有 TTL：凭据持续到显式 revoke、内核清理或机器重启。`--ttl` 只用于
临时主机。kernel keyring 不落盘，但不能防护 root 或同一 UID 下的其他进程；
不要在 shared-root 账户中导入个人凭据。

## 端口和兼容性

正式 SII 配置保持旧方案端口不变：

| 接口 | 地址 |
| --- | --- |
| SOCKS5 | `127.0.0.1:1080` |
| HTTP | `127.0.0.1:8888` |

因此 FlClash 等下游配置无需改端口。SSH 可直接使用宿主 SOCKS：

```sshconfig
ProxyCommand /usr/bin/nc -X 5 -x 127.0.0.1:1080 %h %p
```

同一账号通常不应同时运行旧 Docker 会话和 SII Link；它们可能互踢，也会争用
本地端口。迁移时保留旧容器作为静态回滚项，但只启动其中一套。

## 配置要点

```toml
protocol = "atrust"
server_address = "vpn.sii.edu.cn"
server_port = 443
auth_type = "auth/cas"
login_domain = "cas.sii.edu.cn"

sii_unattended_cas = true
sii_credential_source = "keychain"
sii_keychain_account = "YOUR_MACOS_ACCOUNT"
client_data_file = "/ABSOLUTE/PATH/TO/client-data.json"

auto_detect_interface = true
keep_alive_url = "https://qz.sii.edu.cn/"
sii_health_failure_threshold = 3
sii_health_interval = 300
sii_health_retry_interval = 2
sii_health_timeout = 3

skip_tcp_tunnel_wait = true
tcp_tunnel_pool_size = 3
socks_bind = "127.0.0.1:1080"
http_bind = "127.0.0.1:8888"

log_file = "/ABSOLUTE/PATH/TO/sii-link.log"
log_max_size_mb = 5
log_max_backups = 3
```

日志轮转按大小执行：活动文件达到上限后成为 `.1`，旧备份依次后移，超出
`log_max_backups` 的最旧文件被删除。该边界不依赖外部 `logrotate` 或 launchd。

## 构建和验证

需要 Go 1.25.6 或兼容工具链：

```bash
go test ./...
go vet ./...
go build -trimpath -o sii-link .
```

正式 release 由 `scripts/build-release.sh` 生成。每个压缩包包含 AGPLv3、派生
声明以及按目标二进制依赖闭包收集的第三方许可；缺少许可文件时构建会失败。
当前依赖和许可类型见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 安全与隐私

- `client-data.json` 含会话 cookie，应保持 `0600`，不要提交或分享。
- Linux kernel keyring 凭据默认不设 TTL，但不会跨重启；需要时从可信机器重新
  授权。它不提供 root 或同 UID 进程之间的隔离。
- 不要在配置文件、issue 或日志中放入密码、CAS ticket、SID、Device ID 或
  Sign Key。
- SII 无人值守模式只允许把凭据发送到固定的 SII HTTPS 端点，并启用系统 TLS
  信任链验证。
- 发现安全问题请按 [SECURITY.md](SECURITY.md) 私下报告。

## 许可

本项目及修改部分整体按 [GNU AGPLv3](LICENSE) 分发。发布二进制时必须同时
提供对应版本的完整源代码和适用的第三方许可。版权归各自贡献者所有。
