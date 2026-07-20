# Deploying the remotedesk relay

The relay (`relayd`) is the only component that runs unattended on a public
host. It is pure Go, so it builds without CGO and ships as a single static
binary. Host agents and viewers are native desktop binaries — see the top-level
[README](../README.md) for those.

## What the relay needs

- A public IP and one open TCP port (default **7700**).
- A persistent path for its **host key** so its fingerprint stays stable across
  restarts. Agents pin that fingerprint with `--relay-key`; if it changes,
  pinned agents will (correctly) refuse to connect until re-pinned.

## Option A — systemd (recommended for a VPS)

```sh
# Build a static relay binary (no CGO, no X11 headers needed):
CGO_ENABLED=0 go build -ldflags "-s -w" -o relayd ./cmd/relayd

sudo install -m0755 relayd /usr/local/bin/relayd
sudo install -m0644 deploy/remotedesk-relay.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now remotedesk-relay
```

Grab the host-key line to pin on agents:

```sh
sudo journalctl -u remotedesk-relay | grep -A1 'pin this'
```

The unit runs the relay under a `DynamicUser` with a locked-down sandbox
(`ProtectSystem=strict`, `NoNewPrivileges`, a `@system-service` syscall filter,
etc.) and stores the host key under `/var/lib/remotedesk`.

## Option B — Docker

Prebuilt multi-arch (amd64/arm64) images are published to GHCR on every push to
`main` (`:edge`) and on release tags (`:X.Y.Z` and `:latest`), so you don't have
to build locally:

```sh
docker run -d --name relay --restart unless-stopped \
  -p 7700:7700 -v remotedesk-relay:/data \
  ghcr.io/rnetis/remotedesk-relay:edge     # :latest / :X.Y.Z once a release is tagged

docker logs relay | grep -A1 'pin this'    # host-key line to pin on agents
```

The named volume keeps the host key stable across container restarts.

To build the image yourself instead of pulling:

```sh
docker build -f deploy/Dockerfile -t remotedesk-relay .
```

> **Publishing note:** GHCR packages start **private**. After the first
> `docker` workflow run, make `remotedesk-relay` public under the repo owner's
> *Packages* settings if you want anonymous `docker pull` (or authenticate with a
> token that has `read:packages`).

## Hardening flags

`relayd` ships with brute-force and DoS protections on by default; tune them as
needed:

| Flag | Default | Purpose |
|------|---------|---------|
| `--authorized-keys FILE` | *(open)* | Restrict access to agents whose public key is listed (OpenSSH `authorized_keys` format). Recommended for a private relay. |
| `--pin-attempts N` | `5` | Wrong PINs tolerated before a host is locked out. |
| `--lockout DUR` | `30s` | How long a host is locked out after too many bad PINs. |
| `--handshake-timeout DUR` | `15s` | Deadline for the SSH handshake and a peer's first channel (slow-loris protection). |
| `--max-conns N` | `512` | Maximum concurrent connections serviced at once. |

### Restricting which agents can connect

By default any key may reach the relay (the rendezvous model — hosts and viewers
are gated by the PIN and the host's Accept prompt). For a relay used only by
machines you control, collect each agent's public key and allowlist them:

```sh
# On each agent, its key is created on first run under the OS config dir, e.g.
#   ~/.config/remotedesk/host_key      (host)
#   ~/.config/remotedesk/viewer_key    (viewer)
# Print the public half:
ssh-keygen -y -f ~/.config/remotedesk/host_key >> /etc/remotedesk/authorized_keys

# Then start the relay with:
relayd --authorized-keys /etc/remotedesk/authorized_keys
```

Both host agents and viewers must present a listed key.

## Firewall

Open only the relay port; nothing else is needed:

```sh
sudo ufw allow 7700/tcp
```

## Trust model reminder

The relay terminates both SSH hops, so it can observe the plaintext VNC stream —
run a relay **you** control. Always pin the relay host key on agents with
`--relay-key`. See [SECURITY.md](../SECURITY.md) for the full model.
