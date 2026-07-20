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
  RFB server served over the tunnel, and **auto-reconnects** (with backoff) if
  the relay connection drops.
- **Viewer** dials the relay, presents the ID + PIN, and the relay splices the
  two SSH connections together — rendered in a built-in window, or (with
  `--forward`) exposed as a local port for any standard VNC client.
- Both hops are SSH-encrypted. The relay only bridges after the host **accepts**
  the incoming session (unless run `--unattended`).
- Only changed screen tiles are sent on incremental updates (dirty-region
  diffing), keeping bandwidth low for mostly-static screens.

The **`remotedesk` desktop app** rolls host and viewer into one GUI (like
AnyDesk): one window shows this machine's ID/PIN so it can be controlled, and a
form lets you enter a remote ID/PIN to control another machine. The standalone
`host` and `viewer` CLIs remain for headless and scripted use.

Run any binary with `--version` to print build info.

## Status

| Milestone | State |
|-----------|-------|
| 1. Relay SSH broker + tunnel | ✅ done, tested |
| 2. Embedded RFB server (capture + input) | ✅ done, tested |
| 3. End-to-end + consent gate + tray | ✅ done, tested |
| 4. Self-contained Go viewer GUI | ✅ done, tested |
| 5. Windows build + cross-NAT | 🟡 cross-compiles; runtime needs a Windows box |

## Build

Requires Go 1.23+, a C toolchain, and (Linux) X11/GL + Wayland dev headers:

```sh
sudo apt-get install -y gcc libc6-dev \
  libx11-dev libxtst-dev libxkbcommon-dev libxi-dev \
  libgl1-mesa-dev libxcursor-dev libxinerama-dev libxrandr-dev libxxf86vm-dev \
  libwayland-dev wayland-protocols libegl-dev libgles-dev libasound2-dev pkg-config
```

```sh
make            # native build into bin/ (relayd, host, viewer, remotedesk)
make test       # run the test suite
```

### Cross-compiling for Windows (from Linux)

Requires the mingw-w64 toolchain: `apt install gcc-mingw-w64-x86-64`.

```sh
make windows    # -> bin/windows/{relayd,host,viewer,remotedesk}.exe
```

The relay is pure Go (`CGO_ENABLED=0`); `host.exe`, `viewer.exe`, and
`remotedesk.exe` are built with CGO via `x86_64-w64-mingw32-gcc` for screen
capture, input injection, the tray, and the GUI. They are linked `-H windowsgui`
so no console window appears.

## Run

On your public VPS:

```sh
./relayd --listen :7700
```

### Desktop app (host + viewer in one)

The simplest way to use remotedesk. Launch it on each machine:

```sh
./remotedesk --relay RELAY_IP:7700 --relay-key "ssh-ed25519 AAAA..."
```

The window shows **this** machine's ID and PIN (share them to let someone in),
and has a **Connect to a remote computer** form — type the other machine's ID +
PIN and hit Connect to control it. Incoming requests pop up an Accept/Reject
prompt. Pass `--unattended` to auto-accept.

### Standalone CLIs (headless / scripted)

Run the machine to be controlled as a console or tray agent:

```sh
./host --relay RELAY_IP:7700          # console UI, prompts to accept
./host --relay RELAY_IP:7700 --tray   # system-tray UI
```

Connect from the standalone viewer (built-in window — no external VNC client):

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
- Access is gated by the session PIN **and** an explicit host Accept prompt.
- **Brute-force protection.** The relay locks out a host after a few wrong PINs
  (`--pin-attempts`, default 5) for a cooldown window (`--lockout`, default 30s)
  and alerts the host, so the 6-digit PIN can't be guessed at speed. Slow-loris
  clients are dropped by a handshake deadline (`--handshake-timeout`), and
  concurrent connections are capped (`--max-conns`).
- **Restrict who can connect.** For a private relay, pass `--authorized-keys` to
  allow only agents whose public key you've listed (see [deploy/](deploy/)).
- The relay terminates both SSH hops, so it can see plaintext VNC — the trust
  model is "you own the relay" (same as TeamViewer's relays). An in-tunnel TLS
  layer for true end-to-end encryption is the next planned hardening step.

## Deploy the relay

The relay is pure Go and runs unattended on a public host. Pull the prebuilt,
multi-arch image (no local build needed) — `:edge` tracks `main`, and `:latest`
/ `:X.Y.Z` are published once release tags are cut:

```sh
docker run -d -p 7700:7700 -v remotedesk-relay:/data \
  ghcr.io/rnetis/remotedesk-relay:edge
```

See [deploy/](deploy/) for a hardened **systemd** unit, the **Docker** details,
and the full hardening reference. `relayd` handles `SIGTERM`/`SIGINT` for clean
shutdown under either.

## Layout

- `cmd/{relayd,host,viewer}` — the relay and the standalone host/viewer CLIs.
- `cmd/remotedesk` — the unified host+viewer desktop app (Fyne GUI).
- `internal/relay` — SSH broker, ID/PIN registry, consent bridging.
- `internal/rfb` — RFB 3.8 server (handshake, VNC auth, Raw encoding).
- `internal/capture` / `internal/input` — screen capture / input injection.
- `internal/tunnel` — SSH client helpers (host reverse side, viewer forward side).
- `internal/tray` — console + systray front-ends.
- `internal/{id,config,wire}` — IDs/PINs, key storage, control protocol.
