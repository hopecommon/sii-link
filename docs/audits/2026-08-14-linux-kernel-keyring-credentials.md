# Linux kernel-keyring credential validation

- Date: 2026-08-14
- Tracking: [issue #1](https://github.com/hopecommon/sii-link/issues/1)
- Scope: Linux keyring credential storage, credential CLI, source selection,
  cross-platform compatibility, and documentation

## Claim ledger

| Claim | Check | Result |
| --- | --- | --- |
| Default provisioning has no TTL | Store/CLI tests and Linux `credentials put` followed by `status` | Pass: `expires_at` is null |
| Explicit TTL expires | Linux `credentials put --ttl 1s`, wait, then `status` | Pass: status becomes unavailable |
| Credentials survive process boundaries | Run `put` and `status` in separate Linux processes | Pass |
| Revoke is explicit and observable | Linux `revoke`, then `status` | Pass: revoked true, available false |
| Kernel permissions exclude group/other | Inspect the test key in `/proc/keys` | Pass: `3f3f0000` |
| No external `keyctl` executable is required | Run the release-shaped binary on a host without `keyctl` | Pass |
| macOS and Linux builds remain valid | Full Go tests/vet plus darwin/arm64 and linux/amd64+arm64 builds | Pass |

## Linux Canary

The Linux syscall path was exercised on HK-DEV with a temporary cross-built
binary and synthetic credentials. No SII authentication request was made.
The test covered explicit expiry, default no-expiry storage, a second-process
read, non-secret status output, and revocation. The temporary binary was
removed and no SII Link or qz login daemon remained running.

Real SII authentication and installed-release validation belong to the
downstream chezmoi migration. That Canary must stop both Linux services after
the read-only QZ check to avoid leaving a concurrent platform session active.
