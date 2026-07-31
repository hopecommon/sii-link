# Notices and provenance

SII Link is an independent, unofficial modified derivative of
[ZJU Connect](https://github.com/Mythologyli/zju-connect) by Mythologyli and its
contributors. ZJU Connect is based on
[EasierConnect](https://github.com/lyc8503/EasierConnect) by lyc8503 and its
contributors. Copyright remains with the respective contributors; the
[upstream contributor graph](https://github.com/Mythologyli/zju-connect/graphs/contributors)
records that authorship.

The SII Link modifications were developed during 2026 and include SII CAS
automation, credential isolation, TLS hardening for that flow, wake-aware
recovery, aTrust transport reuse, native macOS service integration, bounded log
rotation, packaging, and documentation. The product, module, binary, service,
and release names were changed to distinguish this derivative from upstream.

SII Link is not affiliated with, authorized by, endorsed by, or supported by
SII, Zhejiang University, Sangfor Technologies, or any of their affiliates. No
institutional or vendor logo is used. Names and endpoints appear only to
describe technical interoperability.

The combined work is licensed under the GNU Affero General Public License,
version 3, in [LICENSE](LICENSE). Embedded source notices, including the
MIT-derived code notice in `service/http.go`, the Apache-2.0-derived notice in
`service/shadowsocks.go`, and `internal/ping/LICENSE`, remain in place. Release
archives include the license and notice files for every linked third-party Go
module under `LICENSES/`; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
