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
# The files that configure the checks of a product are protected: the dev container, the CI, the lint, licence
# and base-CI configuration, and the Go lint configuration. The list is protected-common beside this script
# (the same file in every build realm) plus the Go names below. A task's change to one is refused, against the
# commit the task started from (main): added, changed, deleted, moved, swapped for a symlink (a type change),
# not added to git yet, or hidden by .gitignore. The linters read no configuration of the leaf: golangci-lint
# gets the config baked beside this script (below), and sqlc is read from the commit the task started from
# (sqlc-owned.sh), so a config swapped, hidden or edited in the leaf changes nothing. With no main to compare
# to, or no list, the rung fails: it never passes unchecked.
if ! base="$(git merge-base HEAD main 2>/dev/null)"; then
  echo "static: there is no main branch to compare this task with, so the protected files cannot be checked."
  echo "Make the commit the task started from a branch called main."
  exit 1
fi
list="$(dirname "${BASH_SOURCE[0]}")/protected-common"
roots=()
globs=()
if [ -r "$list" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    case "$line" in
      '' | '#'*) ;;
      ':(glob)'*) globs+=("$line") ;;
      *) roots+=("$line") ;;
    esac
  done <"$list"
fi
if [ "$((${#roots[@]} + ${#globs[@]}))" -eq 0 ]; then
  echo "static: the list of protected files, protected-common, is missing or empty (looked for $list), so they cannot be checked."
  exit 1
fi
globs+=(':(glob)**/.golangci.yml' ':(glob)**/.golangci.yaml' ':(glob)**/.golangci.toml' ':(glob)**/.golangci.json')
# Installed packages carry their own config files and no tool reads one from there for the leaf, so the glob
# entries skip node_modules. The root-anchored entries (.github/, .devcontainer/, ...) do not: whatever is in
# them is protected.
globs+=(':(exclude,glob)**/node_modules/**')
# No --diff-filter (a type change is a change), and no --exclude-standard (an ignored file is listed too).
# Each git command is checked: a failing one fails the rung.
gitfail() {
  echo "static: git failed while the protected files were checked ($*), so they cannot be checked."
  exit 1
}
changed=""
if [ "${#roots[@]}" -gt 0 ]; then
  changed+="$(git diff --no-renames --name-only "$base" -- "${roots[@]}")" || gitfail diff
  changed+=$'\n'"$(git ls-files --others -- "${roots[@]}")" || gitfail ls-files
fi
if [ "${#globs[@]}" -gt 1 ]; then
  changed+=$'\n'"$(git diff --no-renames --name-only "$base" -- "${globs[@]}")" || gitfail diff
  changed+=$'\n'"$(git ls-files --others -- "${globs[@]}")" || gitfail ls-files
fi
changed="$(printf '%s\n' "$changed" | sed '/^$/d' | sort -u)"
if [ -n "$changed" ]; then
  echo "static: these files configure the checks, they are protected, and this task changed them:"
  printf '  %s\n' "$changed"
  echo "Put them back as they were; if the check is wrong, say so in your report."
  exit 1
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
