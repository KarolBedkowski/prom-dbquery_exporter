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
	"maps"
	"slices"
	"sort"

	"github.com/hashicorp/go-multierror"
	"prom-dbquery_exporter.app/internal/conf"
)

// GlobalRegistry keep information about all registered database drivers.
var GlobalRegistry = newDbRegistry()

// Database load data from database.
type Database interface {
	// Query execute sql and returns records or error. Open connection when necessary.
	Query(ctx context.Context, q *conf.Query, params map[string]any) (*QueryResult, error)
	// Close db connection.
	Close(ctx context.Context) error
	// Human-friendly info
	String() string
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

type dbDefinition interface {
	instanate(cfg *conf.Database) (Database, error)
	validateConf(cfg *conf.Database) error
}

type Registry struct {
	dbDefs map[string]dbDefinition
}

func newDbRegistry() Registry {
	return Registry{
		dbDefs: make(map[string]dbDefinition),
	}
}

func (d Registry) List() []string {
	if len(d.dbDefs) == 0 {
		return nil
	}

	s := slices.Collect(maps.Keys(d.dbDefs))
	sort.Strings(s)

	return s
}

func (d Registry) IsSupported(cfg *conf.Database) bool {
	_, ok := d.dbDefs[cfg.Driver]

	return ok
}

func (d Registry) Validate(cfg *conf.Database) error {
	def, ok := d.dbDefs[cfg.Driver]
	if !ok {
		return NotSupportedError(cfg.Driver)
	}

	var errs *multierror.Error
	errs = multierror.Append(errs, def.validateConf(cfg))
	errs = multierror.Append(errs, validateCommon(cfg))

	return errs.ErrorOrNil()
}

// CreateLoader returns configured Loader for given configuration.
func (d Registry) GetInstance(cfg *conf.Database) (Database, error) {
	if db, ok := d.dbDefs[cfg.Driver]; ok {
		return db.instanate(cfg)
	}

	return nil, InvalidConfigurationError(fmt.Sprintf("unsupported database type '%s'", cfg.Driver))
}

func registerDatabase(def dbDefinition, names ...string) {
	for _, n := range names {
		GlobalRegistry.dbDefs[n] = def
	}
}
