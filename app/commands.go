package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dbfixture"
	"github.com/uptrace/bun/migrate"
	"github.com/uptrace/bunrouter"
	"github.com/uptrace/bunrouter/extra/bunrouterotel"
	"github.com/uptrace/bunrouter/extra/reqlog"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Handler struct {
	db     *bun.DB
	models []interface{}
	config *Config
}

func NewCommandHandler(config *Config, dbHandler *DatabaseHandler) *Handler {
	return &Handler{
		db:     dbHandler.Db(),
		models: dbHandler.models,
		config: config,
	}
}

func (h *Handler) StartServer(start StartFunc) *cli.Command {
	return h.createCommand("app", "server", func(ctx context.Context, cmd *cli.Command) {
		router := bunrouter.New(
			bunrouter.WithMiddleware(reqlog.NewMiddleware(
				reqlog.WithEnabled(true),
				reqlog.WithVerbose(true),
			)),

			bunrouter.Use(bunrouterotel.NewMiddleware(
				bunrouterotel.WithClientIP(),
			)))

		start(h.db, router, h.config)

		handler := otelhttp.NewHandler(router, "")
		httpSrv := &http.Server{
			Addr:         h.config.Server.Host,
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  60 * time.Second,
			Handler:      handler,
		}

		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
				log.Fatalf("ListenAndServe failed: %s", err)
			}
		}()

		fmt.Printf("listening on http://%s\n", httpSrv.Addr)
	})
}

func (h *Handler) RunMigrations() *cli.Command {
	return h.createCommand("db", "migration", func(ctx context.Context, cmd *cli.Command) {
		migrator := migrate.NewMigrator(h.db, migrate.NewMigrations())
		if err := migrator.Init(ctx); err != nil {
			panic(err)
		}
		if err := h.db.ResetModel(ctx, h.models...); err != nil {
			panic(err)
		}

		fixture := dbfixture.New(h.db)
		if err := fixture.Load(ctx, os.DirFS("cmd/fixtures"), "fixture.yml"); err != nil {
			fmt.Printf("load fixture error: %v\n", err)
			panic(err)
		}
	})
}

func (h *Handler) SyncModels() *cli.Command {
	return h.createCommand("db", "sync", func(ctx context.Context, cmd *cli.Command) {
		for _, model := range h.models {
			if _, err := h.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
				log.Fatalf("create table: %v", err)
			}

			table := h.db.Table(reflect.TypeOf(model).Elem())

			// Columns the live table already has.
			var currentColumns []string
			if err := h.db.NewSelect().
				ColumnExpr("column_name").
				TableExpr("information_schema.columns").
				Where("table_schema = current_schema()").
				Where("table_name = ?", table.Name).
				Scan(ctx, &currentColumns); err != nil {
				log.Fatalf("inspect %s columns: %v", table.Name, err)
			}
			have := make(map[string]bool, len(currentColumns))
			for _, c := range currentColumns {
				have[c] = true
			}

			// Add any model column missing from the table. New columns are added
			for _, column := range table.Fields {
				if have[column.Name] {
					continue
				}

				q := h.db.NewAddColumn().Model(model).IfNotExists()
				if column.SQLDefault != "" {
					q.ColumnExpr("? ? DEFAULT ?", bun.Ident(column.Name), bun.Safe(column.CreateTableSQLType), bun.Safe(column.SQLDefault))
				} else {
					q.ColumnExpr("? ?", bun.Ident(column.Name), bun.Safe(column.CreateTableSQLType))
				}
				if _, err := q.Exec(ctx); err != nil {
					log.Fatalf("add column %s.%s: %v", table.Name, column.Name, err)
				}
				fmt.Printf("added column %s.%s\n", table.Name, column.Name)
			}
		}

		fmt.Println("schema sync complete")
	})
}

func (h *Handler) createCommand(category string, name string, action func(ctx context.Context, cmd *cli.Command)) *cli.Command {
	return &cli.Command{
		Name:     name,
		Category: category,
		Action: func(ctx context.Context, cmd *cli.Command) error {

			action(ctx, cmd)

			return nil
		},
	}
}
