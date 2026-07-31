# Running as a service

## macOS / SII

Use the user LaunchAgent template at
[`deploy/dev.hopecommon.sii-link.plist.example`](../deploy/dev.hopecommon.sii-link.plist.example).
It starts after graphical login, restarts after non-zero exits, and sends normal
logs to the application's bounded rotating writer. See the
[native SII migration guide](sii-native-migration.md) for configuration,
Keychain, and wake-recovery details.

## Linux / systemd

Install the binary at `/usr/local/bin/sii-link` and the configuration at
`/etc/sii-link/config.toml`. Directories and files containing passwords or
session state should use modes `0700` and `0600`, respectively.

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

Save it as `/etc/systemd/system/sii-link.service`, then run:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now sii-link
sudo systemctl status sii-link
journalctl -u sii-link -f
```

When `log_file` is configured, the application bounds files using
`log_max_size_mb` and `log_max_backups`. Otherwise output goes to journald and
uses the system retention policy. TUN mode needs additional network privileges;
do not grant root or `CAP_NET_ADMIN` for a normal SOCKS5/HTTP proxy setup.

For OpenWrt, TUN, and generic ZJU configurations, consult the
[upstream service documentation](https://github.com/Mythologyli/zju-connect/blob/main/docs/service_en.md).
