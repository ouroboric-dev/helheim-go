#!/bin/bash
# Rung 5 of the ladder for a Go leaf: the tests that need the services the sandbox starts beside this
# container. It waits for Postgres (host postgres, port 5432, unless YGG_DB_HOST and YGG_DB_PORT say
# otherwise; YGG_DB_WAIT seconds, 60 by default), then runs the tests behind the integration tag. The
# database has no password: the sandbox starts it for this run alone, on a network with no route out.
set -euo pipefail
host="${YGG_DB_HOST:-postgres}" port="${YGG_DB_PORT:-5432}" wait_s="${YGG_DB_WAIT:-60}"
up=0
for ((i = 0; i < wait_s; i++)); do
  if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then up=1; break; fi
  sleep 1
done
[ "$up" -eq 1 ] || { echo "integration: Postgres at $host:$port did not answer in ${wait_s}s"; exit 1; }
export YGG_DATABASE_URL="postgres://postgres@$host:$port/app?sslmode=disable"
export YGG_S3_ENDPOINT="${YGG_S3_ENDPOINT:-http://seaweedfs:8333}"
go test -race -count=1 -tags integration ./...
