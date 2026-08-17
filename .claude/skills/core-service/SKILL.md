---
name: core-service
description: Build or extend an HTTP service on the alpha-omega-corp `_core` framework (module github.com/alpha-omega-corp/_core) — bootstrapping a new service binary, or adding a resource end to end across model → handler → controller → routes. Use whenever the work touches app.New/Bootstrap, app.Deps, app.BaseController, app.Repository, helpers.Handle*, a bun model registered with Bootstrap, or a `cmd/config.yml` + `cmd/fixtures/fixture.yml` embed pair. Also use when running the service's CLI (`server`, `migration`, `sync`).
---

# Building on `_core`

`_core` is a library. A service is a thin binary that hands it an `embed.FS`, a list of `bun`
models, and a function that registers routes. Everything below is the shape that binary takes.

A request moves one way, and each layer has exactly one job:

```
main.go  →  StartFunc  →  Controller  →  Handler  →  Repository[E]  →  bun
(embed)     (routes)      HTTP shape    business    generic CRUD
```

Never skip a layer. A controller that touches `deps.DB`, or a handler that touches
`http.ResponseWriter`, is the mistake this framework exists to prevent.

## Adding a resource to an existing service

Five files, in this order. The running example is `SocialLink`

### 1. Model — `pkg/models/social_link.go`

```go
type SocialLink struct {
	bun.BaseModel `bun:"table:social_links,alias:social_link"`

	Id int64 `bun:"id,pk,autoincrement" json:"id"`

	Platform string `bun:"platform,notnull" json:"platform"`
	Url      string `bun:"url,notnull" json:"url"`
	Position int    `bun:"position,notnull" json:"position"`

	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:current_timestamp" json:"updatedAt"`
}
```

Non-negotiable: the PK field must be named **`Id`** — `Repository.Update` finds it by reflection and
panics without it. Table name snake_plural, JSON tags camelCase (the clients are TypeScript).

### 2. Handler — `pkg/handlers/social_link.go`

The handler owns the business rules and is the only layer that knows the repository exists. It wires
itself in `New`; nothing else constructs it.

```go
type SocialLinkHandler struct {
	repository app.Repository[models.SocialLink]
}

func (h *SocialLinkHandler) New(deps app.Deps) {
	h.repository = app.NewRepository[models.SocialLink](deps.DB)
}

func (h *SocialLinkHandler) GetAll(ctx context.Context) (*[]models.SocialLink, error) {
	return h.repository.GetAll(ctx, func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Order("position ASC", "id ASC")
	})
}

func (h *SocialLinkHandler) GetOne(ctx context.Context, id int64) (*models.SocialLink, error) {
	return h.repository.GetOne(ctx, id)
}

func (h *SocialLinkHandler) Create(ctx context.Context, data *models.SocialLink) (*models.SocialLink, error) {
	now := time.Now()
	data.CreatedAt, data.UpdatedAt = now, now
	return h.repository.Create(ctx, data)
}

// Update keeps the stored id: the repository matches on the struct's Id field,
// so a body with a missing or wrong one would update the wrong row.
func (h *SocialLinkHandler) Update(ctx context.Context, id int64, data *models.SocialLink) (*models.SocialLink, error) {
	data.Id = id
	data.UpdatedAt = time.Now()
	return h.repository.Update(ctx, data)
}

func (h *SocialLinkHandler) Delete(ctx context.Context, id int64) error {
	return h.repository.Delete(ctx, id)
}
```

Handler method signatures are not free-form — they are the contracts the route adapters accept:

| Adapter           | Handler method signature                              |
| ----------------- | ----------------------------------------------------- |
| `Handle`          | `func(ctx) (*T, error)`                               |
| `HandleOne`       | `func(ctx, id int64) (*T, error)`                     |
| `HandleBody`      | `func(ctx, *B) (*T, error)`                           |
| `HandleOneBody`   | `func(ctx, id int64, *B) (*T, error)`                 |
| `HandleDelete`    | `func(ctx, id int64) error`                           |

Write the handler to fit one of these and the controller is one line. A handler that does not fit —
an argument past the context, a value return instead of a pointer — is not a handler to bend; it
goes through `app.HttpResponse` (below) instead.

Validators are plain functions, colocated with the handler:

```go
func ValidateSocialLink(data *models.SocialLink) error {
	if strings.TrimSpace(data.Platform) == "" {
		return fmt.Errorf("platform is required")
	}
	return nil
}
```

A validator error becomes a **400** and the handler never runs. Anything the handler returns is a
**500** — by then the input was acceptable and it is the server that failed. Put every
caller-fixable check in a validator.

### 3. Controller — `pkg/controllers/social_link.go`

Stateless. Embed `BaseController`, expose a package-level singleton, one method per route.

```go
type SocialLinkController struct {
	app.BaseController[*handlers.SocialLinkHandler]
}

var SocialLink = &SocialLinkController{}

func (c *SocialLinkController) GetAll() bunrouter.HandlerFunc {
	return helpers.Handle(c.Handler().GetAll)
}
func (c *SocialLinkController) GetOne() bunrouter.HandlerFunc {
	return helpers.HandleOne(c.Handler().GetOne)
}
func (c *SocialLinkController) Create() bunrouter.HandlerFunc {
	return helpers.HandleBody(c.Handler().Create, handlers.ValidateSocialLink)
}
func (c *SocialLinkController) Update() bunrouter.HandlerFunc {
	return helpers.HandleOneBody(c.Handler().Update, handlers.ValidateSocialLink)
}
func (c *SocialLinkController) Delete() bunrouter.HandlerFunc {
	return helpers.HandleDelete(c.Handler().Delete)
}
```

The type parameter is a **pointer** type (`*handlers.SocialLinkHandler`). A value type compiles and
panics at `Setup`.

Two controllers may share one handler when they serve different audiences — `butcher`'s
`CheckoutController` and `OrderController` both drive `OrderHandler`.

### 4. Register + route — in the service's `StartFunc`

```go
app.RegisterControllers(deps, controllers.SocialLink /*, ... */)

social := api.NewGroup("/social")
social.GET("", controllers.SocialLink.GetAll())
social.GET("/:id", controllers.SocialLink.GetOne())
social.POST("", controllers.SocialLink.Create())
social.PUT("/:id", controllers.SocialLink.Update())
social.DELETE("/:id", controllers.SocialLink.Delete())
```

**`RegisterControllers` must come before any route registration.** `c.Handler()` is evaluated while
the route is being wired, not when it is called; an unregistered controller nil-panics at startup.

The `:id` param name is load-bearing — `GetParams` coerces exactly `id` to `int64`. Any other param
(`:reference`) stays a string and needs `req.Param("reference")` directly.

### 5. Model list + fixtures

Add the model to the `Bootstrap` list in `cmd/main.go`, as a typed nil pointer:

```go
}, []interface{}{
	(*models.SocialLink)(nil),
}...)
```

Then add a block to `cmd/fixtures/fixture.yml` (`- model: SocialLink` / `rows:`), and apply it:

- `go run ./cmd sync` — creates the table, adds new columns, **keeps data**. Default choice.
- `go run ./cmd migration` — **drops and recreates every table**, then reloads fixtures. Dev/seed
  only.

(The `app:`/`db:` labels in `--help` are `urfave/cli` categories — display grouping only. The
commands are top-level: `server`, `migration`, `sync`.)

`sync` is additive only: it never re-types a column, never drops one a model lost, and never adds
a constraint. A rename or a type change is a hand-written SQL job.

## When the handler does not fit an adapter

`app.HttpResponse` is the next step down, not a hand-written `func(w, req)`. It takes one closure —
the one doing the work — and owns the encoding, the status and the error path:

```go
func (c *SlotController) GetAll() bunrouter.HandlerFunc {
	return app.HttpResponse(func(w http.ResponseWriter, req bunrouter.Request) ([]handlers.Slot, error) {
		return c.Handler().Available(req.Context(), time.Now())
	})
}
```

Two things it buys over the old two-closure form:

- **`T` is the type the handler already returns**, value or pointer. `helpers.Response` fixed it at
  `*T`, so a handler returning `[]Slot` had to assign, nil-check and address-take across three lines
  the encoder never needed.
- **`w` and `req` are both in scope**, so a route keyed by something other than `:id` stays in this
  form — `req.Param("reference")`, a header, a query string — and so do the decoding primitives,
  which take the pair:

```go
return app.HttpResponse(func(w http.ResponseWriter, req bunrouter.Request) (*models.Meat, error) {
	params := helpers.GetParams[helpers.GetOne](w, req)
	body := helpers.GetBody[handlers.MeatBody](w, req)

	return c.Handler().Update(req.Context(), params.Id, body)
})
```

`GetParams`/`GetBody` report their own failure and still hand back a zero value (see Sharp edges), so
a handler that cannot proceed on one should return `HttpFail` rather than run on the zero.

Errors returned from the closure are **500** unless tagged. Tag the ones the caller could have
avoided, and the controller never touches the `ResponseWriter`:

```go
func (h *OrderHandler) GetByReference(ctx context.Context, ref string) (*models.Order, error) {
	order, err := h.repository.GetOneBy(ctx, "reference", ref)
	if err != nil {
		return nil, app.HttpFail(http.StatusNotFound, err)
	}
	return order, nil
}
```

`HttpStatus` unwraps, so an `HttpFail` that has been through `fmt.Errorf("...: %w", err)` on the way
up still lands on the status it was tagged with.

## Escaping HttpResponse

Write the raw `func(w http.ResponseWriter, req bunrouter.Request) error` only when the route does
something other than "produce a value, encode it" — a multipart upload streamed to disk, a webhook
that must ack with a fixed body, a redirect:

```go
func (c *MeatController) UploadImage() bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		if err := req.ParseMultipartForm(10 << 20); err != nil {
			helpers.Error(w, err, http.StatusBadRequest)
			return nil
		}
		// ...
	}
}
```

After `helpers.Error`, **`return nil`**. Returning the error too makes bunrouter write a second
response.

This verbose form also survives in older code that predates the adapters
(`butcher/pkg/controllers/meat.go`, `checkout.go`). It is being migrated away from — don't copy it
into new resources, and prefer converting nearby methods when you are already editing that file.

## Bootstrapping a new service

```
server/
  cmd/
    main.go
    config.yml               # must sit at the embed root
    fixtures/fixture.yml
  pkg/
    controllers/  handlers/  models/  middleware/
```

`cmd/main.go`:

```go
var (
	//go:embed config.yml fixtures/fixture.yml
	efs embed.FS
)

func main() {
	app.New(efs).Bootstrap(func(router *bunrouter.Router, deps app.Deps) {
		api := router.Use(middleware.CORS)

		app.RegisterControllers(deps, controllers.Thing)

		thing := api.NewGroup("/thing")
		thing.GET("", controllers.Thing.GetAll())

		// One preflight for every group: bunrouter answers an unrouted OPTIONS
		// from its own 405 handler, outside the CORS middleware, so the reply
		// carries no CORS headers and the browser's write never happens.
		api.OPTIONS("/*path", middleware.Preflight)
	}, []interface{}{
		(*models.Thing)(nil),
	}...)
}
```

`LoadConfig` does `efs.ReadFile("config.yml")` and `RunMigrations` does `fs.Sub(efs, "fixtures")` —
both paths are relative to the embed root, which is why `main.go` lives in `cmd/` beside them.

`cmd/config.yml`:

```yaml
server:
  host: localhost
  port: 8080

database:
  url: localhost
  port: 5433
  username: svc
  password: svc
  name: svc

stripe:            # omit entirely unless the service takes money
  secret_key: ""
  webhook_secret: ""
  currency: "chf"
  api_base: ""     # points at stripe-mock when set; empty in production
```

Need a new config section? Add the struct + `mapstructure` tag to `_core/app/config.go` — `Config`
is shared, so every consumer sees the field, and a missing key just leaves it zero.

## Running it

```
go run ./cmd server        # serve
go run ./cmd sync          # additive schema update
go run ./cmd migration     # DESTRUCTIVE reset + fixtures
go build ./... && go vet ./...
```

## Before you call it done

- [ ] Model has an `Id` field and `bun` table/alias tags; JSON tags camelCase.
- [ ] Model added to the `Bootstrap` list **and** to `fixture.yml`.
- [ ] Handler wires itself in `New(deps)`; no `*bun.DB` outside handlers.
- [ ] Handler signatures match an adapter; caller-fixable checks are in validators.
- [ ] Controller is stateless, one line per route, pointer type parameter.
- [ ] Controller is in `RegisterControllers`, and that call precedes route registration.
- [ ] Every `helpers.Error` is followed by `return nil`.
- [ ] `go build ./...` and `go vet ./...` clean; ran `sync` against a live DB.

## Framework quirks that will bite

- `helpers.JSON` does not return after `Error` — an error response gets `null` appended and a second
  `WriteHeader`. This affects `helpers.Response` and every `Handle*` adapter; `app.HttpResponse` is
  clean. `GetBody`/`GetParams` have it independently: they report the failure and still hand back a
  zero value, so a malformed body reaches the handler as an empty struct. Validate.
- `Config.Debug` is dead (it reads a top-level `debug:`, configs write `server.debug`), and
  `reqlog`/`bundebug` are hardcoded on — full query logging in production.
- `Repository.Update` writes **every** column except `created_at`. Load-modify-save, or a partial
  struct silently zeroes what it omitted.
- No graceful shutdown: the `server` command exits on signal with requests in flight.
- DSN is hardcoded `sslmode=disable`.
