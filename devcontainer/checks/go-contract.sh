#!/bin/bash
# Rung 3 of the ladder for a Go leaf (spec §9.5): the contract. openapi.yaml is linted, the code sqlc
# generates is what the SQL says, and the breaking changes of openapi.yaml against the commit the task
# started from are listed. A breaking change is not refused here: the consumers must follow it, and the
# task says so (it decides the pointer-contract approval point later). Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
[ -f openapi.yaml ] || { echo "contract: this leaf has no openapi.yaml, and it owns the contract of its API"; exit 1; }
echo "== openapi lint"
vacuum lint --no-style --details --fail-severity error openapi.yaml
if [ -f sqlc.yaml ]; then
  echo "== generated code"
  sqlc diff || { echo "contract: the code in internal/store is not what the SQL says; run sqlc generate"; exit 1; }
fi
echo "== breaking changes since the task started"
base="$(git merge-base HEAD main)" || { echo "contract: this leaf has no main to compare with"; exit 1; }
if git cat-file -e "$base:openapi.yaml" 2>/dev/null; then
  old="$(mktemp)"
  trap 'rm -f -- "$old"' EXIT
  git show "$base:openapi.yaml" > "$old"
  if [ "$(oasdiff breaking "$old" openapi.yaml --format json)" = "[]" ]; then
    echo "none"
  else
    echo "BREAKING: consumers of this API must follow these changes; say so in your summary."
    oasdiff breaking "$old" openapi.yaml
  fi
else
  echo "openapi.yaml is new in this change"
fi
