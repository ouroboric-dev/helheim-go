#!/bin/bash
# Rung 4 of the ladder for a Go leaf (spec §9.5, §9.6): known vulnerabilities, in the code that is called
# (govulncheck) and in the modules that are required (OSV). Secrets are scanned by Odin before every
# checkpoint, and the product's own security tests run with the unit tests. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start security
# The module cache is kept between runs and the agent can write it. A dependency edited there (an init that
# exits 0 makes a failing test report ok) is caught: the cache must match go.sum, which the diff shows.
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
echo "== govulncheck"
# The machine mode exits 0 with findings; the text mode keeps the exit code and the log.
if ygg_typed; then
  govulncheck -json ./... >"$YGG_TMP/vuln.json" 2>/dev/null || true
  govulncheck -json -tags integration ./... >"$YGG_TMP/vuln-integration.json" 2>/dev/null || true
  ygg_tool_stage govulncheck-json build "govulncheck wrote no report; see the log" "$YGG_TMP/vuln.json"
fi
govulncheck ./...
! ygg_typed || ygg_tool_stage govulncheck-json build "govulncheck wrote no report; see the log" "$YGG_TMP/vuln-integration.json"
govulncheck -tags integration ./...
echo "== OSV"
if ygg_typed; then
  osv-scanner scan source -L go.mod --format json >"$YGG_TMP/osv.json" 2>/dev/null || true
  ygg_tool_stage osv-json build "osv-scanner wrote no report; see the log" "$YGG_TMP/osv.json"
fi
osv-scanner scan source -L go.mod
