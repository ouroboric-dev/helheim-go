#!/bin/bash
# Prints, one per line, the files of this leaf that are what sqlc generates, and nothing else. A file counts
# only by this test: sqlc runs on a scratch copy of HEAD with the sqlc.yaml of the commit the task started
# from (main), and a file of the leaf is listed when it is a regular file, not a link, with exactly the bytes
# sqlc wrote. A "Code generated" header proves nothing and a folder proves nothing: the agent can write
# both. Used by go-static.sh (what the linter may skip), go-tdd.sh (what needs no test) and, in its own way,
# go-contract.sh. A leaf with no sqlc.yaml at the base owns nothing. Run in the leaf's top folder.
set -euo pipefail
# The tools are found in the image's root-owned folders only, never in one the agent can write (/go/bin).
export PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"
base="$(git merge-base HEAD main 2>/dev/null)" || exit 0
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
git show "$base:sqlc.yaml" >"$scratch/base.yaml" 2>/dev/null || exit 0
mkdir "$scratch/tree"
git archive HEAD | tar -x -C "$scratch/tree"
# Whatever the commit holds under a name sqlc would read is replaced by the base's config.
rm -f -- "$scratch/tree/sqlc.yaml" "$scratch/tree/sqlc.yml" "$scratch/tree/sqlc.json"
cp -- "$scratch/base.yaml" "$scratch/tree/sqlc.yaml"
# Everything is made old first, so that the files sqlc writes are the ones that are newer afterwards.
find "$scratch/tree" -exec touch -h -d '2000-01-01 00:00:00' {} +
# If sqlc fails, nothing is verified, so nothing is owned: the files are code, and go-contract.sh says why.
(cd "$scratch/tree" && sqlc generate -f sqlc.yaml) >&2 || exit 0
while IFS= read -r f; do
  [ -f "$f" ] && [ ! -L "$f" ] && cmp -s -- "$scratch/tree/$f" "$f" && printf '%s\n' "$f"
done < <(cd "$scratch/tree" && find . -type f -newermt '2001-01-01' -printf '%P\n' | sort)
exit 0
