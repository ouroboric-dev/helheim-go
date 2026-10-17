# AGENTS.md: api

The Go HTTP API of this product. It owns `openapi.yaml`, the contract every consumer builds against.

Follow the Yggdrasil invariants:
https://github.com/ouroboric-dev/yggdrasil/blob/main/AGENTS.md#invariants
This file only adds rules for this leaf. Read `docs/` for the design; this file says how to work here.

## Work here

- Build, test, lint: `go build ./...`, `go test -race ./...`, `go vet ./...`, `golangci-lint run`.
- `go.mod` says `module api`. Imports are `api/internal/...`.
- Standard library first: `net/http` with the Go 1.22 pattern mux. A new dependency needs a reason in
  the commit message that the standard library cannot give.
- The server listens on `PORT`. It reads nothing else from the environment that is not named in `docs/`.

## Rules for every change

- **Test first.** A new test fails on the parent commit, for the reason the task gives. The factory
  checks this: a change to code without a new or changed test is refused.
- **The contract and the code change together.** A route is added in `internal/httpapi` and described in
  `openapi.yaml` in the same commit. A change that breaks consumers (a removed route or field, a new
  required field) is a contract change: say so in the commit message.
- **The server builds every storage key** from the logged-in user and the resource. It ignores keys a
  client sends. Authorisation is checked in the handler, not only in middleware.
- Errors answer JSON `{"error": "..."}` with the right status. A handler never leaks a stack trace, a
  query or a file path.
- No secret in the repository, in a test or in a log line.

## Layout

- `cmd/api`: the program. It wires things and starts the server; it holds no logic.
- `internal/httpapi`: routes, request and response encoding, errors.
- `openapi.yaml`: the contract.
