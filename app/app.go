package app

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alpha-omega-corp/_core/command"
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

func NewApp(efs embed.FS, models ...any) *App {
	config, err := LoadConfig(efs)
	if err != nil {
		log.Fatalf("load config error: %v\n", err)
	}

	dbHandler := NewDatabaseHandler(config.Database)

	return &App{
		config:    config,
		dbHandler: dbHandler,
	}
}

func (app *App) Bootstrap() os.Signal {

	cmd := command.NewCommandHandler(app)

	appCli := &cli.Command{
		Usage: "application cli",
		Commands: []*cli.Command{
			cmd.StartServer(),
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

func (app *App) GetDb() *bun.DB {
	return app.db
}

func (app *App) GetModels() []any {
	return app.models
}

func (app *App) GetConfig() *Config {
	return app.config
}
