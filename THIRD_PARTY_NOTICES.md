# Third-party notices

SII Link includes and links third-party software. Copyright and license terms
remain with the respective authors. This inventory is the union of the current
Darwin, Linux, and Windows runtime dependency closures; a particular binary may
link only a subset.

## GNU GPLv3 or later

- `github.com/mythologyli/sing-tun`
- `github.com/sagernet/sing`

These components are compatible with the combined AGPLv3 work under AGPLv3
section 13. Their covered portions remain subject to their GPL terms.

## Apache License 2.0

- `github.com/containers/winquit`
- `github.com/ebitengine/purego`
- `github.com/google/btree`
- `github.com/pquerna/otp` (including its `NOTICE` file)
- `github.com/riobard/go-bloom`
- `github.com/sagernet/netlink`
- `github.com/shadowsocks/go-shadowsocks2`
- `github.com/vishvananda/netns`
- `gvisor.dev/gvisor`

## BSD-style licenses

- `github.com/beevik/etree`
- `github.com/klauspost/compress`
- `github.com/miekg/dns`
- `github.com/refraction-networking/utls`
- `github.com/shirou/gopsutil/v4`
- `go4.org/intern`
- `go4.org/unsafe/assume-no-moving-gc`
- `golang.org/x/crypto`
- `golang.org/x/exp`
- `golang.org/x/net`
- `golang.org/x/sys`
- `golang.org/x/time`
- `inet.af/netaddr` (implemented by `github.com/inetaf/netaddr`)

## MIT and MIT-like licenses

- `github.com/BurntSushi/toml`
- `github.com/andybalholm/brotli`
- `github.com/boombuler/barcode`
- `github.com/go-ole/go-ole`
- `github.com/patrickmn/go-cache`
- `github.com/scjalliance/comshim`
- `github.com/sirupsen/logrus`
- `github.com/things-go/go-socks5`
- `github.com/yusufpapurcu/wmi`
- `golang.zx2c4.com/wireguard`
- `golang.zx2c4.com/wireguard/windows`
- `golang.zx2c4.com/wintun`

The source tree also preserves local embedded-code notices in
`service/http.go`, `service/shadowsocks.go`, and `internal/ping/LICENSE`.

Official release archives contain the complete license and notice files copied
from the exact selected module versions under `LICENSES/`. The packaging script
fails if any linked module does not expose a root license file. The archive's
corresponding source is the Git tag with the same version at
<https://github.com/hopecommon/sii-link>.
