#!/bin/bash
# Rung 4 of the ladder for a Go leaf, the two-tenant authorisation harness (spec §9.5; v0-T260): every route of
# openapi.yaml that has a path variable (every path variable is a resource id) has a test TestCrossTenant<OperationId> that passes, in which user B asks
# for user A's resource and gets 403 or 404 while user A gets it (internal/authztest.Check prints both answers). The
# only exceptions are the routes authz.json names as public, each with a reason, and they are listed here. The
# reader of the contract is ygg-authz, which the image builds into /usr/local/bin. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source-path=SCRIPTDIR source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start authz
# authz_problems turns what ygg-authz printed into one policy finding for each problem line: the lines that start
# with "authz:" or "missing", not the count it ends with and not the indented test output after it.
authz_problems() {
  local line
  local -a pairs=()
  while IFS= read -r line; do
    [[ "$line" =~ ^authz:\ [0-9]+\ problem ]] && continue
    [[ "$line" =~ ^(authz:|missing) ]] && pairs+=(policy "$line")
  done <<<"$1"
  [ "${#pairs[@]}" -gt 0 ] || pairs=(policy "${1:-ygg-authz failed}")
  ygg_stage none "${pairs[@]}"
}
if [ ! -f openapi.yaml ]; then
  ygg_stage none policy "authz: this leaf has no openapi.yaml, and it owns the contract of its API"
  echo "authz: this leaf has no openapi.yaml, and it owns the contract of its API"
  exit 1
fi
args=(-spec openapi.yaml)
[ ! -f authz.json ] || args+=(-registry authz.json)
echo "== routes with a resource id"
plan="$(ygg-authz plan "${args[@]}")" || {
  authz_problems "$plan"
  echo "$plan"
  exit 1
}
echo "$plan"
grep -q '^require ' <<<"$plan" || { echo "authz: no route takes a resource id"; exit 0; }
# The module cache is kept between runs and the agent can write it (see go-security.sh).
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
scratch="$YGG_TMP"
echo "== cross-tenant tests"
# A failing test or a leaf that does not build is reported by ygg-authz from the JSON; stderr is shown on failure.
go test -count=1 -json -run '^TestCrossTenant' ./... >"$scratch/tests.json" 2>"$scratch/stderr" || true
if ! ygg-authz check "${args[@]}" -tests "$scratch/tests.json" >"$scratch/authz.txt"; then
  cat "$scratch/authz.txt"
  cat "$scratch/stderr"
  # A test that failed, or a leaf that did not build, is a finding of go test, with its output; any other problem
  # (a route with no test, a user B who got the resource) is the rule's own.
  if grep -q '"Action":"fail"' "$scratch/tests.json"; then
    ygg_stage go-test-json "$scratch/tests.json"
  else
    authz_problems "$(cat "$scratch/authz.txt")"
  fi
  exit 1
fi
cat "$scratch/authz.txt"
