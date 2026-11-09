# DashboardGO

## Workflow (mandatory)
1. Before any task, ask the user clarifying questions and confirm scope. Never start work unprompted.
2. Before altering anything in the code (source, migrations, tests, config or docs), enter plan mode and
   write a plan (files to touch, approach, tests). Wait for the user's approval before making any change.
   For multi-part designs, present and approve each section in plan mode, one section at a time.
3. Work in small steps. Run formatters, linters and tests before reporting a task done.

## Ponytail (mandatory on every coding task)
- Use the `ponytail` skill at level `full`: the smallest change that fully solves the task. Delete before adding.
- Reuse what the codebase already has; add no abstraction, option or dependency nobody asked for.
- A shortcut with a known limit gets a one-line `shortcut: <limit>, <when to upgrade>` comment.
- The workflow above still applies: ask first, plan mode, then change.

## Stack
- Go 1.26+, Gin, PostgreSQL 18 via `pgx/v5` + `pgxpool` with hand-written parameterized SQL, `golang-migrate` for migrations.
- `log/slog` for logging, env-based config, Gin binding tags (`validator/v10`) for input validation.

## Architecture (handler → service → repository; dependencies point inward)
- `cmd/api/main.go`: wiring and graceful shutdown (`http.Server` + `signal.NotifyContext`) only.
- `internal/handler`: bind/validate requests, call services, map to DTOs and HTTP codes. No SQL here.
- `internal/service`: business logic. Depends on repository interfaces, never on Gin.
- `internal/repository`: Postgres access; `internal/repository/db` holds only the `DBTX` interface; filter-dropdown lookups live in `lookups.go`.
- `internal/model` (domain), `internal/dto` (request/response), `internal/middleware` (auth, logging, recovery, CORS).
- `migrations/`: numbered `up`/`down` SQL files. Never edit an applied migration.

## PostgreSQL rules
- One shared `pgxpool` with tuned `MaxConns`. Pass `c.Request.Context()` through every layer and set query timeouts.
- Parameterized queries only; never build SQL by string concatenation.
- Use transactions in services through a repository `WithTx` helper.
- Avoid N+1: use JOINs or batch with `= ANY($1)`. Paginate lists (keyset preferred). Index FKs and filter columns.
- Wrap errors with `fmt.Errorf("...: %w", err)`; map domain errors to HTTP codes in one place.

## Testing (every function and feature)
- `tests/unit/<package>/`: table-driven tests with `testify` and repositories mocked with `mockery`.
- `tests/integration/<feature>/`: real Postgres via `TEST_POSTGRES_URL` (throwaway database per test), endpoints via `httptest`, tagged `//go:build integration`.
- Each new function gets a unit test; each new endpoint or feature gets an integration test.
- Run: `go tool -modfile=tools/go.mod task test`, `go tool -modfile=tools/go.mod task test:integration`, `go tool -modfile=tools/go.mod task check` before done.

## Formatting hooks (`.claude/settings.json`, PostToolUse on Edit|Write)
- `*.go` → `gofmt -w` then `golangci-lint run` on the file's package, reporting only the edited file's issues (no `--fix`, so other files are never rewritten; `go tool -modfile=tools/go.mod task check` covers the rest).
- Fix lint errors at the source; don't disable rules to silence them.
