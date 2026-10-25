#!/usr/bin/env bash
# Full local check for this repository: gofmt, go vet for every release target
# OS, the engine's test suite, and the engine auditing its own source. Run it
# before calling a change done.
# Written for the bash 3.2 that macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root/engine"

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
