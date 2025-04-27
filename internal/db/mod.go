package db

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"database/sql"
	"fmt"

	"prom-dbquery_exporter.app/internal/conf"
)

// CreateLoader returns configured Loader for given configuration.
func CreateLoader(cfg *conf.Database) (Database, error) {
	switch cfg.Driver {
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		return newPostgresLoader(cfg)
	case "sqlite3", "sqlite":
		return newSqliteLoader(cfg)
	case "mysql", "mariadb", "tidb":
		return newMysqlLoader(cfg)
	case "oracle", "oci8":
		return newOracleLoader(cfg)
	case "mssql":
		return newMssqlLoader(cfg)
	}

	return nil, InvalidConfigurationError(fmt.Sprintf("unsupported database type '%s'", cfg.Driver))
}

// Database load data from database.
type Database interface {
	// Query execute sql and returns records or error. Open connection when necessary.
	Query(ctx context.Context, q *conf.Query, params map[string]any) (*QueryResult, error)
	// Close db connection.
	Close(ctx context.Context) error
	// Human-friendly info
	String() string
	// UpdateConf return true when configuration was updated
	UpdateConf(db *conf.Database) bool

	// Stats return database stats if available
	Stats() *DatabaseStats
}

// DatabaseStats transfer stats from database driver.
type DatabaseStats struct {
	Name                   string
	DBStats                sql.DBStats
	TotalOpenedConnections uint32
	TotalFailedConnections uint32
}
