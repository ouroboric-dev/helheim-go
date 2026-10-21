#!/bin/sh
# Starts the leaf: builds the server into a temporary folder and runs it in the foreground on PORT.
# Run it in the leaf with PORT set. It fetches no packages (it needs the modules in the Go module
# cache), reads no secret and writes only into the temporary folder.
set -eu

if [ -z "${PORT:-}" ]; then
	echo "run.sh: PORT is not set; set it to the port the server listens on" >&2
	exit 1
fi

cd "$(dirname "$0")"
bin=$(mktemp -d)
# GOPROXY=off: a missing module stops the build with a message instead of reaching for the network.
if ! GOWORK=off GOFLAGS=-mod=readonly GOPROXY=off go build -o "$bin/api" ./cmd/api; then
	echo "run.sh: go build failed; the Go modules of this leaf must be in the module cache (run init.sh once)" >&2
	exit 1
fi
exec "$bin/api"
