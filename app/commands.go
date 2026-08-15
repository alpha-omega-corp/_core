package app

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"
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

type CommandHandler struct {
	db     *bun.DB
	models []interface{}
	config *Config
	fs     embed.FS
}

func NewCommandHandler(config *Config, db *bun.DB, efs embed.FS, models []interface{}) *CommandHandler {
	return &CommandHandler{
		db:     db,
		models: models,
		config: config,
		fs:     efs,
	}
}

func (h *CommandHandler) StartServer(start StartFunc) *cli.Command {
	return h.createCommand("app", "server", func(ctx context.Context, cmd *cli.Command) {
		router := bunrouter.New(
			bunrouter.WithMiddleware(reqlog.NewMiddleware(
				reqlog.WithEnabled(true),
				reqlog.WithVerbose(true),
			)),

			bunrouter.Use(bunrouterotel.NewMiddleware(
				bunrouterotel.WithClientIP(),
			)))

		start(router, Deps{DB: h.db, Config: h.config})

		handler := otelhttp.NewHandler(router, "")
		host := fmt.Sprintf("%s:%d", h.config.Server.Host, h.config.Server.Port)

		httpSrv := &http.Server{
			Addr:         host,
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

		ch := make(chan os.Signal, 3)
		signal.Notify(
			ch,
			syscall.SIGINT,
			syscall.SIGQUIT,
			syscall.SIGTERM,
		)

		<-ch
	})
}

func (h *CommandHandler) RunMigrations() *cli.Command {
	return h.createCommand("db", "migration", func(ctx context.Context, cmd *cli.Command) {
		migrator := migrate.NewMigrator(h.db, migrate.NewMigrations())
		if err := migrator.Init(ctx); err != nil {
			panic(err)
		}
		if err := h.db.ResetModel(ctx, h.models...); err != nil {
			panic(err)
		}

		fixturesFS, err := fs.Sub(h.fs, "fixtures")
		if err != nil {
			panic(err)
		}

		fixture := dbfixture.New(h.db)
		if err := fixture.Load(ctx, fixturesFS, "fixture.yml"); err != nil {
			fmt.Printf("load fixture error: %v\n", err)
			panic(err)
		}
	})
}

// RunMigrationUpdate Create an updated state. Update existing table structures.
func (h *CommandHandler) RunMigrationUpdate() *cli.Command {
	return h.createCommand("db", "sync", func(ctx context.Context, cmd *cli.Command) {
		for _, model := range h.models {
			if _, err := h.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
				log.Fatalf("create table: %v", err)
			}

			table := h.db.Table(reflect.TypeOf(model).Elem())

			// Find existing tables
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

			// Add missing model column
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

func (h *CommandHandler) createCommand(category string, name string, action func(ctx context.Context, cmd *cli.Command)) *cli.Command {
	return &cli.Command{
		Name:     name,
		Category: category,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			action(ctx, cmd)

			return nil
		},
	}
}
