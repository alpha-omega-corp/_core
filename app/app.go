package app

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

type App struct {
	name      string
	dbHandler *DatabaseHandler
	config    *Config
	models    []any
	router    *bunrouter.Router

	fs embed.FS
}

func NewApp(efs embed.FS) *App {
	config, err := LoadConfig(efs)
	if err != nil {
		log.Fatalf("load config error: %v\n", err)
	}

	return &App{
		config: config,
	}
}

func (app *App) Bootstrap(start func(db *bun.DB, router *bunrouter.Router), m ...any) os.Signal {
	app.models = append(app.models, append(m)...)

	appCli := &cli.Command{
		Usage: "application cli",
		Commands: []*cli.Command{
			app.serverCommand(start),
			app.migrationCommand(),
		},
	}

	if err := appCli.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("app start error: %v\n", err)
	}

	// Create keyboard listener
	ch := make(chan os.Signal, 3)
	signal.Notify(
		ch,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)

	return <-ch
}

func (app *App) serverCommand(start func(db *bun.DB, router *bunrouter.Router)) *cli.Command {
	return app.createCommand("app", "server", func(ctx context.Context, cmd *cli.Command) {
		router := bunrouter.New(
			bunrouter.WithMiddleware(reqlog.NewMiddleware(
				reqlog.WithEnabled(true),
				reqlog.WithVerbose(true),
			)),

			bunrouter.Use(bunrouterotel.NewMiddleware(
				bunrouterotel.WithClientIP(),
			)))

		handler := otelhttp.NewHandler(router, "")
		httpSrv := &http.Server{
			Addr:         app.config.Server.Host,
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

func (app *App) migrationCommand() *cli.Command {
	return app.createCommand("db", "migration", func(ctx context.Context, cmd *cli.Command) {
		db := app.dbHandler.Db()

		migrator := migrate.NewMigrator(db, migrate.NewMigrations())
		if err := migrator.Init(ctx); err != nil {
			panic(err)
		}
		if err := db.ResetModel(ctx, app.models...); err != nil {
			panic(err)
		}

		fixture := dbfixture.New(db)
		if err := fixture.Load(ctx, os.DirFS("cmd/fixtures"), "fixture.yml"); err != nil {
			fmt.Printf("load fixture error: %v\n", err)
			panic(err)
		}
	})
}

func (app *App) createCommand(category string, name string, action func(ctx context.Context, cmd *cli.Command)) *cli.Command {
	return &cli.Command{
		Name:     name,
		Category: category,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			app.dbHandler = NewDatabaseHandler(app.config.Database)
			app.dbHandler.Db().RegisterModel(app.models...)

			action(ctx, cmd)

			return nil
		},
	}
}
