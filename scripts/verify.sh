#!/usr/bin/env bash
# Full local check for this repository: gofmt, go vet for every release target
# OS, the engine's test suite, and the engine auditing its own source. Run it
# before calling a change done.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root/engine"

# The TypeScript analyzer's tests skip without their fixture, and a skip is
# not a pass: refuse to start rather than report an incomplete run as done.
if [ -z "${MILLENNIUM_TYPESCRIPT:-}" ] || [ ! -f "$MILLENNIUM_TYPESCRIPT/node_modules/typescript/package.json" ]; then
	printf 'verify: MILLENNIUM_TYPESCRIPT must point at the TypeScript test fixture.\n' >&2
	# shellcheck disable=SC2016 # the command is printed for the user to copy, unexpanded
	printf 'Set it up once with: eval "$(bash scripts/setup-fixtures.sh)"\n' >&2
	exit 1
fi

echo "== gofmt"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
	printf 'not gofmt-formatted (run: gofmt -w engine):\n%s\n' "$unformatted" >&2
	exit 1
fi

echo "== go vet"
for target_os in linux darwin windows; do
	GOOS="$target_os" go vet ./...
done

echo "== go test"
go test -count=1 ./...

# The plugin practises what it enforces: the engine audits its own source
# against .millennium/config.json, so a rule it would block in a user's
# repository fails this repository too.
echo "== self-audit"
bin_dir="$(mktemp -d)"
trap 'rm -rf "$bin_dir"' EXIT
go build -o "$bin_dir/millennium" .
(cd "$root" && "$bin_dir/millennium" audit)

echo "verify: all checks passed"
