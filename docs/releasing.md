# Releasing the engine

The plugin runs the engine binary named in `plugin/engine.lock`. A release
puts the hashes in a reviewed change first and publishes the binaries second,
so a published binary can only be one built from reviewed source.

## How it fits together

- `scripts/release-build.sh VERSION` builds the engine for linux, darwin and
  windows on amd64 and arm64, reproducibly: `CGO_ENABLED=0`, `-trimpath`, no
  VCS stamp or build id, and a pinned Go version (`go1.26.10`; `GOTOOLCHAIN`
  fetches it, verified against the Go checksum database, if your installed Go
  differs). The same source gives the same bytes on any machine.
- `plugin/engine.lock` records the version, the Go version, the download URL
  and each platform's SHA-256.
- `plugin/scripts/millennium`, the launcher, downloads the binary for the
  user's platform on first use, refuses it unless its SHA-256 matches the
  lock, and checks the cached copy again on every run.
- `.github/workflows/release-engine.yml` runs on a pushed `engine/v*` tag. It
  rebuilds the tag, runs the engine's tests, and publishes the binaries only
  if their hashes match the lock committed at that tag. It uses no
  third-party actions: git clones the tag, and Go comes from the official
  tarball checked against a pinned SHA-256. The publishing job alone may
  write; the rest read.

## Steps

1. On a branch, with the code to release merged:

   ```
   bash scripts/verify.sh
   bash scripts/release-build.sh 0.1.0 --lock
   ```

   The second command writes `plugin/engine.lock`. Bump `version` in
   `plugin/.claude-plugin/plugin.json` too, so users' plugins update to the
   release that names this engine.

2. Open a pull request with the lock and the version bump. The reviewer can
   rebuild and compare: `bash scripts/release-build.sh 0.1.0 --check`.

3. After the merge, tag the merge commit and push the tag:

   ```
   git tag engine/v0.1.0 <merge commit>
   git push origin engine/v0.1.0
   ```

4. The workflow publishes the release `engine/v0.1.0` with the six binaries
   and `SHA256SUMS`. If any hash differs from the lock, it fails and
   publishes nothing; find out why before retrying (a different Go version,
   or a change between the lock and the tag).

## Repository settings

- Protect tags matching `engine/v*` (Settings, Rules, tag ruleset) so only
  maintainers can create or move them.
- Keep the default workflow permissions read-only; the workflow asks for
  `contents: write` in its publishing job only.

## When the Go version changes

Update `go_version` in `scripts/release-build.sh`, and `GO_VERSION` and
`GO_SHA256` in the workflow, from `https://go.dev/dl/?mode=json&include=all`
(the `sha256` of `<version>.linux-amd64.tar.gz`). Check the hash against
`https://dl.google.com/go/<version>.linux-amd64.tar.gz.sha256` as well.
