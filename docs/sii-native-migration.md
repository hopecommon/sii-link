# SII 原生二进制迁移指南

## 结论

SII 场景可以用原生 Go 二进制替代 aTrust Docker。当前实现已覆盖动态 CAS RSA、macOS Keychain、macOS 休眠唤醒事件、健康检查失败退出和 launchd 重启契约。正式配置保持原端口 `1080/8888`；迁移期间停止而不删除旧容器，以便快速回退。

SII 同一账号同时在线时会使上一条会话的 SID 失效。因此“两套同时运行、同时压流量”不可行，正确的灰度方式是保留两套配置，但分时 A/B：停止旧容器、启动原生 canary、验证后停止 canary 并恢复旧容器。

## 安全边界

- CAS 页面、表单、脚本和回调必须使用 HTTPS；表单与动态 `login.js` 必须来自 `cas.sii.edu.cn`。
- SII 模式同时开启 aTrust 网关的系统信任链校验，不沿用上游默认的跳过证书校验行为。
- RSA 公钥每次从当前登录脚本提取，不把易变公钥固化到二进制。
- 凭据默认从 macOS Keychain 的 `atrust.username`、`atrust.password` 读取，不写入 TOML 或日志。
- 如需 Linux/launch daemon 文件凭据，必须同时配置 `sii_username_file` 与 `sii_password_file`，且文件权限不得宽于 `0600`。
- aTrust `client_data_file` 含登录 cookie，保存时强制设为 `0600`。
- SII 模式严格限定 `vpn.sii.edu.cn:443`、`auth/cas` 和 `cas.sii.edu.cn`，避免误向其他站点提交凭据。

## 休眠、断网与重启模型

原生 aTrust L3 隧道能在流量到来时重连，但旧 keep-alive 只记录错误，不会让服务管理器介入。SII 模式新增以下闭环：

1. macOS 通过 IOKit 订阅系统电源事件；收到 `kIOMessageSystemHasPoweredOn` 后立即触发检查，不用靠高频轮询猜测唤醒时间。休眠许可事件会按 IOKit 契约确认，不阻塞系统睡眠。
2. 空闲时默认每 300 秒通过 VPN 请求 `keep_alive_url`，所以日常唤醒响应更快的同时，定时器唤醒次数降为旧 60 秒方案的五分之一。
3. 失败检查默认超时 3 秒，并以 2 秒间隔重试；任一次成功即清零失败计数。默认连续失败 3 次后，进程清理代理监听器、解析器和 VPN 客户端，并以状态码 1 退出。一次唤醒触发的最坏检测窗口约 13 秒，常见的立即拒绝或重连会更快。
4. 用户级 launchd 根据非零退出重启进程；`ThrottleInterval=30` 防止密码失效、CAS 限流或持续断网时高速崩溃循环。
5. 启动时先验证 `client_data_file` 中的既有 cookie；有效时直接复用且完全不访问 CAS，无效时才从 Keychain/私密文件读取凭据重新执行 CAS。Device ID 始终由 `client_data_file` 保持稳定。因此“每次重启都重新走 CAS”不是当前实现的行为。
6. SII 启动阶段每个候选节点只做一次可达性探测，成功建隧道后再由后台按原有三次采样策略周期更新最佳节点，避免让已知不可达节点的重复超时阻塞服务就绪。

Canary 与正式本地配置都启用 `skip_tcp_tunnel_wait = true`，让应用首包与隧道建链重叠，并设置 `tcp_tunnel_pool_size = 3`。后者按中继惰性建立、最多保留三条 TLS transport；只有收到服务端干净关闭帧的短隧道才会回池，错误连接会立即丢弃。启用 zero-RTT 后，建链错误可能延迟到首次读写才返回。

在本机存在 FlClash/mihomo TUN 时必须使用 `auto_detect_interface = true` 或显式 `bind_interface = "en0"`。否则 10.x 内部节点可能被本地 TUN 误判为可用 underlay，最终在建隧道时返回 EOF。SII 配置会对此 fail-fast。

## Canary 配置

1. 复制 [`configs/sii-canary.toml.example`](../configs/sii-canary.toml.example)，替换绝对路径和 macOS 账号。
2. 确认 Keychain 项存在：

   ```bash
   security find-generic-password -a "$USER" -s atrust.username -w >/dev/null
   security find-generic-password -a "$USER" -s atrust.password -w >/dev/null
   ```

3. 保留旧 Docker 文件和容器；功能测试时短暂停止它，避免 SII 单会话互踢：

   ```bash
   docker stop atrust
   ./sii-link -config /ABSOLUTE/PATH/TO/sii-canary.toml
   ```

4. 从另一终端验证：

   ```bash
   curl --socks5-hostname 127.0.0.1:1082 -L https://qz.sii.edu.cn/ -o /dev/null
   curl -x http://127.0.0.1:8890 -L https://qz.sii.edu.cn/ -o /dev/null
   ```

5. canary 结束后恢复旧方案：

   ```bash
   docker start atrust
   ```

旧 `1080/8888`、`~/.ssh/config` 与启动项在全面切换确认前都不修改。实际切换时再把原生监听端口改为 `1080/8888`，并把 SSH `ProxyCommand` 改为：

```sshconfig
ProxyCommand nc -X 5 -x 127.0.0.1:1080 %h %p
```

## 正式本地配置与 launchd

正式配置模板是 [`configs/sii-local.toml.example`](../configs/sii-local.toml.example)，LaunchAgent 模板是 [`deploy/dev.hopecommon.sii-link.plist.example`](../deploy/dev.hopecommon.sii-link.plist.example)。所有路径必须使用绝对路径。`sii_cas_proxy` 只在直连 CAS 不可用时添加，避免把开机认证不必要地绑定到另一个本地代理的启动顺序。

安装或更新用户级服务：

```bash
if launchctl print gui/$(id -u)/dev.hopecommon.sii-link >/dev/null 2>&1; then
  launchctl bootout gui/$(id -u) "$HOME/Library/LaunchAgents/dev.hopecommon.sii-link.plist"
fi
launchctl bootstrap gui/$(id -u) "$HOME/Library/LaunchAgents/dev.hopecommon.sii-link.plist"
launchctl enable gui/$(id -u)/dev.hopecommon.sii-link
launchctl kickstart -k gui/$(id -u)/dev.hopecommon.sii-link
launchctl print gui/$(id -u)/dev.hopecommon.sii-link
```

它在用户登录图形会话后由 `RunAtLoad` 启动，不是在 FileVault 登录前启动。首次后台读取 Keychain 可能要求当前用户确认；重启后要等用户解锁 Keychain。服务正常运行时，重启进程优先复用 cookie；只有 cookie 已失效才重新 CAS。

SSH 应直接连接本地 SOCKS，不再进入容器：

```sshconfig
ProxyCommand /usr/bin/nc -X 5 -x 127.0.0.1:1080 %h %p
```

观察自然休眠/唤醒后的日志：

```bash
grep -E 'wake event|KeepAlive|Runtime failure|Already logged in|Starting login' \
  "$HOME/Library/Logs/sii-link/sii-link.log"
```

正常证据应包含唤醒事件后的立即检查；若旧隧道失效，则随后出现健康失败、非零退出、launchd 拉起，以及 `Already logged in` 或仅在 cookie 失效时出现的 `Starting login`。

## 回退

旧容器和 `~/atrust` 数据在 canary 期间不删除。回退时先卸载本地服务，再启动容器，并把 SSH `ProxyCommand` 恢复成旧的 `docker exec` 版本：

```bash
launchctl bootout gui/$(id -u) "$HOME/Library/LaunchAgents/dev.hopecommon.sii-link.plist"
docker start atrust
```
