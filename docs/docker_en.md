# Docker compatibility mode

The primary SII Link deployment is the native binary. The Dockerfile remains
for users who need isolated generic EasyConnect/aTrust configurations. macOS
Keychain access, wake events, and LaunchAgent integration are only available in
the native macOS setup.

Create a `config.toml` without embedded secrets, then build from the current
source:

```bash
docker compose up -d --build
docker compose logs -f sii-link
```

The default Compose file maps SOCKS5 port `1080` and HTTP port `1081`. To use
the SII production HTTP port `8888`, change both `http_bind` in the config and
the Compose port mapping.

Never bake passwords, CAS tickets, or `client-data.json` into an image. Mount a
data volume containing mode-`0600` state files when persistence is required.
Official releases do not promise a published container image; build from the
source tag matching the intended version.
