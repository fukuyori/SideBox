#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

app_dir="${SIDEBOX_APP_DIR:-dist/Sidebox.app}"
sign_identity="${SIDEBOX_APP_SIGN_IDENTITY:--}"

mkdir -p "$app_dir/Contents/MacOS"
cp macos/Info.plist "$app_dir/Contents/Info.plist"

go test ./...
CGO_ENABLED=1 GOOS=darwin go build \
	-trimpath \
	-ldflags="-s -w" \
	-o "$app_dir/Contents/MacOS/Sidebox" \
	.
if [ "$sign_identity" = "-" ]; then
	codesign --force --sign - --identifier jp.fukuyori.sidebox "$app_dir"
else
	codesign --force --timestamp --options runtime \
		--sign "$sign_identity" "$app_dir"
fi
codesign --verify --deep --strict "$app_dir"

echo "Built: $app_dir"
echo "Run: open $app_dir"
