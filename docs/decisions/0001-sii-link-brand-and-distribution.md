# ADR 0001: SII Link brand and distribution

- Status: accepted
- Date: 2026-08-01

## Decision

The SII-focused derivative is named **SII Link**. Its repository, Go module,
binary, release assets, configuration namespace, state namespace, and log
namespace use `sii-link`. The macOS service label is
`dev.hopecommon.sii-link`.

The local compatibility contract remains SOCKS5 on `127.0.0.1:1080` and HTTP
on `127.0.0.1:8888`, so existing downstream routing does not change. The old
Docker deployment remains a stopped rollback option during the transition but
must not run concurrently with the native service.

The current private operational repository remains private because its history
contains host-specific audit material. Public releases come from a separate
standalone repository that preserves the public ZJU Connect history and applies
the reviewed SII changes without private deployment records.

SII Link remains under AGPLv3. Public documentation must identify the ZJU
Connect and EasierConnect provenance and state that the project is independent,
unofficial, and not endorsed by any named institution or vendor. Release
archives include corresponding source links and the linked dependency license
closure.

## Consequences

- Existing Keychain service names `atrust.username` and `atrust.password` remain
  a compatibility interface; secrets do not need to be copied or renamed.
- The old binary, config, state, log, and LaunchAgent paths may remain briefly
  for rollback, but only the new LaunchAgent is enabled.
- Module imports move to `github.com/hopecommon/sii-link`, matching Go's public
  module path and allowing versioned installation from the public repository.
- Generic upstream protocol behavior remains available, while the new README
  leads with the maintained SII use case rather than claiming superiority over
  official or upstream clients.
