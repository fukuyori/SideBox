#!/bin/bash

set -euo pipefail

readonly project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly notary_profile="${SIDEBOX_NOTARY_PROFILE:-notarytool}"

app_identity="${SIDEBOX_APP_SIGN_IDENTITY:-}"
installer_identity="${SIDEBOX_INSTALLER_SIGN_IDENTITY:-}"

usage() {
	cat <<'EOF'
Usage: ./scripts/package-macos.sh

Packages an existing dist/Sidebox.app, signs it with Developer ID, submits the
PKG to Apple for notarization, and staples the notarization ticket.

Environment variables:
  SIDEBOX_APP_SIGN_IDENTITY        Developer ID Application identity
  SIDEBOX_INSTALLER_SIGN_IDENTITY  Developer ID Installer identity
  SIDEBOX_NOTARY_PROFILE           notarytool Keychain profile (default: notarytool)
EOF
}

die() {
	echo "Error: $*" >&2
	exit 1
}

resolve_identity() {
	local kind="$1" matches count
	matches=$(security find-identity -v 2>/dev/null |
		sed -n "s/.*\"\($kind: [^\"]*\)\".*/\1/p" | sort -u)
	count=$(printf '%s\n' "$matches" | sed '/^$/d' | wc -l | tr -d ' ')
	[ "$count" = "1" ] || die "有効な $kind 証明書を1件に特定できません。環境変数で指定してください。"
	printf '%s' "$matches"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		-h|--help) usage; exit 0 ;;
		*) usage >&2; die "unknown option: $1" ;;
	esac
	done

[ "$(uname -s)" = "Darwin" ] || die "このスクリプトはmacOS専用です。"
for command_name in codesign ditto security pkgbuild pkgutil plutil xcrun spctl; do
	command -v "$command_name" >/dev/null || die "$command_name が見つかりません。"
done

[ -n "$app_identity" ] || app_identity=$(resolve_identity "Developer ID Application")
[ -n "$installer_identity" ] || installer_identity=$(resolve_identity "Developer ID Installer")
xcrun notarytool history --keychain-profile "$notary_profile" >/dev/null 2>&1 ||
	die "notarytoolのKeychain profile '$notary_profile' を利用できません。"

cd "$project_dir"

version=$(tr -d '[:space:]' < VERSION)
architecture=$(uname -m)
package_name="Sidebox-$version-macos-$architecture.pkg"
package_path="$project_dir/dist/Sidebox-$version-macos-$architecture.pkg"
package_temp_dir=$(mktemp -d "/tmp/sidebox-pkg.XXXXXX")
trap 'rm -rf "$package_temp_dir"' EXIT HUP INT TERM
source_app="$project_dir/dist/Sidebox.app"
package_app="$package_temp_dir/root/Applications/Sidebox.app"
component_plist="$package_temp_dir/components.plist"

[ -x "$source_app/Contents/MacOS/Sidebox" ] ||
	die "dist/Sidebox.app がありません。先に ./scripts/build-macos.sh を実行してください。"
source_version=$(plutil -extract CFBundleShortVersionString raw "$source_app/Contents/Info.plist" 2>/dev/null || true)
[ "$source_version" = "$version" ] ||
	die "dist/Sidebox.app のバージョンは $source_version です。$version をビルドし直してください。"

mkdir -p "$package_temp_dir/root/Applications"
ditto "$source_app" "$package_app"
echo "Signing app with: $app_identity"
codesign --force --timestamp --options runtime \
	--sign "$app_identity" "$package_app"
codesign --verify --deep --strict "$package_app"
codesign --display --verbose=2 "$package_app" 2>&1 |
	sed -n '/^Identifier=/p;/^TeamIdentifier=/p;/^Runtime Version=/p'

# Prevent Installer from relocating the payload to another copy of Sidebox.app.
pkgbuild --analyze --root "$package_temp_dir/root" "$component_plist"
plutil -replace '0.BundleIsRelocatable' -bool NO "$component_plist"

unsigned_package="$package_temp_dir/$package_name"
pkgbuild \
	--root "$package_temp_dir/root" \
	--component-plist "$component_plist" \
	--identifier "jp.fukuyori.sidebox.pkg" \
	--version "$version" \
	--ownership recommended \
	--install-location / \
	--sign "$installer_identity" \
	"$unsigned_package"

mkdir -p dist
mv -f "$unsigned_package" "$package_path"
pkgutil --check-signature "$package_path"

echo "Submitting to Apple notarization service..."
xcrun notarytool submit "$package_path" \
	--keychain-profile "$notary_profile" \
	--wait
xcrun stapler staple "$package_path"
xcrun stapler validate "$package_path"
spctl --assess --type install --verbose=4 "$package_path"

echo "Packaged and notarized: $package_path"
