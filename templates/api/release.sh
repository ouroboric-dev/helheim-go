#!/bin/sh
# Builds the release: a static binary and a run.sh that starts it, in the folder OUT. Run it in the leaf
# with OUT set (a relative OUT is relative to where you stand). It may fetch modules; OUT/run.sh fetches
# nothing, needs no Go and runs from any working directory. The migrations are embedded in the binary.
set -eu

if [ -z "${OUT:-}" ]; then
	echo "release.sh: OUT is not set; set it to the folder that receives the release" >&2
	exit 1
fi
mkdir -p "$OUT"
# Resolve OUT before the cd below.
OUT=$(cd "$OUT" && pwd)

cd "$(dirname "$0")"
rm -rf "$OUT/api" "$OUT/run.sh"
CGO_ENABLED=0 GOWORK=off GOFLAGS=-mod=readonly go build -trimpath -o "$OUT/api" ./cmd/api

# OUT/run.sh was removed above: a link planted there must not be followed.
cat >"$OUT/run.sh" <<'RUN'
#!/bin/sh
# Starts the released API in the foreground on PORT (and HOST).
set -eu

if [ -z "${PORT:-}" ]; then
	echo "run.sh: PORT is not set; set it to the port the server listens on" >&2
	exit 1
fi

case "$0" in
*/*) cd "${0%/*}" ;;
esac
exec ./api
RUN
chmod 755 "$OUT/run.sh"

echo "api: $(wc -c <"$OUT/api" | tr -d ' ') bytes"
