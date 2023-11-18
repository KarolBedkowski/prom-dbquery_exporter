//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"prom-dbquery_exporter.app/internal/conf"
)

// Record is one record (row) loaded from database.
type Record map[string]interface{}

func newRecord(rows *sqlx.Rows) (Record, error) {
	rec := Record{}
	if err := rows.MapScan(rec); err != nil {
		return nil, fmt.Errorf("map scan record error: %w", err)
	}

	// convert []byte to string
	for k, v := range rec {
		if v, ok := v.([]byte); ok {
			rec[k] = string(v)
		}
	}

	return rec, nil
}

// Loader load data from database.
type Loader interface {
	// Query execute sql and returns records or error. Open connection when necessary.
	Query(ctx context.Context, q *conf.Query, params map[string]string) (*QueryResult, error)
	// Close db connection.
	Close(ctx context.Context) error
	// Human-friendly info
	String() string
	// UpdateConf return true when given configuration is differ than used
	UpdateConf(db *conf.Database) bool

	// Stats return database stats if available
	Stats() *LoaderStats
}

// QueryResult is result of Loader.Query.
type QueryResult struct {
	// rows
	Records []Record
	// query duration
	Duration float64
	// query start time
	Start time.Time
	// all query parameters
	Params map[string]interface{}
}

// LoaderStats transfer stats from database driver.
type LoaderStats struct {
	Name                   string
	DBStats                sql.DBStats
	TotalOpenedConnections uint32
	TotalFailedConnections uint32
}
