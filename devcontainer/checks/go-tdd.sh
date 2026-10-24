#!/bin/bash
# The TDD check, the other half of rung 2 (spec §9.5): the tests a change adds or changes MUST fail on the
# commit it started from. It builds that commit in a folder of its own, puts the change's test files on it,
# and runs their packages: they have to fail (a test of code that is not there yet fails to compile, which
# counts). A change to Go code without a new or changed test is refused. Tests behind the integration tag
# need a database and cannot be run here; a change that only has those is passed with a note, and the
# reviewers look at it. Only what sqlc generates is not code that needs a test (a "Code generated" header or a
# folder proves nothing, the agent can write both): a file counts when it is byte for byte what sqlc writes
# from sqlc.yaml as it was when the task started (sqlc-owned.sh), which go-contract.sh checks with
# `sqlc diff`. The SQL it reads (queries and migrations) is code that needs a test. Run in the leaf's top
# folder, on the run branch; main is where the task started from.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start tdd
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
base="$(git merge-base HEAD refs/heads/main)" || {
  ygg_stage none policy "tdd: this leaf has no main to compare with"
  echo "tdd: this leaf has no main to compare with"
  exit 1
}
# sqlc.yaml is read as it was at the base: the agent must not widen the exemption by editing it.
sqlc_value() { { git show "$base:sqlc.yaml" 2>/dev/null || true; } | sed -n "s/^ *$1: *[\"']\{0,1\}\([^\"' #]*\).*/\1/p" | sed 's|/*$||'; }
queries_dir="$(sqlc_value queries | head -n 1)" schema_dir="$(sqlc_value schema | head -n 1)"
# generated: a file that is byte for byte what sqlc generates from that sqlc.yaml (sqlc-owned.sh runs it).
owned="$("$(dirname "$0")/sqlc-owned.sh")"
generated() { [ -n "$owned" ] && grep -qxF -- "$1" <<<"$owned"; }
integration() { head -n 5 "$1" | grep -q '^//go:build .*integration'; }

tests=() code=() skipped=()
while IFS= read -r f; do
  [ -f "$f" ] || continue
  if integration "$f"; then skipped+=("$f"); else tests+=("$f"); fi
done < <(git diff --no-renames --name-only --diff-filter=d "$base" HEAD -- '*_test.go')
while IFS= read -r f; do
  [ -f "$f" ] || continue
  generated "$f" || code+=("$f")
done < <(git diff --no-renames --name-only --diff-filter=d "$base" HEAD -- '*.go' ':!*_test.go')
# the SQL sqlc reads is code too
for dir in "$queries_dir" "$schema_dir"; do
  [ -n "$dir" ] || continue
  while IFS= read -r f; do
    [ -f "$f" ] && code+=("$f")
  done < <(git diff --no-renames --name-only --diff-filter=d "$base" HEAD -- "$dir/*.sql")
done

if [ ${#tests[@]} -eq 0 ]; then
  if [ ${#code[@]} -gt 0 ] && [ ${#skipped[@]} -eq 0 ]; then
    pairs=()
    for f in "${code[@]}"; do pairs+=(policy "$f changed without a new or changed test: write the test first, see it fail, then write the code"); done
    ygg_stage none "${pairs[@]}"
    echo "tdd: Go code changed without a new or changed test:"
    printf '  %s\n' "${code[@]}"
    echo "Write the test first, see it fail, then write the code."
    exit 1
  fi
  if [ ${#skipped[@]} -gt 0 ]; then
    echo "tdd: only tests behind the integration tag changed, and they need a database; they cannot be shown to fail here:"
    printf '  %s\n' "${skipped[@]}"
  else
    echo "tdd: no Go code changed"
  fi
  exit 0
fi

parent="$(mktemp -d "$YGG_TMP/parent.XXXXXX")"
git archive "$base" | tar -x -C "$parent"
for f in "${tests[@]}"; do
  mkdir -p "$parent/$(dirname "$f")"
  cp -- "$f" "$parent/$f"
done
pkgs="$(printf '%s\n' "${tests[@]}" | xargs -n1 dirname | sort -u | sed 's|^\([^.]\)|./\1|; s|^\.$|./.|')"
log="$parent/.tdd.log"
# shellcheck disable=SC2086 # the packages are words
if (cd "$parent" && go test -count=1 $pkgs) >"$log" 2>&1; then
  pairs=()
  for f in "${tests[@]}"; do pairs+=(policy "$f passes on the commit the task started from, so it tests nothing new: a test of new behaviour fails before the code for it exists"); done
  ygg_stage none "${pairs[@]}"
  echo "tdd: the new tests pass on the commit the task started from, so they test nothing new:"
  printf '  %s\n' "${tests[@]}"
  echo "A test of new behaviour fails before the code for it exists. Change the tests so they do."
  exit 1
fi
echo "tdd: the new tests fail on the commit the task started from, as they must:"
printf '  %s\n' "${tests[@]}"
tail -n 12 "$log"
