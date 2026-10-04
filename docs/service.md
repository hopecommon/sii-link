# 作为系统服务运行

## macOS / SII

SII 使用 `sii install` 安装用户级监督服务，首次安装选择 Off；
用 `sii server` 选择本机，或 `sii client HOST` 选择 SSH 上游。
服务管理器恢复保存的角色，监督与转发自动恢复，登录权限由控制器管理。
详见 [Server／Client 与恢复契约](gateway.md)。

用户级 LaunchAgent 模板为
[`deploy/dev.hopecommon.sii-link.plist.example`](../deploy/dev.hopecommon.sii-link.plist.example)。
它在用户图形登录后启动，以非零退出自动重启，并把正常日志交给应用内的有界
轮转器。完整配置、Keychain 和休眠恢复说明见
[SII 原生迁移指南](sii-native-migration.md)。

## Linux / SII

同样使用 `sii install` 和角色命令，生成的用户级 systemd 服务无需 root。
Client 不读取 SII 凭据；监督启动不依赖凭据文件存在。

## Linux / 通用 systemd

将二进制安装到 `/usr/local/bin/sii-link`，配置安装到
`/etc/sii-link/config.toml`。若配置包含密码或状态路径，目录和文件权限应分别为
`0700` 与 `0600`。

```ini
[Unit]
Description=SII Link
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sii-link -config /etc/sii-link/config.toml
Restart=on-failure
RestartSec=30
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

保存为 `/etc/systemd/system/sii-link.service` 后：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now sii-link
sudo systemctl status sii-link
journalctl -u sii-link -f
```

如果使用 `log_file`，应用会按 `log_max_size_mb` 和 `log_max_backups` 自行限制
文件；否则日志进入 journald，由系统保留策略管理。TUN 模式需要额外网络权限，
不应为了普通 SOCKS5/HTTP 代理授予 root 或 `CAP_NET_ADMIN`。

OpenWrt、TUN 和通用 ZJU 配置仍可参考
[上游服务文档](https://github.com/Mythologyli/zju-connect/blob/main/docs/service.md)。
