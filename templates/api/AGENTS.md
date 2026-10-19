# AGENTS.md: api

The Go HTTP API of this product. It owns `openapi.yaml`, the contract every consumer builds against.

Follow the Yggdrasil invariants:
<https://github.com/ouroboric-dev/yggdrasil/blob/main/AGENTS.md#invariants>
This file only adds rules for this leaf. Read `docs/` for the design; this file says how to work here.

## Work here

- Build, test, lint: `go build ./...`, `go test -race ./...`, `go vet ./...`, `golangci-lint run`.
- `go.mod` says `module api`. Imports are `api/internal/...`.
- Standard library first: `net/http` with the Go 1.22 pattern mux. A new dependency needs a reason in
  the commit message that the standard library cannot give. A dependency is a tagged release: no pseudo-version, no `replace`.
- The server listens on `PORT` and uses the database at `DATABASE_URL` when that is set. It reads nothing
  else from the environment that is not named in `docs/`.

## Rules for every change

- **Test first.** A new test fails on the parent commit, for the reason the task gives. The factory
  checks this: a change to code without a new or changed test is refused.
- **The contract and the code change together.** A route is added in `internal/httpapi` and described in
  `openapi.yaml` in the same commit. A change that breaks consumers (a removed route or field, a new
  required field) is a contract change: say so in the commit message.
- **Data goes through sqlc.** A query is SQL in `db/queries`, a schema change is a new file in
  `internal/store/migrations` (never an edit of one that exists), and `sqlc generate` writes
  `internal/store`. The factory refuses a change whose generated code is out of date (`sqlc diff`).
- **The server builds every storage key** from the logged-in user and the resource. It ignores keys a
  client sends. Authorisation is checked in the handler, not only in middleware.
- **Every route with a path variable has a cross-tenant test.** Every path variable is a resource id,
  whatever its name (`{id}`, `{album}`, `{uuid}`). A route with one needs a test `TestCrossTenant<OperationId>` that calls `authztest.Check`: user A gets
  the resource, user B gets 403 or 404. The factory refuses a change without one. A route that anyone may
  read on purpose is named, with the reason, in `authz.json` (`{"schema":"ygg.authz/v1","public":{"<operationId>":"<why>"}}`).
- Errors answer JSON `{"error": "..."}` with the right status. A handler never leaks a stack trace, a
  query or a file path.
- No secret in the repository, in a test or in a log line.
- **Do not change the files that configure the checks.** They are `.github/`, `.devcontainer/`,
  `.pre-commit-config.yaml`, `.golangci.*`, `.markdownlint*`, `.yamllint*`, `.shellcheckrc`, `.gitleaks*`,
  `REUSE.toml`, `.reuse/`, `LICENSES/`, `LICENSE` and `*.license`. The factory refuses a task that adds,
  changes, deletes or links one. If a check is wrong, say so in your report; do not edit the file.

## Layout

- `cmd/api`: the program. It wires things and starts the server; it holds no logic.
- `internal/httpapi`: routes, request and response encoding, errors.
- `internal/store`: the database: migrations, and the code sqlc generates. Do not edit `*.sql.go`.
- `db/queries`: the SQL that sqlc turns into Go.
- Tests that need a database carry `//go:build integration` and read `YGG_DATABASE_URL`.
- `openapi.yaml`: the contract.
