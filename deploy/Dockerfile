# Container image for the remotedesk relay (relayd).
#
# Only the relay is containerized: it is pure Go (no CGO) and is the component
# that runs unattended on a public host. The host agent and viewer need a
# desktop session and are distributed as native binaries instead.
#
# Build from the repository root:
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

FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build \
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
