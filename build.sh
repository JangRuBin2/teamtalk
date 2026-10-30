#!/usr/bin/env bash
# Builds TeamTalk binaries for Windows, macOS and Linux into dist/.
# Set RELAY_URL to bake in the relay server address (see relay/README.md).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
build() {
	echo "building $3"
	CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w -X main.relayURL=${RELAY_URL:-}" -o "dist/$3" .
}
build windows amd64 teamtalk-windows.exe
build darwin  arm64 teamtalk-mac-applesilicon
build darwin  amd64 teamtalk-mac-intel
build linux   amd64 teamtalk-linux
