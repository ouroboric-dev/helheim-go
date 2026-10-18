#!/bin/bash
# Rung 4 of the ladder for a Go leaf, first half (spec §9.5, §9.6; v0-T257): the dependency policy. Every direct
# dependency is a tagged release, nothing is replaced, the packages the policy names are at or above their
# minimum patched version, and the audit tools the policy names find nothing. The policy is
# config/deps-policy.json, baked beside this script where the agent cannot write; a policy file in the leaf
# is never read. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
here="$(cd "$(dirname "$0")" && pwd)"
policy="$here/config/deps-policy.json"
echo "== dependency policy"
GOFLAGS=-mod=mod go run "$here/lib/godeps.go" check "$policy"
# The module cache is kept between runs and the agent can write it (see go-security.sh).
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
for tool in $(GOFLAGS=-mod=mod go run "$here/lib/godeps.go" audit "$policy"); do
  case "$tool" in
    govulncheck)
      echo "== govulncheck"
      govulncheck ./...
      govulncheck -tags integration ./...
      ;;
    osv)
      echo "== OSV"
      osv-scanner scan source -L go.mod
      ;;
  esac
done
