# SII Link

English | [中文](README.md)

SII Link is a lightweight command-line client for SII aTrust deployments. It
runs as a native Go binary and provides local SOCKS5/HTTP proxies, unattended
CAS authentication, macOS Keychain and Linux kernel-keyring credential loading, event-driven wake
recovery, and bounded log rotation. It is intended for users who do not want to
keep a full Docker environment running for one VPN connection.

> **Unofficial project.** SII Link is independently maintained and is not
> affiliated with, authorized by, endorsed by, or supported by SII, ZJU,
> Sangfor, or their affiliates. Follow your institution's network and account
> policies. The software is provided as-is and is used at your own risk.

SII Link is a modified derivative of
[ZJU Connect](https://github.com/Mythologyli/zju-connect), which is based on
[EasierConnect](https://github.com/lyc8503/EasierConnect). Thanks to their
authors and contributors. See [NOTICE.md](NOTICE.md) for provenance,
modification dates, and third-party notices.

## Goals

- Run natively without a Docker daemon, privileged container, or TUN access for
  the normal local-proxy setup.
- Renew SII CAS sessions from macOS Keychain without persisting the static
  password in TOML, state, or logs.
- Run each Linux host as an independent SII data plane after provisioning its
  CAS credentials into the kernel keyring; no Mac traffic relay is required.
- Check immediately after a macOS wake event while keeping idle checks
  infrequent instead of polling every 15 seconds.
- Reduce repeated aTrust TLS setup with an optional short-tunnel transport pool
  and first-packet/connection overlap.
- Bound logs to a 5 MiB active file plus three backups by default, about 20 MiB
  total.
- Retain the upstream EasyConnect/aTrust, SOCKS5, HTTP, DNS, forwarding, and TUN
  capabilities. Use `sii-link -h` and the upstream documentation for generic
  configurations.

## Quick start on macOS

Download the matching archive from
[Releases](https://github.com/hopecommon/sii-link/releases), or run:

```bash
curl -fsSL https://raw.githubusercontent.com/hopecommon/sii-link/main/scripts/install-macos.sh | sh
```

Store the CAS credentials in the current user's Keychain. The `security`
command prompts for the values and they are not written to the repository:

```bash
security add-generic-password -U -a "$USER" -s atrust.username -w
security add-generic-password -U -a "$USER" -s atrust.password -w
```

Copy [configs/sii-local.toml.example](configs/sii-local.toml.example) to
`~/.config/sii-link/config.toml`, then replace the absolute paths and account
placeholder. Recommended production paths are:

```text
~/.local/bin/sii-link
~/.config/sii-link/config.toml
~/.local/state/sii-link/client-data.json
~/Library/Logs/sii-link/sii-link.log
~/Library/LaunchAgents/dev.hopecommon.sii-link.plist
```

Replace the paths in the
[LaunchAgent template](deploy/dev.hopecommon.sii-link.plist.example), then load
it:

```bash
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/dev.hopecommon.sii-link.plist"
launchctl enable "gui/$(id -u)/dev.hopecommon.sii-link"
launchctl kickstart -k "gui/$(id -u)/dev.hopecommon.sii-link"
```

The service starts after graphical login, not before FileVault login. If the
CAS cookie has expired, the first background Keychain access may require user
approval.

## Quick start on Linux

Copy the [Linux configuration example](configs/sii-linux.toml.example) and set
real state and log paths. The production Linux config selects a credential
source without containing a username or password:

```toml
sii_unattended_cas = true
sii_credential_source = "kernel-keyring"
```

The provisioning interface reads one JSON object exclusively from stdin. Use
a trusted helper that does not expand the secret through a shell; never place
the password in argv, environment variables, or shell history:

```text
sii-link credentials put [--ttl 8h]
sii-link credentials status
sii-link credentials revoke
```

Credentials have no TTL by default and remain until explicit revocation,
kernel cleanup, or reboot. `--ttl` is only for temporary hosts. Kernel-keyring
storage is not written to disk, but it cannot isolate credentials from root or
another process with the same UID. Do not provision personal credentials into
a shared-root account.

## Ports and compatibility

The production SII profile keeps the existing local contract:

| Interface | Address |
| --- | --- |
| SOCKS5 | `127.0.0.1:1080` |
| HTTP | `127.0.0.1:8888` |

Downstream clients therefore do not need port changes. SSH can use the host
SOCKS endpoint directly:

```sshconfig
ProxyCommand /usr/bin/nc -X 5 -x 127.0.0.1:1080 %h %p
```

Do not normally run the legacy Docker session and SII Link at the same time:
they can invalidate each other's account session and compete for local ports.
Keep the old container stopped as a rollback option during migration.

## Production profile

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

Rotation is size based and self-contained: a full active file becomes `.1`,
older backups move up, and files beyond `log_max_backups` are deleted. No
external `logrotate` service is required.

## Build and release

Use Go 1.25.6 or a compatible toolchain:

```bash
go test ./...
go vet ./...
go build -trimpath -o sii-link .
```

`scripts/build-release.sh` creates official archives. Each archive includes the
AGPLv3 license, derivative notice, and the exact target binary's dependency
license closure. The build fails if a linked module has no root license file.
See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for the current inventory.

## Security and privacy

- `client-data.json` contains session cookies. Keep it at mode `0600` and never
  commit or share it.
- Linux kernel-keyring credentials have no default TTL but do not survive a
  reboot. Provision them again from a trusted host when needed. They do not
  isolate root or processes running under the same UID.
- Never put passwords, CAS tickets, SIDs, device IDs, or sign keys in configs,
  issues, or logs.
- Unattended SII mode only sends credentials to the fixed SII HTTPS endpoints
  and enables system trust-store TLS verification.
- Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

The combined project and modifications are distributed under the
[GNU AGPLv3](LICENSE). Binary releases must provide the exact corresponding
source and all applicable third-party notices. Copyright remains with the
respective contributors.
