# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`_core` (module `github.com/alpha-omega-corp/_core`) is a shared Go library, not a standalone
application. It has no `main` package and no `config.yml` of its own — it provides a small framework
that other alpha-omega-corp services import to bootstrap a CLI + HTTP server backed by Postgres (via
`bun`). A consuming binary supplies its own `embed.FS` (containing `config.yml` and
`fixtures/fixture.yml`), its own `bun` models, and a `StartFunc` that registers routes.

Known consumers: `butcher/server`, `deployer/server`. `butcher/server` is the reference
implementation — when a framework change lands here, that repo shows the shape it is meant to
produce.

**Because this is a library, every exported change is a breaking change for consumers.** Grep the
consumer repos before renaming or re-shaping an exported symbol.

## Commands

There is no Makefile or task runner; use plain Go tooling.

- Build: `go build ./...`
- Vet: `go vet ./...`
- Format check: `gofmt -l .`
- Test: `go test ./...` (no tests currently exist in this repo)
- After touching deps: `go mod tidy` (`go.sum` has been incomplete before)

## Architecture

Four layers, and a request moves through them in one direction:

```
main.go  →  StartFunc  →  Controller  →  Handler  →  Repository[E]  →  bun  →  Postgres
(embed)     (routes)      (HTTP shape)   (business)  (generic CRUD)
```

### `app` package — the framework

- **`app.go`** — `App` / `New(efs embed.FS)` / `Bootstrap(start StartFunc, models ...any)`.
  `New` loads the config out of the consumer's embedded FS and builds a `DatabaseHandler`.
  `Bootstrap` opens the DB, registers the models, builds the `urfave/cli/v3` command set, and runs
  it. `StartFunc` is `func(router *bunrouter.Router, deps Deps)`.

- **`config.go`** — `Config` (`Server`, `Database`, `Stripe`, `Debug`), populated by `viper` from the
  `config.yml` at the **root** of the consumer's `embed.FS`. `LoadConfig` uses `helpers.Must` /
  `helpers.Check`, so a bad config panics at startup by design. `CONFIG_PATH`, when set, names a file
  on disk that is read **instead** of the embedded one — that is how a container is configured, with
  the config mounted from the host rather than baked into an image. Unset, behaviour is unchanged; set
  and unreadable, it panics rather than falling back, because silently serving build-time defaults
  (another database, another Stripe account) is the worse failure.

- **`database.go`** — `DatabaseHandler.Db()` lazily builds a single `*bun.DB` (Postgres via
  `pgdriver`/`pgdialect`, `bundebug` query logging), guarded by `sync.Once`.

- **`commands.go`** — `CommandHandler` builds the three CLI commands:
  - `server` (category `app`) — builds a `bunrouter.Router` with `reqlog` + `bunrouterotel`
    middleware, calls `start(router, Deps{DB, Config})` so the consumer registers routes, serves
    through `otelhttp`, then blocks on `SIGINT`/`SIGQUIT`/`SIGTERM`.
  - `migration` (category `db`) — **destructive.** `ResetModel` drops and recreates every registered
    model's table, then loads `fixtures/fixture.yml` from the consumer's embed FS.
  - `sync` (category `db`) — the additive counterpart. Creates missing tables, diffs
    `information_schema.columns` against each model's `bun` field tags, and `ADD COLUMN`s whatever
    the model has grown. It never drops, never re-types, and never removes a column a model lost.

- **`controller.go`** — the controller wiring. `Deps{DB, Config}` is what a `Handler` gets;
  `Handler` is the `New(deps Deps)` interface a handler implements to wire its own fields;
  `BaseController[PT Handler]` allocates that handler reflectively in `Setup` and exposes it via
  `Handler()`; `RegisterControllers(deps, ...)` calls `Setup` on each. `PT` must be a pointer type
  (`*MeatHandler`) — a non-pointer panics at startup, which is the documented trade for not being
  able to express "T and *T" in one type parameter.

- **`repository.go`** — `Repository[E]` / `NewRepository[E](db)`: generic `bun`-backed CRUD
  (`GetOne`, `GetOneBy`, `GetAll` with relations + a query tweak func, `Create`, `Update`, `Delete`).

- **`http.go`** — `HttpResponse[T](fn func(bunrouter.Request) (T, error))` collapses the two nested
  closures a route used to need into the one doing the work. `T` is the response type as the handler
  returns it, value or pointer — unlike `helpers.Response`, which fixed it at `*T` and made
  value-returning handlers assign-and-address-take for nothing. `HttpFail(code, err)` /
  `HttpError` / `HttpStatus` tag an error with the status to answer it with, so a handler can
  return a 404 without reaching for the `ResponseWriter`; untagged errors are 500.

### `helpers` package — utilities consumers import directly

- **`http.go`** — two tiers:
  - **Route adapters** (`Handle`, `HandleOne`, `HandleBody`, `HandleOneBody`, `HandleDelete`) take a
    handler *method* and return a `bunrouter.HandlerFunc`. This is the preferred way to write a
    controller method; it collapses the whole decode/dispatch/encode ceremony to one line.
    Validators are variadic; a validator error is a 400 and the handler never runs, so anything the
    handler itself returns is a 500.
  - **Primitives** (`GetBody[T]`, `GetParams[T]`, `Response`, `JSON`, `Error`) for routes the
    adapters do not fit — multipart uploads, non-`id` route params, hand-picked status codes.
- **`nil.go`** — `Must` / `Check`, panic-based helpers for fail-fast startup paths only. Do not use
  them on a request path.

## Conventions

- **Prefer the route adapters.** New controller methods should read like
  `return helpers.HandleOne(c.Handler().GetOne)`. When the handler does not fit an adapter — an
  extra argument, a non-`id` param — the fallback is `app.HttpResponse`, not a hand-written
  `func(w, req)`. The hand-written closure form still exists in older consumer code
  (`butcher/pkg/controllers/meat.go`) — treat it as the thing being migrated away from, not as the
  pattern to copy.
- **Controllers hold no state and no logic.** A controller is a `BaseController` embed, a
  package-level singleton var, and one method per route. Business logic lives in the handler;
  persistence lives in the repository.
- **A handler wires itself in `New(deps)`** and nowhere else. Constructors are not called by the
  consumer — `RegisterControllers` does it.
- **`RegisterControllers` must run before routes are registered.** The adapters read
  `c.Handler()` at registration time, so a controller that has not been through `Setup` nil-panics
  as the routes are wired.
- **Errors carry their own status.** Return an error from the handler for genuine server failures;
  use `helpers.Error(w, err, code)` in the controller for input the caller could fix.
- **Reflection is the framework's escape hatch, not a habit.** It is used in exactly three places
  (`BaseController.Setup`, `Repository.Update`'s `Id` lookup, `sync`'s table walk). Each is
  commented with why the type system could not express it. Hold new uses to the same bar.
- Comments explain *why*, in prose, and are used sparingly — see `app.go:51` or `controller.go:22`.
  Match that register; do not narrate what the code already says.

## Sharp edges

Real inconsistencies in the current code. Do not "fix" them incidentally in an unrelated change, but
know they are here.

- **`helpers.JSON` does not return after `Error`.** On a handler error it writes the status and
  error body, then falls through to `bunrouter.JSON(w, res)` and appends `null` plus a superfluous
  `WriteHeader`. Everything reached through `helpers.Response` and the `Handle*` adapters still has
  this; `app.HttpResponse` does not. `GetBody`/`GetParams` have the same shape independently: they
  report the error and still return a usable zero value, so the caller proceeds on garbage input.
- **`Config.Debug` is dead.** It unmarshals from a top-level `debug:` key, but consumer configs write
  `server.debug`, and `ServerConfig` has no `Debug` field. Meanwhile `reqlog` and `bundebug` are both
  hardcoded to `WithEnabled(true)`, so verbose query logging is always on, in production too.
- **`DatabaseHandler.models` is never set or read.** Model registration moved to `Bootstrap`.
- **No graceful shutdown.** The `server` command returns as soon as a signal arrives; `httpSrv` is
  never `Shutdown`, so in-flight requests are cut.
- **DSN is hardcoded `sslmode=disable`** with `pgdriver.WithTLSConfig(nil)`.
- **`Repository.Update` requires a field literally named `Id`** and reflect-panics without one. It
  also always excludes `created_at`, and updates *every* other column — a partial struct silently
  zeroes the columns it omitted.
- **`migration` drops tables.** It is a dev/seed command. `sync` is the one that is safe to point at
  data you care about. Note that `Category` is only a help-menu grouping in `urfave/cli` — the
  commands are invoked as `server` / `migration` / `sync`, *not* `db sync`.
