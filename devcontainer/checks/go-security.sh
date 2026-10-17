#!/bin/bash
# Rung 4 of the ladder for a Go leaf (spec §9.5, §9.6): known vulnerabilities, in the code that is called
# (govulncheck) and in the modules that are required (OSV). Secrets are scanned by Odin before every
# checkpoint, and the product's own security tests run with the unit tests. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
echo "== govulncheck"
govulncheck ./...
govulncheck -tags integration ./...
echo "== OSV"
osv-scanner scan source -L go.mod
