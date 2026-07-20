# Security Policy

remotedesk is remote-access software. Please treat security reports seriously.

## Reporting a vulnerability

Do **not** file a public GitHub issue for security problems. Instead, use
GitHub's private vulnerability reporting:
**Security → Report a vulnerability** on the repository, or open a private
advisory. Include reproduction steps and affected versions.

You can expect an initial response within a few days.

## Security model (what to keep in mind)

- **The relay is trusted.** It terminates both SSH hops, so it can observe the
  plaintext VNC stream. Run a relay you control. True end-to-end encryption
  (an in-tunnel TLS layer that the relay cannot read) is a planned enhancement.
- **Pin the relay host key.** Pass `--relay-key` to the host and viewer with the
  key `relayd` prints on startup. Without pinning, agents log a warning and are
  vulnerable to a man-in-the-middle impersonating the relay.
- **Access control** is a session PIN plus an explicit host Accept prompt. Do
  not use `--unattended` on machines where any inbound connection with the ID
  should not be trusted.
- **Brute-force protection.** The relay locks a host out after repeated wrong
  PINs (`--pin-attempts`, default 5) for a cooldown (`--lockout`, default 30s)
  and pushes an alert to the host, so the PIN cannot be guessed at speed.
  Handshakes have a deadline (`--handshake-timeout`) and concurrent connections
  are capped (`--max-conns`) to blunt DoS. For a private relay, restrict which
  keys may connect with `--authorized-keys`.
- **Key material** (agent keys, relay host key) is stored `0600` under the OS
  config dir. Do not commit these; `.gitignore` excludes `*_key` and `*.pem`.

## Scope

In scope: authentication bypass, MITM against pinned relays, memory-safety or
protocol-parsing bugs reachable from a peer, privilege escalation via injected
input beyond the intended session. Out of scope: issues that require already
controlling the relay you were told to trust.
