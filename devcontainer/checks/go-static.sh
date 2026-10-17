#!/bin/bash
# Rung 1 of the ladder for a Go leaf (spec §9.5): formatting, vet, lint, and the docs. Run in the leaf's
# top folder. The first problem ends it, with what to fix in the output.
set -euo pipefail
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
golangci-lint run ./...
echo "== docs"
"$(dirname "$0")/docs.sh"
