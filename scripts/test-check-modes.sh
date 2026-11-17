#!/usr/bin/env bash
# Tests scripts/check-modes.sh against throwaway repositories: it passes only
# when git records every file under plugin/scripts/ as executable, staged and
# committed, and blocks when it cannot tell. verify.sh runs it.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CEILING_DIRECTORIES="$work"

# check NAME EXIT WANT REJECT DIR [VAR=value ...]: runs DIR's copy of the
# script; its output must contain WANT and must not contain REJECT.
check() {
	local name="$1" want_code="$2" want_text="$3" reject_text="$4" dir="$5"
	shift 5
	local code=0
	out="$(env "$@" bash "$dir/scripts/check-modes.sh" 2>&1)" || code=$?
	if [ "$code" != "$want_code" ] ||
		{ [ -n "$want_text" ] && ! printf '%s' "$out" | grep -q -- "$want_text"; } ||
		{ [ -n "$reject_text" ] && printf '%s' "$out" | grep -q -- "$reject_text"; }; then
		printf 'FAIL %s: exit %s, output: %s\n' "$name" "$code" "$out" >&2
		failures=$((failures + 1))
	else
		printf 'ok   %s\n' "$name"
	fi
}

repo="$work/repo"
mkdir -p "$repo/scripts" "$repo/plugin/scripts"
cp "$root/scripts/check-modes.sh" "$repo/scripts/"
git -C "$repo" init --quiet
check "refuses a repository with no plugin scripts" 1 "lists no files" "" "$repo"

printf '#!/bin/sh\n' >"$repo/plugin/scripts/launcher"
printf '#!/bin/sh\n' >"$repo/plugin/scripts/other tool"
git -C "$repo" add --chmod=+x -- plugin/scripts/launcher "plugin/scripts/other tool"
# A link, made in the index alone: Git Bash's ln -s copies instead.
link="$(printf 'launcher' | git -C "$repo" hash-object -w --stdin)"
git -C "$repo" update-index --add --cacheinfo "120000,$link,plugin/scripts/alias"
check "passes staged executables before the first commit, and a link" 0 "" "" "$repo"

git -C "$repo" update-index --chmod=-x -- "plugin/scripts/other tool"
check "names a staged script that is not executable, and only it" 1 "^plugin/scripts/other tool$" "plugin/scripts/launcher" "$repo"
check "says how to fix it" 1 "git update-index --chmod=+x" "" "$repo"

git -C "$repo" -c user.name=t -c user.email=t@example.com commit --quiet -m init
git -C "$repo" update-index --chmod=+x -- "plugin/scripts/other tool"
check "names a script committed without the bit, even when staged with it" 1 "^plugin/scripts/other tool$" "plugin/scripts/launcher" "$repo"

git -C "$repo" -c user.name=t -c user.email=t@example.com commit --quiet -m fix
check "passes once the bit is committed" 0 "" "" "$repo"

cp "$repo/.git/HEAD" "$work/HEAD"
printf '0000000000000000000000000000000000000001\n' >"$repo/.git/HEAD"
check "blocks when HEAD names no commit" 1 "git cannot list the committed files" "" "$repo"
cp "$work/HEAD" "$repo/.git/HEAD"

mkdir -p "$work/bin"
printf '#!/bin/sh\nexit 2\n' >"$work/bin/grep"
chmod +x "$work/bin/grep"
check "blocks when grep fails" 1 "grep failed with exit status 2" "" "$repo" PATH="$work/bin:$PATH"

mkdir -p "$work/plain/scripts"
cp "$root/scripts/check-modes.sh" "$work/plain/scripts/"
check "blocks outside a git checkout" 1 "git cannot list the staged files" "" "$work/plain"

if [ "$failures" -gt 0 ]; then
	printf '%s check-modes test(s) failed\n' "$failures" >&2
	exit 1
fi
