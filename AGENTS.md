# Project conventions

`CLAUDE.md` is a symlink to this file. Keep technical artifacts in English.

## Architecture

Use [Go With The Domain](https://threedots.tech/go-with-the-domain/) as the conceptual architecture reference.
Keep this application self-contained and domain-focused; add only behavior required by the project.

- Domain: standard library only.
- Adapter root: repository contracts and domain types only.
- Application: adapter-root contracts, domain and common helpers; no concrete adapter imports.
- Application: only `command/` and `query/` handlers plus the `Application` value; catalog behavior with state or I/O (source merging, listing cache, background downloads) sits behind the adapter-root ports `RomListings` and `RomDownloads`, implemented by the `romlistings` and `romdownloads` adapters. There is no `app/catalog` package.
- Transport ports: application and domain, with standard-library HTTP/templates/logging.
- SQLite adapter: adapter-root contracts, domain, bob, opt, dbmate and the SQLite driver.
- Service: `NewApp` is the single application constructor and assembles all handlers in one application value; do not add catalog-specific application constructors. Only `cmd/web` and `cmd/worker` assemble infrastructure: `cmd/web` owns the HTTP server and queues the downloads, `cmd/worker` owns the download runner. The two processes share one SQLite database.
- Downloads adapter: `romdownloads.Enqueuer` only queues a job and is what the web server uses; `romdownloads.Runner` claims and runs the queued jobs and is what the download worker uses. The web process never runs a download.
- Expose every application use case, including the catalog, through `Application.Command` or `Application.Query` using the existing handler/decorator contracts. Transport ports must not bypass them through a parallel application service or an `Application.Catalog` field. `internal/service` is composition only.
- Common helpers: standard library, local trace context and zerolog.

Depguard enforces these boundaries. Repository interfaces belong in `internal/adapter/repository.go`.

## Behavior and tools

Methods receive `context.Context` first. Commands return an error, or the entity they created when the caller needs it; queries return data without modifying state.
The web handler generates new IDs. Preserve existing log fields, metric keys and typed error behavior.
Logger configuration lives in `internal/common/logger`, independent of the environment adapter; `cmd/web`
and `cmd/worker` only invoke it with parsed values and the output writer.

The web UI swaps `#users-section`. Validation/conflict responses preserve submitted values; other errors target
`#flash` without exposing their cause. Templates and assets are embedded. Preserve required third-party notices.

Use `make test`, `make lint`, `make coverage`, `make format`, `make mock` and `make generate`.
Tools use `go tool -modfile=./tools/go.tool.mod`. Never edit generated mocks or `*.bob.go` files manually.
Migrations are embedded SQL with dbmate up/down sections. Optional seeds run transactionally in filename order;
new datasets must be repeatable without overwriting existing users. Defaults and environment variables are
documented in `env.dist`. Building needs CGO and a C compiler; Docker supplies the build environment.
