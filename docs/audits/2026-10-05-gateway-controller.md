# Gateway controller validation

Date: 2026-10-05. Scope: local SII Server / Client / Off intent, login admission,
SSH handover, supervision and role-preserving installation. The user accepted
bounded recovery and remaining partition races rather than a third coordinator.

## Claims and evidence

| Claim | Replayable check | Result |
| --- | --- | --- |
| Client / Off / unowned workers cannot create sessions | `go test -race ./internal/gateway` admission tests | Pass |
| Reservation survives process restart and installer refresh | Controller persistence and installation tests | Pass |
| Peer acquisition and concurrent handovers revoke or reject stale authority | Channel-coordinated preflight, stale receipt and crossing-selection tests | Pass |
| Submitted login stays pending until cookie persistence | Controller receipt test and real child-process fixture | Pass |
| Handover stops native worker and physical proxy listener before acknowledgement | `TestRealWorkerReceiptsAndHandoverStopProxy` | Pass |
| Cache reuse bypasses fresh admission; failed/missing session status permits no login | `go test -race ./client/atrust/auth` | Pass |
| Gateway event decoding follows deployed event/data/type contract | Auth TLS event fixture; first-party portal source inspection | Pass for decoding |
| Replacement revokes authority; verified timeout retains bounded recovery; unknown policy pauses | Logout decision and stale generation tests | Pass for local transitions |
| Installation cannot downgrade a managed binary to an old release | `python3 scripts/test-gateway-install.py` | Pass |
| Packaged CLI starts Off, holds one supervisor, exposes status and drains on termination | Same script, real unpacked macOS package | Pass |
| Existing packages and static checks remain healthy | `go test ./...`, `go vet ./...` | Pass |
| Distribution builds | macOS arm64 / Linux amd64 release script; Windows amd64 compile | Pass |

The auth fixtures use synthetic responses. The worker fixture uses a real child
process and local sockets, without credentials or live SII authentication. Race
checks were repeated three times after the final controller change.

Package smoke prerequisites:

```sh
GOOS=darwin GOARCH=arm64 DIST_DIR=dist/gateway-check-darwin ./scripts/build-release.sh
python3 scripts/test-gateway-install.py
```

Companion dotfiles validation renders both service templates and exercises the
installer with synthetic archives. It checks role-preserving restart, alternate
config selection, credential-independent Client supervision and downgrade
protection. The shared CLI wrappers delegate policy to this controller.

## Review and limits

Self-review covered the live specification, state ownership, effective native
login calls, private persistence, process drain, SSH argument construction and
both installation routes. A smaller shell-only policy would leave live login
admission and receipt persistence outside the state owner; the controller keeps
those decisions together. No third-party registry or generic event-policy
configuration was added.

Review fixes include unknown session-state handling, pacing immediate empty event
responses, preserving spent permission during updater restart, generation checks
on late health results, and guarding the public download installer against old
releases. `SessionExpiration` has no established natural-expiry meaning and takes
the unknown-policy path. Local launchd documentation confirms that its default
process-group cleanup includes child providers.

Verdict: implementation and isolated runtime pass. Live paired acceptance is
incomplete: the second machine is unavailable. Real replacement delivery, natural
session expiry over time, and reciprocal SSH handover need that machine. Windows
compilation preserves generic CLI compatibility; gateway service management targets
macOS and Linux. No release or remote deployment is asserted by this audit.

The event endpoint can return empty history for an already-invalid SID. The
device-list endpoint denied invalid-session probes, so a failed read cannot prove
an empty account. Preflight and durable budgets reduce competition; they cannot
atomically prevent a submitted login completing late or ensure a global final
manual winner while peers are partitioned.
