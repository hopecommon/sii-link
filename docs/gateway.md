# Gateway roles and recovery

Use `sii` for both manual operations and agent subprocesses. It invokes the
installed `sii-link gateway` controller; the native configuration and credentials
stay local. SOCKS and HTTP endpoints retain their configured loopback ports.

```sh
sii server                         # select this machine
sii client Mini                    # forward through an SSH peer
sii off                            # revoke local login permission, keep supervisor
sii status --json                  # inspect without changing roles
sii doctor --json                  # inspect cache and peer without logging in
```

The first selection can supply `--config /absolute/path/config.toml`, `--peer Mini`
and `--self Air`. `--self` is an SSH address the other machine can use; the default
is the current username plus Tailscale DNS name, or hostname. SSH must already
have appropriate keys, host trust and routing on both machines. The controller
uses BatchMode and never creates SSH trust or copies credentials.

`sii server` stops this machine's previous relay, asks the reachable peer to
persist Client mode and stop its native provider, then selects the local Server.
The command waits for proxy readiness. An unreachable peer permits explicit
takeover; a reachable peer with unsupported control protocol blocks handover.
Both peers need this version for reciprocal one-command switching.

`sii install` installs or refreshes the user service and restores its saved role.
First installation starts in Off. Updates, supervisor restarts and wake events
restore existing intent; they grant no new Server authority. `sii run` is the
foreground supervisor entrypoint used by launchd or systemd. macOS service
installation backs up the previous plist and disables the old independent Mini
relay. It preserves the selected native configuration, including alternate file
credential configurations.

## Session permission

Cached SID validation precedes fresh authentication. A valid SID needs no new
login permission. In managed mode, only the supervisor's selected native worker
can request a fresh login. Before granting it the controller checks the peer:
a ready or acquiring peer Server becomes the upstream. Unknown peer status
allows one attempt under the existing Server grant.

The renewal budget and pending receipt are saved before authentication. A
successful login remains pending until its cookie cache has been written.
Authentication that failed while obtaining a CAS ticket, before submitting a
gateway callback, can retry automatically. An uncertain submitted result or a
second invalid session pauses new authentication; cached resume remains possible.
Two minutes of continuously healthy proxy checks replenish a spent budget.
The supervisor continues transport recovery with backoff capped at 30 seconds.

The gateway polls the published SII event endpoint using its existing session
and saved cursor. Confirmed `relogin` or `trustDevice` logout revokes Server
permission and selects Client when a peer is configured, otherwise Off.
`timeout` and `timeoutOffline` trigger ordinary recovery. Other policy logout
types pause new authentication. A late event is checked against the current
cache, authentication generation and current session validity before acting.
Empty events and failed requests establish no logout cause.

The portal's device-list endpoint exposes online state but denied this project's
invalid-session read probes. It is therefore not used as proof of an empty
account. SSH preflight, typed events and bounded recovery reduce conflicts;
they do not provide atomic platform admission during a partition or cancel an
already-submitted server-side login. Offline takeover accepts this residual risk.

## Diagnosis and storage

`status --json` reports role, upstream, provider readiness, pending authentication,
renewal availability and pause reason. `doctor` adds cache validity and a bounded
peer control probe. Both commands are read-only and never promote Client mode.
An explicit `sii server` renews authority after ordinary pause; a pending outcome
should first be diagnosed rather than retried as a new login.

Private controller state lives beside the default native cache under
`~/.local/state/sii-link/`: `gateway.json`, `gateway.sock` and bounded
`gateway.log`. Status output excludes cookies, cursor and session fingerprint.
The control socket is user-private. Native logs stay at the configured `log_file`.

For foreground development, use an absolute `--state-dir` with `sii run` and
subsequent control commands. Custom state directories require explicit foreground
supervision and never rewrite the installed service. Generic unmanaged native
CLI usage remains available until a gateway controller has been initialized.
