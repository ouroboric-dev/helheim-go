#!/bin/bash
# The TDD check, the other half of rung 2 (spec §9.5): the tests a change adds or changes MUST fail on the
# commit it started from. It builds that commit in a folder of its own, puts the change's test files on it,
# and runs their packages: they have to fail (a test of code that is not there yet fails to compile, which
# counts). A change to Go code without a new or changed test is refused. Tests behind the integration tag
# need a database and cannot be run here; a change that only has those is passed with a note, and the
# reviewers look at it. Generated files are not code that needs a test. Run in the leaf's top folder, on the
# run branch; main is where the task started from.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
base="$(git merge-base HEAD main)" || { echo "tdd: this leaf has no main to compare with"; exit 1; }
generated() { head -n 5 "$1" | grep -q 'Code generated'; }
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

if [ ${#tests[@]} -eq 0 ]; then
  if [ ${#code[@]} -gt 0 ] && [ ${#skipped[@]} -eq 0 ]; then
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

parent="$(mktemp -d)"
trap 'rm -rf -- "$parent"' EXIT
git archive "$base" | tar -x -C "$parent"
for f in "${tests[@]}"; do
  mkdir -p "$parent/$(dirname "$f")"
  cp -- "$f" "$parent/$f"
done
pkgs="$(printf '%s\n' "${tests[@]}" | xargs -n1 dirname | sort -u | sed 's|^\([^.]\)|./\1|; s|^\.$|./.|')"
log="$parent/.tdd.log"
# shellcheck disable=SC2086 # the packages are words
if (cd "$parent" && go test -count=1 $pkgs) >"$log" 2>&1; then
  echo "tdd: the new tests pass on the commit the task started from, so they test nothing new:"
  printf '  %s\n' "${tests[@]}"
  echo "A test of new behaviour fails before the code for it exists. Change the tests so they do."
  exit 1
fi
echo "tdd: the new tests fail on the commit the task started from, as they must:"
printf '  %s\n' "${tests[@]}"
tail -n 12 "$log"
