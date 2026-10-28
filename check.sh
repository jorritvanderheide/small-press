#!/usr/bin/env bash
# The Go checks, as CI runs them (.forgejo/workflows/check.yml)
# and as you run them before calling a change done: ./check.sh for all of
# them (in the dev shell: nix develop -c ./check.sh), or one by name. The
# tools' versions are pinned here, so a new release can't fail a build that
# passed.
set -euo pipefail
cd "$(dirname "$0")"

tests() {
  unformatted="$(gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "not formatted (gofmt -w):"
    echo "$unformatted"
    return 1
  fi
  go vet ./...
  go test ./...
}
staticcheck() { go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...; }
govulncheck() { go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...; }

case "${1:-all}" in
  tests | staticcheck | govulncheck) "$1" ;;
  all) tests && staticcheck && govulncheck && echo "all checks pass" ;;
  *)
    echo "usage: $0 [tests|staticcheck|govulncheck]" >&2
    exit 2
    ;;
esac
