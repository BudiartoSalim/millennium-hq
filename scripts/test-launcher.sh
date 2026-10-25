#!/usr/bin/env bash
# Tests plugin/scripts/millennium, the launcher, against a release served
# from a local directory: download, verification, caching, and every way it
# must refuse to run something. verify.sh runs it.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0

check() {
	local name="$1" want_code="$2" want_text="$3"
	shift 3
	local code=0
	out="$("$@" 2>&1)" || code=$?
	if [ "$code" != "$want_code" ] || { [ -n "$want_text" ] && ! printf '%s' "$out" | grep -q -- "$want_text"; }; then
		printf 'FAIL %s: exit %s, output: %s\n' "$name" "$code" "$out" >&2
		failures=$((failures + 1))
	else
		printf 'ok   %s\n' "$name"
	fi
}

sha() { sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$1" | cut -d' ' -f1; }

# A stand-in engine and its release.
mkdir -p "$work/release" "$work/plugin/scripts"
printf '#!/bin/sh\necho "fake engine: $*"\n' >"$work/release/millennium-linux-amd64"
chmod +x "$work/release/millennium-linux-amd64"
cp "$root/plugin/scripts/millennium" "$work/plugin/scripts/millennium"
launcher="$work/plugin/scripts/millennium"
write_lock() {
	printf 'version 9.9.9\nurl file://%s/release\nsha256 linux-amd64 %s\n' "$work" "$1" >"$work/plugin/engine.lock"
}
run() { env -u MILLENNIUM_ENGINE -u CLAUDE_PLUGIN_DATA MILLENNIUM_PLATFORM=linux-amd64 "$launcher" "$@"; }

write_lock "$(sha "$work/release/millennium-linux-amd64")"
check "downloads, verifies and runs the release" 0 "fake engine: gate --tier fast" run gate --tier fast
check "keeps it beside the plugin" 0 "" test -x "$work/plugin/.engine/9.9.9/millennium-linux-amd64"
rm "$work/release/millennium-linux-amd64"
check "runs the cached copy without the network" 0 "fake engine: version" run version

printf '#!/bin/sh\necho tampered\n' >"$work/plugin/.engine/9.9.9/millennium-linux-amd64"
check "refuses a changed cached copy" 2 "does not match the engine lock" run version

rm -rf "$work/plugin/.engine"
printf '#!/bin/sh\necho evil\n' >"$work/release/millennium-linux-amd64"
chmod +x "$work/release/millennium-linux-amd64"
write_lock "0000000000000000000000000000000000000000000000000000000000000000"
check "refuses a download whose hash differs" 2 "refusing to run it" run version
check "leaves nothing behind after a refused download" 1 "" sh -c "ls '$work/plugin/.engine/9.9.9/' | grep ."

check "refuses a platform the lock does not list" 2 "has no binary for darwin-arm64" env -u MILLENNIUM_ENGINE MILLENNIUM_PLATFORM=darwin-arm64 "$launcher" version

printf 'version unreleased\n' >"$work/plugin/engine.lock"
check "explains an unreleased plugin" 2 "set MILLENNIUM_ENGINE" run version
printf '#!/bin/sh\necho "own build: $*"\n' >"$work/own-engine"
chmod +x "$work/own-engine"
check "MILLENNIUM_ENGINE wins" 0 "own build: x" env MILLENNIUM_ENGINE="$work/own-engine" "$launcher" x
rm "$work/plugin/engine.lock"
check "refuses without a lock" 2 "engine lock" run version

if [ "$failures" -gt 0 ]; then
	printf '%s launcher test(s) failed\n' "$failures" >&2
	exit 1
fi
