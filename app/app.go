package app

import (
	"context"
	"embed"
	"log"
	"os"

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

type StartFunc func(router *bunrouter.Router, deps Deps)

func New(efs embed.FS) *App {
	config, err := LoadConfig(efs)
	if err != nil {
		log.Fatalf("load config error: %v\n", err)
	}

	dbHandler := NewDatabaseHandler(config.Database)

	return &App{
		config:    config,
		dbHandler: dbHandler,
		fs:        efs,
	}
}

func (app *App) Bootstrap(start StartFunc, models ...any) {
	db := app.dbHandler.Db()
	db.RegisterModel(models...)

	cmd := NewCommandHandler(app.config, db, app.fs, models)

	appCli := &cli.Command{
		Usage: "application cli",
		Commands: []*cli.Command{
			cmd.StartServer(start),
			cmd.RunMigrations(),
			// Additive counterpart to migration: adds columns a model has
			// grown without dropping the tables, so a schema change does not
			// cost a consumer its data.
			cmd.RunMigrationUpdate(),
		},
	}

	if err := appCli.Run(context.Background(), os.Args); err != nil {
		log.Fatalf("app start error: %v\n", err)
	}
}
