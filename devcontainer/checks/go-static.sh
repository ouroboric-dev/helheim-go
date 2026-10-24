#!/bin/bash
# Rung 1 of the ladder for a Go leaf (spec §9.5): formatting, vet, lint, and the docs. Run in the leaf's
# top folder. The first problem ends it, with what to fix in the output.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start static
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
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
# to (a branch called main: a tag or any other ref of that name does not count), or no list, the rung fails: it
# never passes unchecked.
# git reads the variables below from the environment and they change what a pathspec means (a literal one
# turns :(glob)**/.yamllint into a name no file has), so the protected step does not trust them.
unset GIT_LITERAL_PATHSPECS GIT_GLOB_PATHSPECS GIT_NOGLOB_PATHSPECS GIT_ICASE_PATHSPECS
if ! base="$(git merge-base HEAD refs/heads/main 2>/dev/null)"; then
  ygg_stage none policy "static: there is no main branch to compare this task with, so the protected files cannot be checked"
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
  ygg_stage none policy "static: the list of protected files, protected-common, is missing or empty (looked for $list), so they cannot be checked"
  echo "static: the list of protected files, protected-common, is missing or empty (looked for $list), so they cannot be checked."
  exit 1
fi
globs+=(':(glob)**/.golangci.yml' ':(glob)**/.golangci.yaml' ':(glob)**/.golangci.toml' ':(glob)**/.golangci.json')
# Installed packages carry their own config files and no tool reads one from there for the leaf, so the glob
# entries skip node_modules, in the listing of the files git does not track only: a tracked file is a change
# wherever it is. The root-anchored entries (.github/, .devcontainer/, ...) skip nothing: whatever is in them is
# protected.
# No --diff-filter (a type change is a change), and no --exclude-standard (an ignored file is listed too).
# Each git command is checked: a failing one fails the rung.
gitfail() {
  ygg_stage none policy "static: git failed while the protected files were checked ($*), so they cannot be checked"
  echo "static: git failed while the protected files were checked ($*), so they cannot be checked."
  exit 1
}
changed=""
# The root folders go to git without their trailing slash: a pathspec with one matches a folder only, so a
# folder added as a symlink would pass. A plain folder name matches the folder and all that is in it.
if [ "${#roots[@]}" -gt 0 ]; then
  changed+="$(git diff --no-renames --name-only "$base" -- "${roots[@]%/}")" || gitfail diff
  changed+=$'\n'"$(git ls-files --others -- "${roots[@]%/}")" || gitfail ls-files
fi
# globs always has the Go names, so it is never empty.
changed+=$'\n'"$(git diff --no-renames --name-only "$base" -- "${globs[@]}")" || gitfail diff
changed+=$'\n'"$(git ls-files --others -- "${globs[@]}" ':(exclude,glob)**/node_modules/**')" || gitfail ls-files
changed="$(printf '%s\n' "$changed" | sed '/^$/d' | sort -u)"
if [ -n "$changed" ]; then
  pairs=()
  while IFS= read -r f; do pairs+=(policy "$f configures the checks, it is protected, and this task changed it: put it back as it was; if the check is wrong, say so in your report"); done <<<"$changed"
  ygg_stage none "${pairs[@]}"
  echo "static: these files configure the checks, they are protected, and this task changed them:"
  printf '%s\n' "$changed" | sed 's/^/  /'
  echo "Put them back as they were; if the check is wrong, say so in your report."
  exit 1
fi
echo "== gofmt"
unformatted="$(gofmt -l . || true)"
if [ -n "$unformatted" ]; then
  pairs=()
  while IFS= read -r f; do pairs+=(lint "$f is not gofmt-clean: run gofmt -w on it"); done <<<"$unformatted"
  ygg_stage none "${pairs[@]}"
  echo "These files are not gofmt-clean; run gofmt -w on them:"
  echo "$unformatted"
  exit 1
fi
echo "== go vet"
# The machine mode of go vet exits 0 with findings and writes them to stdout; the text mode keeps the exit code and the
# log. A package that does not type-check has no finding in the JSON, only the message on stderr: then the message is
# the finding. Each run is converted on its own, as the first one that fails ends the rung.
vet_stage() {
  if [ -s "$2" ] && ! grep -q '"message"' "$1"; then
    ygg_stage none build "$(head -c 3500 "$2")"
  else
    ygg_tool_stage go-vet-json build "go vet wrote no report; see the log" "$1"
  fi
}
if ygg_typed; then
  go vet -json ./... >"$YGG_TMP/vet.json" 2>"$YGG_TMP/vet.err" || true
  go vet -json -tags integration ./... >"$YGG_TMP/vet-integration.json" 2>"$YGG_TMP/vet-integration.err" || true
  vet_stage "$YGG_TMP/vet.json" "$YGG_TMP/vet.err"
fi
go vet ./...
! ygg_typed || vet_stage "$YGG_TMP/vet-integration.json" "$YGG_TMP/vet-integration.err"
go vet -tags integration ./...
echo "== golangci-lint"
# The config is the baked one plus a skip for each file verified as sqlc's own output (byte for byte what
# sqlc generates from the base commit's sqlc.yaml). Any other file is linted, whatever its header says.
tmp="$YGG_TMP"
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
lint=(golangci-lint run --config "$conf")
if ygg_typed; then
  lint+=(--output.json.path "$YGG_TMP/golangci.json")
fi
# The report is written when the run ends; a run that failed before it (a config it cannot read) has none, and the
# stage then falls back to the one message.
"${lint[@]}" ./... || {
  rc=$?
  ygg_tool_stage golangci-json build "golangci-lint failed (exit $rc) and wrote no report; see the log" "$YGG_TMP/golangci.json"
  exit "$rc"
}
echo "== docs"
rc=0
"$(dirname "$0")/docs.sh" >"$YGG_TMP/docs.txt" 2>&1 || rc=$?
cat "$YGG_TMP/docs.txt"
if [ "$rc" -ne 0 ]; then
  # One finding for each problem docs.sh names: its lines but for the count it ends with.
  pairs=()
  while IFS= read -r line; do
    [[ -z "$line" || "$line" =~ ^docs:\ [0-9]+\ problem ]] || pairs+=(policy "$line")
  done <"$YGG_TMP/docs.txt"
  [ "${#pairs[@]}" -gt 0 ] || pairs=(policy "the docs check failed with exit $rc")
  ygg_stage none "${pairs[@]}"
  exit "$rc"
fi
