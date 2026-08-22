#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

target_arch="${GOARCH:-amd64}"

mkdir -p dist
go test ./...
GOOS=windows GOARCH="$target_arch" go build \
	-trimpath \
	-ldflags="-s -w -H=windowsgui" \
	-o dist/sidebox.exe \
	.

echo "Built Windows/$target_arch: dist/sidebox.exe"
