//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package db

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/support"
)

const (
	defaultTimeout         uint = 300 // sec
	defaultConnMaxLifetime      = 600 * time.Second
	defaultMaxOpenConns         = 10
	defaultMaxIdleConns         = 2
)

type genericLoader struct {
	connStr                string
	driver                 string
	conn                   *sqlx.DB
	initialSQL             []string
	dbConf                 *conf.Database
	lock                   sync.RWMutex
	totalOpenedConnections uint32
	totalFailedConnections uint32
}

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
	g.conn.SetConnMaxLifetime(defaultConnMaxLifetime)
	g.conn.SetMaxOpenConns(defaultMaxOpenConns)
	g.conn.SetMaxIdleConns(defaultMaxIdleConns)

	if pool := g.dbConf.Pool; pool != nil {
		llog := log.Ctx(ctx)

		if pool.MaxConnections > 0 {
			llog.Debug().Int("max-conn", pool.MaxConnections).Msg("max connection set")
			g.conn.SetMaxOpenConns(pool.MaxConnections)
		}

		if pool.MaxIdleConnections > 0 {
			llog.Debug().Int("max-idle", pool.MaxIdleConnections).Msg("max idle connection set")
			g.conn.SetMaxIdleConns(pool.MaxIdleConnections)
		}

		if pool.ConnMaxLifeTime > 0 {
			llog.Debug().Int("conn-max-life-time", pool.ConnMaxLifeTime).
				Msg("connection max life time set")
			g.conn.SetConnMaxLifetime(time.Duration(pool.ConnMaxLifeTime) * time.Second)
		}
	}
}

func (g *genericLoader) openConnection(ctx context.Context) error {
	// lock loader for write
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.conn != nil {
		return nil
	}

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
	llog := log.Ctx(ctx)
	llog.Debug().Interface("conn", g.conn).Msg("conn")

	// connect to database if not connected
	if err := g.openConnection(ctx); err != nil {
		atomic.AddUint32(&g.totalFailedConnections, 1)

		return nil, fmt.Errorf("open connection error: %w", err)
	}

	conn, err := g.conn.Connx(ctx)
	if err != nil {
		atomic.AddUint32(&g.totalFailedConnections, 1)

		return nil, fmt.Errorf("get connection error: %w", err)
	}

	atomic.AddUint32(&g.totalOpenedConnections, 1)

	// launch initial sqls if defined
	for idx, sql := range g.initialSQL {
		llog.Debug().Str("sql", sql).Msg("genericQuery execute initial sql")

		if err := g.executeInitialQuery(ctx, sql, conn); err != nil {
			conn.Close()

			return nil, fmt.Errorf("execute initial sql [%d] error: %w", idx, err)
		}
	}

	return conn, nil
}

func (g *genericLoader) executeInitialQuery(ctx context.Context, sql string, conn *sqlx.Conn) error {
	lctx, cancel := context.WithTimeout(ctx, g.dbConf.GetConnectTimeout())
	defer cancel()

	rows, err := conn.QueryxContext(lctx, sql)
	if rows != nil {
		defer rows.Close()
	}

	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	return nil
}

// Query get data from database.
func (g *genericLoader) Query(ctx context.Context, query *conf.Query, params map[string]string,
) (*QueryResult, error) {
	llog := log.Ctx(ctx).With().Str("db", g.dbConf.Name).Str("query", query.Name).Logger()
	ctx = llog.WithContext(ctx)
	result := &QueryResult{Start: time.Now()}

	support.TracePrintf(ctx, "db: opening connection")

	conn, err := g.getConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("get connection error: %w", err)
	}
	defer conn.Close()

	g.lock.RLock()
	defer g.lock.RUnlock()

	// prepare query parameters; combine parameters from query and params
	queryParams := prepareParams(query, params)
	timeout := g.queryTimeout(query)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	llog.Debug().Dur("timeout", timeout).Str("sql", query.SQL).Interface("params", queryParams).
		Msg("genericQuery start execute")

	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx error: %w", err)
	}

	defer tx.Rollback() //nolint:errcheck

	support.TracePrintf(ctx, "db: begin query %q", query.Name)

	rows, err := tx.NamedQuery(query.SQL, queryParams)
	if err != nil {
		return nil, fmt.Errorf("execute query error: %w", err)
	}

	if e := llog.Debug(); e.Enabled() {
		if cols, err := rows.Columns(); err == nil {
			e.Interface("cols", cols).Msg("genericQuery columns")
		} else {
			return nil, fmt.Errorf("get columns error: %w", err)
		}
	}

	result.Records, err = createRecords(rows)
	if err != nil {
		return nil, fmt.Errorf("create record error: %w", err)
	}

	result.Params = queryParams
	result.Duration = time.Since(result.Start).Seconds()

	return result, nil
}

func createRecords(rows *sqlx.Rows) ([]Record, error) {
	var records []Record

	for rows.Next() {
		rec, err := newRecord(rows)
		if err != nil {
			return nil, err
		}

		records = append(records, rec)
	}

	return records, nil
}

// Close database connection.
func (g *genericLoader) Close(ctx context.Context) error {
	// lock loader for write
	g.lock.Lock()
	defer g.lock.Unlock()

	log.Ctx(ctx).Debug().Interface("conn", g.conn).
		Str("db", g.dbConf.Name).Msg("genericQuery close conn")

	if g.conn == nil {
		return nil
	}

	err := g.conn.Close()
	g.conn = nil

	if err != nil {
		return fmt.Errorf("close database errors: %w", err)
	}

	return nil
}

func (g *genericLoader) UpdateConf(db *conf.Database) bool {
	g.dbConf = db

	return true
}

func (g *genericLoader) String() string {
	return fmt.Sprintf("genericLoader name='%s' driver='%s' connstr='%v' connected=%v",
		g.dbConf.Name, g.driver, g.connStr, g.conn != nil)
}

func (g *genericLoader) queryTimeout(q *conf.Query) time.Duration {
	timeout := defaultTimeout

	switch {
	case q.Timeout > 0:
		timeout = q.Timeout
	case g.dbConf.Timeout > 0:
		timeout = g.dbConf.Timeout
	}

	return time.Duration(timeout) * time.Second
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
