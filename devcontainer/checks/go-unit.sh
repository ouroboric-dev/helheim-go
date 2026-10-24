#!/bin/bash
# Rung 2, the tests: every test of the leaf, with the race detector, none cached. (go-tdd.sh is the other
# half of the rung: it shows the new tests fail on the parent commit.)
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start unit
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
if ygg_typed; then
  rc=0
  go test -race -count=1 -json ./... >"$YGG_TMP/test.json" || rc=$?
  ygg_render_test_json "$YGG_TMP/test.json"
  ygg_tool_stage go-test-json build "go test wrote no report; see the log" "$YGG_TMP/test.json"
  exit "$rc"
fi
go test -race -count=1 ./...
