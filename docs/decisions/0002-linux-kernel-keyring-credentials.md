# ADR 0002: Linux CAS credentials use the kernel keyring

- Status: accepted
- Date: 2026-08-14
- Tracking: [issue #1](https://github.com/hopecommon/sii-link/issues/1)

## Context

Personal Linux control hosts need to run SII Link natively without routing
their traffic through a Mac. Copying the SII CAS password into home-directory
files would make credential lifetime depend on backups, dotfile tooling, and
manual cleanup. Requiring a live Mac connection would instead make the Mac a
data-plane dependency for every host.

The operating model permits reprovisioning after a host reboot. It does not
require unattended cross-reboot recovery in this phase.

## Decision

Linux uses the per-UID persistent kernel keyring for the SII CAS credential
bundle. The bundle is written atomically through `sii-link credentials put`,
which reads one JSON object from standard input. The static credentials do not
appear in command arguments, environment variables, configuration files,
stdout, or application logs.

Credentials have no application TTL by default. They remain available until
explicit revocation, kernel keyring cleanup, or reboot. `--ttl` is an explicit
temporary-host policy, not the default. macOS continues to use Keychain, and
private credential files remain an explicit compatibility adapter rather than
the Linux production path.

The key is linked into both the per-UID user keyring and persistent keyring.
The user-keyring link keeps it anchored while the user manager or SII Link is
active; the persistent link provides restart and logout recovery subject to
the host kernel policy.

The existing `CredentialSource` seam owns all credential lookup. Linux adds a
kernel-keyring adapter; CAS protocol and ticket construction do not learn
about provisioning or storage.

`credentials status` returns exit code 0 when credentials are available and
exit code 3 when they are absent, while emitting the same non-secret JSON in
both cases. This gives service managers a stable readiness condition without
parsing log text.

## Consequences

- Each Linux host carries its own SII traffic and can reauthenticate without a
  live Mac after provisioning.
- A process restart can reacquire the credential while the kernel key remains
  available. A reboot requires another grant.
- The kernel keyring protects against persistent file and backup exposure, but
  not against root or another process running as the same UID. Personal
  credentials must not be provisioned into a shared account such as
  shared-root.
- `systemd --user` does not require sudo, but continuous execution after the
  last logout still depends on linger or another host-provided facility.
- Same-account concurrent SII sessions may conflict. Migration tests stop the
  Linux client after the Canary; multi-host concurrency is a separate live
  decision.

## Rejected alternatives

- Mac reverse proxy: makes traffic availability depend on one workstation and
  a long-lived SSH forwarding session.
- Persistent plaintext files: unnecessarily widen credential lifetime and
  copy surfaces.
- A credential broker daemon: duplicates a storage and lifecycle mechanism the
  Linux kernel already provides.
- TPM-backed encrypted credentials: useful for unattended cross-reboot
  recovery, but outside the current requirement.
