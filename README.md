# remotedesk

[![ci](https://github.com/rnetis/remotedesk/actions/workflows/ci.yml/badge.svg)](https://github.com/rnetis/remotedesk/actions/workflows/ci.yml)
[![release](https://github.com/rnetis/remotedesk/actions/workflows/release.yml/badge.svg)](https://github.com/rnetis/remotedesk/releases)
[![go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

A cross-platform AnyDesk/TeamViewer-style remote desktop tool in Go. It pairs an
embedded VNC (RFB) server with a self-hosted **SSH relay** so two machines behind
NAT can connect with no manual port-forwarding.

```
[Host agent behind NAT] --ssh -R--> [Relay VPS, public IP] <--ssh -L-- [Viewer]
   embedded RFB server                 SSH broker + ID/PIN registry
```

- **Host** dials *out* to the relay and registers, receiving a connection **ID**
  and one-time **PIN**. It captures the screen and injects input via an embedded
  RFB server served over the tunnel.
- **Viewer** dials the relay, presents the ID + PIN, and the relay splices the
  two SSH connections together. Any standard VNC client (or the built-in viewer,
  once Milestone 4 lands) connects through it.
- Both hops are SSH-encrypted. The relay only bridges after the host **accepts**
  the incoming session (unless run `--unattended`).

## Status

| Milestone | State |
|-----------|-------|
| 1. Relay SSH broker + tunnel | ✅ done, tested |
| 2. Embedded RFB server (capture + input) | ✅ done, tested |
| 3. End-to-end + consent gate + tray | ✅ done, tested |
| 4. Self-contained Go viewer GUI | ✅ done, tested |
| 5. Windows build + cross-NAT | 🟡 cross-compiles; runtime needs a Windows box |

## Build

Requires Go 1.23+, a C toolchain, and (Linux) X11 dev headers:
`libx11-dev libxtst-dev libxkbcommon-dev libxi-dev`.

```sh
make            # native build into bin/
make test       # run the test suite
```

### Cross-compiling for Windows (from Linux)

Requires the mingw-w64 toolchain: `apt install gcc-mingw-w64-x86-64`.

```sh
make windows    # -> bin/windows/{relayd,host,viewer}.exe
```

The relay is pure Go (`CGO_ENABLED=0`); `host.exe` and `viewer.exe` are built
with CGO via `x86_64-w64-mingw32-gcc` for screen capture, input injection, the
tray, and the GUI. They are linked `-H windowsgui` so no console window appears.

## Run

On your public VPS:

```sh
./relayd --listen :7700
```

On the machine to be controlled (note its ID/PIN, and pin the relay host key):

```sh
./host --relay RELAY_IP:7700          # console UI, prompts to accept
./host --relay RELAY_IP:7700 --tray   # system-tray UI
```

On the controller (built-in window — no external VNC client needed):

```sh
./viewer --relay RELAY_IP:7700 --id 314-798-609 --pin 058367
```

Or expose a local port for an external VNC client instead:

```sh
./viewer --relay RELAY_IP:7700 --id 314-798-609 --pin 058367 --forward --listen 127.0.0.1:5901
# then point any VNC client at 127.0.0.1:5901
```

## Security notes

- **Pin the relay host key.** `relayd` prints its key on startup; pass it to the
  agents so a man-in-the-middle can't impersonate the relay:

  ```sh
  ./host   --relay RELAY_IP:7700 --relay-key "ssh-ed25519 AAAA..."
  ./viewer --relay RELAY_IP:7700 --relay-key ./relay.pub --id ... --pin ...
  ```

  The flag accepts an inline authorized-keys line or a file path. Without it,
  connections still work but log a warning that the relay is unauthenticated.
- Access is gated by the one-time PIN **and** an explicit host Accept prompt.
- The relay terminates both SSH hops, so it can see plaintext VNC — the trust
  model is "you own the relay" (same as TeamViewer's relays). An in-tunnel TLS
  layer for true end-to-end encryption is the next planned hardening step.

## Layout

- `cmd/{relayd,host,viewer}` — the three binaries.
- `internal/relay` — SSH broker, ID/PIN registry, consent bridging.
- `internal/rfb` — RFB 3.8 server (handshake, VNC auth, Raw encoding).
- `internal/capture` / `internal/input` — screen capture / input injection.
- `internal/tunnel` — SSH client helpers (host reverse side, viewer forward side).
- `internal/tray` — console + systray front-ends.
- `internal/{id,config,wire}` — IDs/PINs, key storage, control protocol.
