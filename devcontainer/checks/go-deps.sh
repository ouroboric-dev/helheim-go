#!/bin/bash
# Rung 4 of the ladder for a Go leaf, first half (spec §9.5, §9.6; v0-T257): the dependency policy. Every direct
# dependency is a tagged release, nothing is replaced, the packages the policy names are at or above their
# minimum patched version, and the audit tools the policy names find nothing. The policy is
# config/deps-policy.json, baked beside this script where the agent cannot write; a policy file in the leaf
# is never read. The checker is ygg-deps, which the image builds into /usr/local/bin. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start deps
here="$(cd "$(dirname "$0")" && pwd)"
policy="$here/config/deps-policy.json"
echo "== dependency policy"
rc=0
ygg-deps check "$policy" >"$YGG_TMP/policy.txt" || rc=$?
cat "$YGG_TMP/policy.txt"
if [ "$rc" -ne 0 ]; then
  # One finding for each line ygg-deps wrote about the dependencies; the count it ends with is not one.
  pairs=()
  while IFS= read -r line; do
    [[ "$line" =~ ^deps\ policy:\ [0-9]+\ problem ]] || pairs+=(policy "$line")
  done <"$YGG_TMP/policy.txt"
  [ "${#pairs[@]}" -gt 0 ] || pairs=(policy "the dependency policy check failed with exit $rc")
  ygg_stage none "${pairs[@]}"
  exit "$rc"
fi
# The module cache is kept between runs and the agent can write it (see go-security.sh).
{ go mod download && go mod verify; } >/dev/null 2>&1 || {
  ygg_stage none policy "the module cache does not match go.sum, a dependency in it was changed"
  echo "the module cache does not match go.sum, a dependency in it was changed:"
  go mod verify 2>&1 || go mod download 2>&1 || true
  exit 1
}
tools="$(ygg-deps audit "$policy")" || {
  rc=$?
  ygg_stage none policy "the audit tools the dependency policy names could not be read (ygg-deps audit exited $rc)"
  exit "$rc"
}
for tool in $tools; do
  case "$tool" in
    govulncheck)
      echo "== govulncheck"
      if ygg_typed; then
        govulncheck -json ./... >"$YGG_TMP/vuln.json" 2>/dev/null || true
        govulncheck -json -tags integration ./... >"$YGG_TMP/vuln-integration.json" 2>/dev/null || true
        ygg_tool_stage govulncheck-json build "govulncheck wrote no report; see the log" "$YGG_TMP/vuln.json"
      fi
      govulncheck ./...
      ! ygg_typed || ygg_tool_stage govulncheck-json build "govulncheck wrote no report; see the log" "$YGG_TMP/vuln-integration.json"
      govulncheck -tags integration ./...
      ;;
    osv)
      echo "== OSV"
      if ygg_typed; then
        osv-scanner scan source -L go.mod --format json >"$YGG_TMP/osv.json" 2>/dev/null || true
        ygg_tool_stage osv-json build "osv-scanner wrote no report; see the log" "$YGG_TMP/osv.json"
      fi
      osv-scanner scan source -L go.mod
      ;;
  esac
done
