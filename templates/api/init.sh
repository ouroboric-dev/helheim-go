#!/bin/sh
# Prepares a fresh clone for work: downloads the modules. Run it once; it changes nothing else.
set -eu
go mod download
echo "ready: run ./... tests with 'go test ./...'"
