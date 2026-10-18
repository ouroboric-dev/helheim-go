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
# The dev container's files are what a person's editor runs, so a change to them is refused, against the
# commit the task started from (main). This includes a file not added to git yet. The linters read no
# configuration of the leaf: golangci-lint gets the config baked beside this script (below), and sqlc is
# read from the commit the task started from (sqlc-owned.sh), so a config swapped, hidden or edited in the
# leaf changes nothing.
if base="$(git merge-base HEAD main 2>/dev/null)"; then
  protected=('.devcontainer/')
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
# The config is the baked one plus a skip for each file verified as sqlc's own output (byte for byte what
# sqlc generates from the base commit's sqlc.yaml). Any other file is linted, whatever its header says.
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
conf="$tmp/golangci.yml"
cp -- "$(dirname "$0")/config/golangci.yml" "$conf"
owned="$("$(dirname "$0")/sqlc-owned.sh")"
if [ -n "$owned" ]; then
  echo "    paths:" >>"$conf"
  while IFS= read -r f; do
    # a regex for the whole relative path, in a single-quoted YAML string
    esc="$(printf '%s' "$f" | sed -e 's/[][\\.*^$+?(){}|]/\\&/g' -e "s/'/''/g")"
    printf "      - '^%s\$'\n" "$esc" >>"$conf"
  done <<<"$owned"
fi
golangci-lint run --config "$conf" ./...
echo "== docs"
"$(dirname "$0")/docs.sh"
