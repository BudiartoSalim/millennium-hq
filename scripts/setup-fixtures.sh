#!/usr/bin/env bash
# Installs the test-only fixtures the engine's tests need, outside the
# repository, and prints the environment line to use them. Today that is the
# TypeScript compiler the TypeScript analyzer parses with (pinned by
# scripts/fixtures/typescript/package-lock.json, integrity-checked by npm ci,
# install scripts disabled).
#
#   eval "$(bash scripts/setup-fixtures.sh)"
#
# The fixtures go to $MILLENNIUM_FIXTURES, default ${XDG_CACHE_HOME:-~/.cache}/millennium-fixtures.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest="${MILLENNIUM_FIXTURES:-${XDG_CACHE_HOME:-$HOME/.cache}/millennium-fixtures}/typescript"

mkdir -p "$dest"
cp "$root/scripts/fixtures/typescript/package.json" \
	"$root/scripts/fixtures/typescript/package-lock.json" \
	"$root/scripts/fixtures/typescript/.npmrc" "$dest/"
(cd "$dest" && npm ci --ignore-scripts --no-audit --no-fund >&2)
printf 'export MILLENNIUM_TYPESCRIPT=%q\n' "$dest"
