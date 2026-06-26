package app

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bundebug"
)

type DatabaseHandler struct {
	dbOnce sync.Once
	db     *bun.DB

	config DatabaseConfig
}

func NewDatabaseHandler(config DatabaseConfig) *DatabaseHandler {
	return &DatabaseHandler{
		config: config,
	}
}

func (h *DatabaseHandler) Db() *bun.DB {
	h.dbOnce.Do(func() {
		dsn := fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			h.config.Username,
			h.config.Password,
			h.config.Url,
			h.config.Port,
			h.config.Name,
		)

		driverOptions := pgdriver.NewConnector(
			pgdriver.WithDSN(dsn),
			pgdriver.WithTLSConfig(nil),
		)

		conn := sql.OpenDB(driverOptions)
		db := bun.NewDB(conn, pgdialect.New())

		db.WithQueryHook(bundebug.NewQueryHook(
			bundebug.WithEnabled(true),
			bundebug.WithVerbose(true),
		))

		h.db = db
	})

	return h.db
}
