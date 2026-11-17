#!/usr/bin/env bash
# Fails unless every file under plugin/scripts/ is executable in git
# (mode 100755), both staged and committed. Users install the plugin from a
# git clone, so the mode git records is the mode they get, and agents'
# shells run the launcher by its path. A checkout with core.fileMode=false
# (WSL on a Windows drive) shows every file as executable, so only git's own
# record can catch a lost bit. verify.sh and release-build.sh run this.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

unchecked() {
	echo "check-modes: $*, so the modes of plugin/scripts are unchecked" >&2
	exit 1
}

in_git() { git -C "$root" -c core.fsmonitor=false "$@"; }

# Both listings put the mode first and the path after a tab.
staged="$(in_git ls-files --stage -- plugin/scripts)" || unchecked "git cannot list the staged files"
[ -n "$staged" ] || unchecked "git lists no files under plugin/scripts"
committed=""
# Only a branch with no commits yet has nothing committed to check; any
# other HEAD git cannot read blocks.
if head_ref="$(in_git symbolic-ref -q HEAD)" && ! in_git show-ref --verify --quiet "$head_ref"; then
	:
else
	committed="$(in_git ls-tree -r HEAD -- plugin/scripts)" || unchecked "git cannot list the committed files"
fi

# A regular file without the bit is 100644; links and submodules have no
# execute bit to lose. grep exits 1 when there is none.
status=0
not_executable="$(printf '%s\n%s\n' "$staged" "$committed" | grep '^100644 ')" || status=$?
[ "$status" -le 1 ] || unchecked "grep failed with exit status $status"
if [ -n "$not_executable" ]; then
	printf 'check-modes: not executable in git, so installed plugins cannot run them:\n%s\n' \
		"$(printf '%s\n' "$not_executable" | cut -f2 | sort -u)" >&2
	echo "Fix with: git update-index --chmod=+x <file>, and commit it" >&2
	exit 1
fi
