# Security policy

## Supported versions

Security fixes are applied to the latest release and the `main` branch. Older
releases may require upgrading rather than receiving a backport.

## Reporting a vulnerability

Do not open a public issue containing credentials, session data, internal host
details, or an unpatched vulnerability. Use GitHub's private vulnerability
reporting for `hopecommon/sii-link`. If that entry point is unavailable, contact
the repository owner through the private address on their GitHub profile.

Include the affected version, platform, minimal reproduction, expected impact,
and whether any secret may have been exposed. Remove passwords, CAS tickets,
cookies, SIDs, device IDs, sign keys, and private network addresses from logs.

## Local secret boundary

- CAS username and password belong in macOS Keychain or separate `0600` files.
- `client-data.json` contains session cookies and must remain `0600`.
- Configuration, logs, crash reports, and issues must not contain static
  credentials or live authentication artifacts.
