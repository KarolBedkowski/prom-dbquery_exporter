//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
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

type (
	// Loader load data from database.
	Loader interface {
		// Query execute sql and returns records or error. Open connection when necessary.
		Query(ctx context.Context, q *conf.Query, params map[string]string) (*QueryResult, error)
		// Close db connection.
		Close(ctx context.Context) error
		// Human-friendly info
		String() string
		// ConfChanged return true when given configuration is differ than used
		ConfChanged(db *conf.Database) bool

		// Stats return database stats if available
		Stats() *LoaderStats
	}

	// QueryResult is result of Loader.Query.
	QueryResult struct {
		// rows
		Records []Record
		// query duration
		Duration float64
		// query start time
		Start time.Time
		// all query parameters
		Params map[string]interface{}
	}

	genericLoader struct {
		connStr                string
		driver                 string
		conn                   *sqlx.DB
		initialSQL             []string
		dbConf                 *conf.Database
		lock                   sync.RWMutex
		totalOpenedConnections uint32
		totalFailedConnections uint32
	}

	// LoaderStats transfer stats from database driver.
	LoaderStats struct {
		Name                   string
		DBStats                sql.DBStats
		TotalOpenedConnections uint32
		TotalFailedConnections uint32
	}
)

func (g *genericLoader) Stats() *LoaderStats {
	if g.conn != nil {
		return &LoaderStats{
			Name:                   g.dbConf.Name,
			DBStats:                g.conn.Stats(),
			TotalOpenedConnections: atomic.LoadUint32(&g.totalOpenedConnections),
			TotalFailedConnections: atomic.LoadUint32(&g.totalFailedConnections),
		}
	}

	return nil
}

func (g *genericLoader) configureConnection(ctx context.Context) {
	g.conn.SetConnMaxLifetime(600 * time.Second)
	g.conn.SetMaxOpenConns(10)
	g.conn.SetMaxIdleConns(1)

	if p := g.dbConf.Pool; p != nil {
		l := log.Ctx(ctx)

		if p.MaxConnections > 0 {
			l.Debug().Int("max-conn", p.MaxConnections).Msg("max connection set")
			g.conn.SetMaxOpenConns(p.MaxConnections)
		}

		if p.MaxIdleConnections > 0 {
			l.Debug().Int("max-idle", p.MaxIdleConnections).Msg("max idle connection set")
			g.conn.SetMaxIdleConns(p.MaxIdleConnections)
		}

		if p.ConnMaxLifeTime > 0 {
			l.Debug().Int("conn-max-life-time", p.ConnMaxLifeTime).
				Msg("connection max life time set")
			g.conn.SetConnMaxLifetime(time.Duration(p.ConnMaxLifeTime) * time.Second)
		}
	}
}

func (g *genericLoader) openConnection(ctx context.Context) error {
	// lock loader for write
	g.lock.Lock()
	defer g.lock.Unlock()

	llog := log.Ctx(ctx)
	llog.Debug().Str("connstr", g.connStr).Str("driver", g.driver).
		Msg("genericQuery connecting")

	var err error
	if g.conn, err = sqlx.Open(g.driver, g.connStr); err != nil {
		return fmt.Errorf("open error: %w", err)
	}

	g.configureConnection(ctx)

	// check is database is working
	lctx, cancel := context.WithTimeout(ctx, g.dbConf.GetConnectTimeout())
	defer cancel()

	if err = g.conn.PingContext(lctx); err != nil {
		return fmt.Errorf("ping error: %w", err)
	}

	llog.Debug().Msg("genericQuery connected")

	return nil
}

func (g *genericLoader) getConnection(ctx context.Context) (*sqlx.Conn, error) {
	l := log.Ctx(ctx)
	l.Debug().Interface("conn", g.conn).Msg("conn")

	if g.conn == nil {
		// connect to database if not connected
		if err := g.openConnection(ctx); err != nil {
			atomic.AddUint32(&g.totalFailedConnections, 1)

			return nil, fmt.Errorf("open connection error: %w", err)
		}
	}

	conn, err := g.conn.Connx(ctx)
	if err != nil {
		atomic.AddUint32(&g.totalFailedConnections, 1)

		return nil, fmt.Errorf("get connection error: %w", err)
	}

	atomic.AddUint32(&g.totalOpenedConnections, 1)

	// launch initial sqls if defined
	for _, sql := range g.initialSQL {
		l.Debug().Str("sql", sql).Msg("genericQuery execute initial sql")

		lctx, cancel := context.WithTimeout(ctx, g.dbConf.GetConnectTimeout())
		defer cancel()

		if _, err := conn.QueryxContext(lctx, sql); err != nil {
			conn.Close()

			return nil, fmt.Errorf("execute initial sql error: %w", err)
		}
	}

	return conn, nil
}

// Query get data from database.
func (g *genericLoader) Query(ctx context.Context, q *conf.Query, params map[string]string,
) (*QueryResult, error) {
	var err error

	llog := log.Ctx(ctx).With().Str("db", g.dbConf.Name).Str("query", q.Name).Logger()
	ctx = llog.WithContext(ctx)

	conn, err := g.getConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	g.lock.RLock()
	defer g.lock.RUnlock()

	// prepare query parameters; combine parameters from query and params
	queryParams := prepareParams(q, params)
	timeout := g.queryTimeout(q)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	llog.Debug().Dur("timeout", timeout).Str("sql", q.SQL).Interface("params", q.Params).
		Msg("genericQuery start execute")

	tx, err := conn.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("prepare tx error: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	// query
	rows, err := tx.NamedQuery(q.SQL, queryParams)
	if err != nil {
		return nil, fmt.Errorf("execute query error: %w", err)
	}

	if cols, err := rows.Columns(); err == nil {
		llog.Debug().Interface("cols", cols).Msg("genericQuery columns")
	} else {
		return nil, fmt.Errorf("get columns error: %w", err)
	}

	result := &QueryResult{Start: time.Now(), Params: queryParams}

	// load records
	for rows.Next() {
		rec, err := newRecord(rows)
		if err != nil {
			return nil, err
		}

		result.Records = append(result.Records, rec)
	}

	result.Duration = float64(time.Since(result.Start).Seconds())

	return result, nil
}

// Close database connection.
func (g *genericLoader) Close(ctx context.Context) error {
	if g.conn == nil {
		return nil
	}

	// lock loader for write
	g.lock.Lock()
	defer g.lock.Unlock()

	log.Ctx(ctx).Debug().Interface("conn", g.conn).
		Str("db", g.dbConf.Name).Msg("genericQuery close conn")

	err := g.conn.Close()
	g.conn = nil

	if err != nil {
		return fmt.Errorf("close database errors: %w", err)
	}

	return nil
}

func (g *genericLoader) ConfChanged(db *conf.Database) bool {
	return !reflect.DeepEqual(g.dbConf, db)
}

func (g *genericLoader) String() string {
	return fmt.Sprintf("genericLoader name='%s' driver='%s' connstr='%v' connected=%v",
		g.dbConf.Name, g.driver, g.connStr, g.conn != nil)
}

func (g *genericLoader) queryTimeout(q *conf.Query) time.Duration {
	if q.Timeout > 0 {
		return time.Duration(q.Timeout) * time.Second
	}

	if g.dbConf.Timeout > 0 {
		return time.Duration(g.dbConf.Timeout) * time.Second
	}

	return 5 * time.Minute
}

func prepareParams(q *conf.Query, params map[string]string) map[string]interface{} {
	p := make(map[string]interface{})

	if q.Params != nil {
		for k, v := range q.Params {
			p[k] = v
		}
	}

	for k, v := range params {
		p[k] = v
	}

	return p
}
