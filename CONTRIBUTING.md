# Contributing to remotedesk

Thanks for your interest! This is a Go project with three binaries (`relayd`,
`host`, `viewer`) around shared packages in `internal/`.

## Development setup

Requires Go 1.25+, a C toolchain, and (on Linux) X11/GL headers:

```sh
sudo apt-get install -y gcc libc6-dev \
  libx11-dev libxtst-dev libxkbcommon-dev libxi-dev \
  libgl1-mesa-dev libxcursor-dev libxinerama-dev libxrandr-dev \
  libxxf86vm-dev libasound2-dev pkg-config
```

Build and test:

```sh
make          # native build into bin/
make test     # go test ./...
make windows  # cross-compile (needs gcc-mingw-w64-x86-64)
```

## Before opening a PR

- `gofmt -w .` — code must be gofmt-clean (CI checks `go vet`).
- `go test ./...` — all tests must pass. Add tests for new behavior; the
  protocol packages (`rfb`, `rfbclient`, `relay`, `tunnel`) have fast,
  hermetic tests using `net.Pipe`/loopback that you can model new cases on.
- Keep changes focused and describe the user-visible effect in the PR.

## Project layout

| Path | Responsibility |
|------|----------------|
| `cmd/{relayd,host,viewer}` | the three binaries |
| `internal/relay` | SSH broker, ID/PIN registry, consent bridging |
| `internal/rfb` | RFB 3.8 server (handshake, auth, encodings, diffing) |
| `internal/rfbclient` | RFB client decoder used by the built-in viewer |
| `internal/capture` / `internal/input` | screen capture / input injection (CGO) |
| `internal/tunnel` | SSH client helpers (host reverse / viewer forward) |
| `internal/tray` | console + systray front-ends |
| `internal/{id,config,wire,version}` | IDs/PINs, keys, control protocol, build info |

## Reporting security issues

Please do **not** open a public issue for vulnerabilities — see
[SECURITY.md](SECURITY.md).
