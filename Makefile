# remotedesk build targets.
#
# Native build (host OS):        make
# Linux amd64:                   make linux
# Windows amd64 (cross, needs mingw): make windows
# Everything:                    make all
#
# CGO is required for the host and viewer (screen capture, input, GUI). The
# relay is pure Go and cross-compiles with CGO disabled.

BIN     := bin
PKGS    := ./cmd/relayd ./cmd/host ./cmd/viewer
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DATE    := $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w -X remotedesk/internal/version.Version=$(VERSION) -X remotedesk/internal/version.Date=$(DATE)

.PHONY: all native linux windows relay test clean

native:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BIN)/ $(PKGS)

linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
		go build -ldflags "$(LDFLAGS)" -o $(BIN)/linux/ $(PKGS)

# Cross-compile for Windows. Requires: apt install gcc-mingw-w64-x86-64
windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
		go build -ldflags "$(LDFLAGS)" -o $(BIN)/windows/relayd.exe ./cmd/relayd
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
		CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
		go build -ldflags "$(LDFLAGS) -H windowsgui" -o $(BIN)/windows/host.exe ./cmd/host
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
		CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
		go build -ldflags "$(LDFLAGS) -H windowsgui" -o $(BIN)/windows/viewer.exe ./cmd/viewer

# Pure-Go relay only (no CGO), any platform.
relay:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/relayd

all: linux windows

test:
	go test ./...

clean:
	rm -rf $(BIN)
