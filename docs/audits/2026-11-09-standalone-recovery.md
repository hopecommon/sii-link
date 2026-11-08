# 独立登录的会话恢复验证

## 问题与证据范围

本机 SII Link 守护进程仍运行，但原生提供进程已退出，1080 与 8888
均拒绝连接；`doctor` 确认缓存失效，状态为 `server`、`peer=Mini`、
`renewal_available=false`，暂停原因是 repeated session loss。
本地日志从 2026-11-06 04:11 起持续拒绝新登录。03:02 登录成功后很快进入睡眠，
系统电源记录显示此后只有短暂后台唤醒。日志未确认其他设备替换登录，
不能据此宣称首次会话失效一定由睡眠或互踢引起。

已确认的恢复缺口：新登录消耗持久化预算，只有连续两分钟健康检查才补充；
短暂后台唤醒无法保证满足该条件，随后会话失效可能变成永久人工恢复。
用户已明确本机独立登录、Mini 暂时下线。

## 设计与验收约定

- 修正位于 `internal/gateway`，登录授权仍经过原生 worker 的 LoginGate 和
  控制器 dispatcher；shell、LaunchAgent 和 chezmoi 不再增加恢复状态机。
- `server --standalone` 清除保存的 peer/self，不联系旧对端，持久保留 Server。
- 没有 peer 的 Server 在新登录前持久保存五分钟冷却期限。登录完成并确认缓存后，
  普通会话失效可以在期限到达后再次预留一次登录，不依赖连续健康检查。
- 仍保留健康两分钟后补充预算的路径。冷却期间可恢复有效缓存；确认被替换、
  未知策略退出、结果不确定和未完成登录均不会因时间流逝而重新获得权限。
- 对照较小方案：仅执行 `sii server` 可以恢复当前连接，但保留原死锁路径；
  仅移除 Mini 也不能修复预算耗尽。采用一项命令选项和一个持久化期限，
  没有加入周期外部脚本，也没有自动重置所有暂停原因。

## 验证

修复前加入回归场景并运行：

```text
go test ./internal/gateway -run 'TestStandalone|TestCooldown' -count=1
FAIL TestStandaloneSelectionClearsSavedPeerWithoutContactingIt:
  standalone selection contacted the saved peer
FAIL TestStandaloneLossRecoversAfterCooldownAcrossRestart:
  fresh login has no persisted cooldown
```

修复后检查：

| 声明 | 检查 | 结果 |
| --- | --- | --- |
| 普通失效冷却后可恢复，重启不绕过冷却 | 控制器 dispatcher 回归，读取持久化状态重建 controller | 通过 |
| 暂停、在途登录、配对授权仍受保护 | `TestCooldownCannotOverridePausedPendingOrPairedAuthority` 与既有退出事件测试 | 通过 |
| 原生 worker 协作、全仓兼容 | `go test ./...` | 通过 |
| 并发授权检查 | `go test -race ./internal/gateway` | 通过 |
| 静态检查 | `go vet ./...` | 通过 |
| 已安装命令 | `sii help`、错误组合 `sii client Mini --standalone` | 帮助列出新入口；错误组合退出 2 |
| 本机独立角色 | `sii server --standalone --json`、`sii doctor --json` | server，无 peer，会话 valid，ready=true |
| 真实代理入口 | curl 经 HTTP 8888、SOCKS 1080、FlClash 7890 访问 QZ | 三条均返回 302 |
| 浏览器实际入口 | Chrome 打开 QZ，经已有 SSO 自动跳转 | 显示已登录项目列表 |

本机安装版本为 `dev-standalone-recovery-20261109`。旧二进制与 plist 保存在
本地私有 state 目录的 `backups/standalone-recovery-20261109-*`，未复制凭据进仓库。
安装时曾出现 launchd bootstrap 退出 5；手动重试同一 bootstrap 成功，
最终 supervisor/provider 与三条真实代理检查均通过。

限制：回归通过时间期限和持久化重建模拟睡眠后的授权条件，没有强制让用户机器
睡眠，也没有故意使线上 SII 会话失效。未对离线 Mini 做变更。
额外只读 QZ CLI 检查遇到其本机凭据缺失；浏览器已登录验收独立通过，
该 CLI 问题不作为 SII 网络修复成果，也未借此改动 QZ 授权。

审查：按当前用户需求检查独立模式、冷却持久化、角色变更竞态与退出事件保护；
未发现阻断项。现有配对交接协议保持不变。
