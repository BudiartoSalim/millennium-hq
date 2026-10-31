#!/usr/bin/env bash
# Builds the engine's release binaries reproducibly, for every platform the
# launcher supports, into dist/, and prints their SHA-256.
#
#   scripts/release-build.sh VERSION           build and print the hashes
#   scripts/release-build.sh VERSION --lock    also write plugin/code-qc.lock
#   scripts/release-build.sh VERSION --check   fail unless the hashes match
#                                              plugin/code-qc.lock (the release
#                                              workflow runs this)
#
# The same source and toolchain always give the same bytes: the Go version is
# pinned (GOTOOLCHAIN downloads it, verified against the Go checksum
# database, when the installed go differs), and nothing machine-specific is
# embedded. See docs/releasing.md.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

go_version="go1.26.10"
repo_url="https://github.com/BudiartoSalim/millennium-hq"
targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

version="${1:-}"
mode="${2:-}"
if ! printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "usage: scripts/release-build.sh VERSION [--lock|--check]   (VERSION like 0.1.0)" >&2
	exit 2
fi
case "$mode" in "" | --lock | --check) ;; *)
	echo "unknown option $mode" >&2
	exit 2
	;;
esac

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$root/dist"
lock="$root/plugin/code-qc.lock"
rm -rf "$dist"
mkdir -p "$dist"

export GOTOOLCHAIN="$go_version" CGO_ENABLED=0 GOFLAGS=-mod=readonly GOAMD64=v1 GOARM64=v8.0 GOEXPERIMENT=
actual_go="$(cd "$root/code-qc" && go env GOVERSION)"
if [ "$actual_go" != "$go_version" ]; then
	echo "release builds need $go_version, got $actual_go" >&2
	exit 2
fi

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

sums=""
for target in $targets; do
	goos="${target%/*}"
	goarch="${target#*/}"
	asset="millennium-code-qc-$goos-$goarch"
	if [ "$goos" = windows ]; then
		asset="$asset.exe"
	fi
	(cd "$root/code-qc" && GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false \
		-ldflags "-s -w -buildid= -X main.version=$version" -o "$dist/$asset" .)
	sums="$sums$(sha256_of "$dist/$asset") $goos-$goarch"$'\n'
done
printf '%s' "$sums" | while read -r hash platform; do
	asset="millennium-code-qc-$platform"
	case "$platform" in windows-*) asset="$asset.exe" ;; esac
	printf '%s  %s\n' "$hash" "$asset"
done >"$dist/SHA256SUMS"
cat "$dist/SHA256SUMS"

lock_text() {
	cat <<LOCK
# The engine release the launcher downloads, and the SHA-256 of each
# platform's binary. Only a reviewed change updates it, written by
# scripts/release-build.sh VERSION --lock; the release workflow rebuilds the
# tag and refuses to publish binaries whose hashes differ from these.
version $version
go $go_version
url $repo_url/releases/download/code-qc/v$version
LOCK
	printf '%s' "$sums" | while read -r hash platform; do
		printf 'sha256 %s %s\n' "$platform" "$hash"
	done
}

case "$mode" in
--lock)
	lock_text >"$lock"
	echo "wrote $lock"
	;;
--check)
	if ! diff -u "$lock" <(lock_text) >&2; then
		echo "the built binaries differ from plugin/code-qc.lock: refusing to release" >&2
		exit 2
	fi
	echo "the binaries match plugin/code-qc.lock"
	;;
esac
