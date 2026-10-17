#!/bin/bash
# Rung 1 of the ladder for a Go leaf (spec §9.5): formatting, vet, lint, and the docs. Run in the leaf's
# top folder. The first problem ends it, with what to fix in the output.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
echo "== protected paths"
# The linters read configuration the agent could edit, and sqlc.yaml says what go-contract.sh and go-tdd.sh
# check. Any change, addition or deletion of it, against the commit the task started from (main), turns a rung
# green by itself (a .golangci.json beside .golangci.yml wins over it), so it is refused. This includes a file
# not added to git yet.
if base="$(git merge-base HEAD main 2>/dev/null)"; then
  protected=('.golangci.*' 'sqlc.*' '.devcontainer/')
  changed="$({ git diff --no-renames --name-only --diff-filter=AMD "$base" -- "${protected[@]}"
    git ls-files --others --exclude-standard -- "${protected[@]}"; } | sort -u)"
  if [ -n "$changed" ]; then
    echo "static: these files configure the checks, they are protected, and this task changed them:"
    printf '  %s\n' "$changed"
    echo "Put them back as they were; if the check is wrong, say so in your report."
    exit 1
  fi
fi
echo "== gofmt"
unformatted="$(gofmt -l . || true)"
if [ -n "$unformatted" ]; then
  echo "These files are not gofmt-clean; run gofmt -w on them:"
  echo "$unformatted"
  exit 1
fi
echo "== go vet"
go vet ./...
go vet -tags integration ./...
echo "== golangci-lint"
golangci-lint run ./...
echo "== docs"
"$(dirname "$0")/docs.sh"
