#!/bin/bash
# Rung 3 of the ladder for a Go leaf (spec §9.5): the contract. openapi.yaml is linted, the code sqlc
# generates is what the SQL says, and the breaking changes of openapi.yaml against the commit the task
# started from are listed. A breaking change is not refused here: the consumers must follow it, and the
# task says so (it decides the pointer-contract approval point later). Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
# shellcheck source=lib/result.sh
. "$(dirname "$0")/lib/result.sh"
ygg_start contract
if [ ! -f openapi.yaml ]; then
  ygg_stage none policy "contract: this leaf has no openapi.yaml, and it owns the contract of its API"
  echo "contract: this leaf has no openapi.yaml, and it owns the contract of its API"
  exit 1
fi
echo "== openapi lint"
rc=0
vacuum lint --no-style --details --fail-severity error openapi.yaml >"$YGG_TMP/vacuum.txt" 2>&1 || rc=$?
cat "$YGG_TMP/vacuum.txt"
if [ "$rc" -ne 0 ]; then
  ygg_stage none lint "$(head -c 3500 "$YGG_TMP/vacuum.txt")"
  exit "$rc"
fi
base="$(git merge-base HEAD refs/heads/main)" || {
  ygg_stage none policy "contract: this leaf has no main to compare with"
  echo "contract: this leaf has no main to compare with"
  exit 1
}
# sqlc diff reads the sqlc.yaml of the commit the task started from, in a copy of the working tree: a
# sqlc.yaml the agent changed, linked or hid cannot matter.
if git cat-file -e "$base:sqlc.yaml" 2>/dev/null; then
  echo "== generated code"
  scratch="$YGG_TMP/tree"
  mkdir "$scratch"
  tar --exclude=./.git -cf - . | tar -x -C "$scratch"
  rm -f -- "$scratch/sqlc.yaml" "$scratch/sqlc.yml" "$scratch/sqlc.json"
  git show "$base:sqlc.yaml" >"$scratch/sqlc.yaml"
  if ! (cd "$scratch" && sqlc diff -f sqlc.yaml) >"$YGG_TMP/sqlc.out" 2>"$YGG_TMP/sqlc.err"; then
    cat "$YGG_TMP/sqlc.out"
    cat "$YGG_TMP/sqlc.err" >&2
    # sqlc names the file, line and column of an error in the SQL; a diff of generated code names none.
    if grep -Eq '^[^[:space:]:][^:]*:[0-9]+:([0-9]+:)? ' "$YGG_TMP/sqlc.out" "$YGG_TMP/sqlc.err"; then
      ygg_stage sqlc-text "$YGG_TMP/sqlc.out" "$YGG_TMP/sqlc.err"
    else
      ygg_stage none build "contract: the code in internal/store is not what the SQL says; run sqlc generate"
    fi
    echo "contract: the code in internal/store is not what the SQL says; run sqlc generate"
    exit 1
  fi
  cat "$YGG_TMP/sqlc.out"
  cat "$YGG_TMP/sqlc.err" >&2
fi
# A breaking change is listed, not refused (the header above says why), so it is no failure of the typed result either.
echo "== breaking changes since the task started"
if git cat-file -e "$base:openapi.yaml" 2>/dev/null; then
  old="$YGG_TMP/openapi.base.yaml"
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
