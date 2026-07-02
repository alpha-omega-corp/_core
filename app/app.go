package app

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/uptrace/bun"
	"github.com/uptrace/bunrouter"
	"github.com/urfave/cli/v3"
)

type App struct {
	name      string
	dbHandler *DatabaseHandler
	config    *Config
	models    []any
	router    *bunrouter.Router

	fs embed.FS
}

type StartFunc func(db *bun.DB, router *bunrouter.Router, config *Config)

func NewApp(efs embed.FS, models ...any) *App {
	config, err := LoadConfig(efs)
	if err != nil {
		log.Fatalf("load config error: %v\n", err)
	}

	dbHandler := NewDatabaseHandler(config.Database, models)

	return &App{
		config:    config,
		dbHandler: dbHandler,
	}
}

func (app *App) Bootstrap(start StartFunc) os.Signal {
	cmd := NewCommandHandler(app.config, app.dbHandler)

	appCli := &cli.Command{
		Usage: "application cli",
		Commands: []*cli.Command{
			cmd.StartServer(start),
			cmd.RunMigrations(),
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
