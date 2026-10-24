#!/bin/bash
# Rung 5 of the ladder for a Go leaf: the tests that need the services the sandbox starts beside this
# container. It waits for Postgres (host postgres, port 5432, unless YGG_DB_HOST and YGG_DB_PORT say
# otherwise; YGG_DB_WAIT seconds, 60 by default), then runs the tests behind the integration tag. The
# database has no password: the sandbox starts it for this run alone, on a network with no route out.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source-path=SCRIPTDIR source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start integration
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
host="${YGG_DB_HOST:-postgres}" port="${YGG_DB_PORT:-5432}" wait_s="${YGG_DB_WAIT:-60}"
up=0
for ((i = 0; i < wait_s; i++)); do
  if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then up=1; break; fi
  sleep 1
done
[ "$up" -eq 1 ] || {
  ygg_stage none build "integration: Postgres at $host:$port did not answer in ${wait_s}s"
  echo "integration: Postgres at $host:$port did not answer in ${wait_s}s"
  exit 1
}
export YGG_DATABASE_URL="postgres://postgres@$host:$port/app?sslmode=disable"
export YGG_S3_ENDPOINT="${YGG_S3_ENDPOINT:-http://s3:8333}"
if ygg_typed; then
  rc=0
  go test -race -count=1 -tags integration -json ./... >"$YGG_TMP/test.json" || rc=$?
  ygg_render_test_json "$YGG_TMP/test.json"
  ygg_tool_stage go-test-json build "go test wrote no report; see the log" "$YGG_TMP/test.json"
  exit "$rc"
fi
go test -race -count=1 -tags integration ./...
