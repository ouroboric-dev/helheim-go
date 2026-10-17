#!/bin/bash
# Rung 2, the tests: every test of the leaf, with the race detector, none cached. (go-tdd.sh is the other
# half of the rung: it shows the new tests fail on the parent commit.)
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
go test -race -count=1 ./...
