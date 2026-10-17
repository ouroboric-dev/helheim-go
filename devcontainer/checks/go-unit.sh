#!/bin/bash
# Rung 2, the tests: every test of the leaf, with the race detector, none cached. (go-tdd.sh is the other
# half of the rung: it shows the new tests fail on the parent commit.)
set -euo pipefail
go test -race -count=1 ./...
