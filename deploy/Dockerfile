# Container image for the remotedesk relay (relayd).
#
# Only the relay is containerized: it is pure Go (no CGO) and is the component
# that runs unattended on a public host. The host agent and viewer need a
# desktop session and are distributed as native binaries instead.
#
# Prebuilt multi-arch (amd64/arm64) images are published to GHCR by
# .github/workflows/docker.yml — pull instead of building (:edge tracks main,
# :latest / :X.Y.Z once a release is tagged):
#   docker pull ghcr.io/rnetis/remotedesk-relay:edge
#
# Or build locally from the repository root:
#   docker build -f deploy/Dockerfile -t remotedesk-relay .
#
# Run, persisting the relay host key so its fingerprint is stable across
# restarts (agents pin it with --relay-key):
#   docker run -d --name relay -p 7700:7700 \
#     -v remotedesk-relay:/data remotedesk-relay
#
# The image runs as the distroless "nonroot" user (uid 65532). /data is created
# owned by that user, so a fresh named volume inherits writable ownership and
# relayd can create its host key on first start. (A bind mount instead of a
# named volume must be chowned to 65532 on the host.)

# The build stage runs on the native builder platform and cross-compiles to the
# target platform (Go makes this cheap and CGO is off), so multi-arch builds
# need no emulation. TARGETOS/TARGETARCH are provided by BuildKit; they are
# empty on the legacy builder, which harmlessly yields a native build.
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags "-s -w -X remotedesk/internal/version.Version=${VERSION}" \
    -o /out/relayd ./cmd/relayd
# Staging dir copied into the final image with nonroot ownership below.
RUN mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/relayd /usr/local/bin/relayd
COPY --from=build --chown=65532:65532 /data /data
EXPOSE 7700
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/relayd"]
CMD ["--listen", ":7700", "--hostkey", "/data/relay_host_key"]
