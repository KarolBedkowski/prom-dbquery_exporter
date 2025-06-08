//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package db

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/debug"
)

const (
	defaultTimeout = time.Duration(300) * time.Second // sec
)

type genericDatabase struct {
	conn    *sqlx.DB
	dbcfg   *conf.Database
	connstr string
	driver  string

	mu sync.Mutex
}

func newGenericDatabase(connstr, driver string, dbcfg *conf.Database) Database {
	return &genericDatabase{ //nolint:exhaustruct
		connstr: connstr,
		driver:  driver,
		dbcfg:   dbcfg,
	}
}

func (g *genericDatabase) CollectMetrics(resCh chan<- prometheus.Metric) { //nolint:funlen
	if g.conn == nil {
		return
	}

	name := g.dbcfg.Name
	stat := g.conn.Stats()

	resCh <- prometheus.MustNewConstMetric(
		dbpoolOpenConnsDesc,
		prometheus.GaugeValue,
		float64(stat.OpenConnections),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolActConnsDesc,
		prometheus.GaugeValue,
		float64(stat.InUse),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolIdleConnsDesc,
		prometheus.GaugeValue,
		float64(stat.Idle),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolconfMaxConnsDesc,
		prometheus.GaugeValue,
		float64(stat.MaxOpenConnections),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolConnWaitCntDesc,
		prometheus.CounterValue,
		float64(stat.WaitCount),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolConnIdleClosedDesc,
		prometheus.CounterValue,
		float64(stat.MaxIdleClosed),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolConnIdleTimeClosedDesc,
		prometheus.CounterValue,
		float64(stat.MaxIdleTimeClosed),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolConnLifeTimeClosedDesc,
		prometheus.CounterValue,
		float64(stat.MaxLifetimeClosed),
		name,
	)
	resCh <- prometheus.MustNewConstMetric(
		dbpoolConnWaitTimeDesc,
		prometheus.CounterValue,
		stat.WaitDuration.Seconds(),
		name,
	)
}

// Query get data from database.
func (g *genericDatabase) Query(ctx context.Context, query *conf.Query, params map[string]any,
) (*QueryResult, error) {
	llog := loggerFromCtx(ctx).With().Str("db", g.dbcfg.Name).Str("query", query.Name).Logger()
	ctx = llog.WithContext(ctx)
	startts := time.Now()

	debug.TracePrintf(ctx, "db: opening connection")

	conn, err := g.getConnection(ctx)
	if err != nil {
		dbpoolConnFailedTotal.WithLabelValues(g.dbcfg.Name).Inc()

		return nil, fmt.Errorf("get connection error: %w", err)
	}
	defer conn.Close()

	// prepare query parameters; combine parameters from query and params
	queryParams := cloneMap(query.Params, params)
	timeout := g.queryTimeout(query)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	llog.Debug().Str("sql", query.SQL).Interface("params", queryParams).
		Msgf("db: generic: start execute query with timeout=%s", timeout)

	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx error: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	debug.TracePrintf(ctx, "db: begin query %q", query.Name)

	rows, err := tx.NamedQuery(query.SQL, queryParams)
	if err != nil {
		return nil, fmt.Errorf("execute query error: %w", err)
	}

	if err := g.logColumns(rows, llog); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	records, err := recordsFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("create record error: %w", err)
	}

	return newQueryResult(startts, queryParams, records), nil
}

// Close database connection.
func (g *genericDatabase) Close(ctx context.Context) error {
	// lock loader for write
	g.mu.Lock()
	defer g.mu.Unlock()

	log.Ctx(ctx).Debug().Interface("conn", g.conn).Str("db", g.dbcfg.Name).Msg("db: generic: close conn")

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

func (g *genericDatabase) String() string {
	return fmt.Sprintf("genericDatabase: name=%q driver=%q connstr=%q connected=%v",
		g.dbcfg.Name, g.driver, g.connstr, g.conn != nil)
}

func (g *genericDatabase) configure(ctx context.Context) {
	llog := log.Ctx(ctx)

	pool := g.dbcfg.Pool
	if pool == nil {
		llog.Warn().Msg("no pool configuration; using defaults")

		return
	}

	if pool.MaxConnections > 0 {
		llog.Debug().Msgf("db: max connection set to %d", pool.MaxConnections)
		g.conn.SetMaxOpenConns(pool.MaxConnections)
	}

	if pool.MaxIdleConnections > 0 {
		llog.Debug().Msgf("db: max idle connection set to %d", pool.MaxIdleConnections)
		g.conn.SetMaxIdleConns(pool.MaxIdleConnections)
	}

	if pool.ConnMaxLifeTime > 0 {
		llog.Debug().Msgf("db: connection max life time set to %s", pool.ConnMaxLifeTime)
		g.conn.SetConnMaxLifetime(pool.ConnMaxLifeTime)
	}
}

func (g *genericDatabase) openConnection(ctx context.Context) error {
	// lock loader for write
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.conn != nil {
		return nil
	}

	llog := log.Ctx(ctx)
	llog.Debug().Str("connstr", g.connstr).Str("driver", g.driver).Msg("db: generic: connecting")

	var err error
	if g.conn, err = sqlx.Open(g.driver, g.connstr); err != nil {
		return fmt.Errorf("open error: %w", err)
	}

	g.configure(ctx)

	// check is database is working
	if err := g.ping(ctx); err != nil {
		_ = g.conn.Close()
		g.conn = nil

		return err
	}

	llog.Debug().Msg("db: generic: connected")

	return nil
}

func (g *genericDatabase) ping(ctx context.Context) error {
	lctx, cancel := context.WithTimeout(ctx, g.dbcfg.ConnectTimeout)
	defer cancel()

	if err := g.conn.PingContext(lctx); err != nil {
		return fmt.Errorf("ping error: %w", err)
	}

	return nil
}

func (g *genericDatabase) getConnection(ctx context.Context) (*sqlx.Conn, error) {
	llog := log.Ctx(ctx)

	// connect to database if not connected
	if err := g.openConnection(ctx); err != nil {
		dbpoolConnFailedTotal.WithLabelValues(g.dbcfg.Name).Inc()

		return nil, fmt.Errorf("open connection error: %w", err)
	}

	conn, err := g.conn.Connx(ctx)
	if err != nil {
		dbpoolConnFailedTotal.WithLabelValues(g.dbcfg.Name).Inc()

		return nil, fmt.Errorf("get connection error: %w", err)
	}

	dbpoolConnOpenedTotal.WithLabelValues(g.dbcfg.Name).Inc()

	// launch initial sqls if defined
	for idx, sql := range g.dbcfg.InitialQuery {
		llog.Debug().Msgf("db: generic: execute initial sql: %q", sql)
		debug.TracePrintf(ctx, "db: execute initial sql")

		if err := g.executeInitialQuery(ctx, sql, conn); err != nil {
			conn.Close()

			return nil, fmt.Errorf("execute initial sql [%d] error: %w", idx, err)
		}
	}

	return conn, nil
}

func (g *genericDatabase) executeInitialQuery(ctx context.Context, sql string, conn *sqlx.Conn) error {
	timeout := defaultTimeout
	if g.dbcfg.Timeout > 0 {
		timeout = g.dbcfg.Timeout
	}

	lctx, cancel := context.WithTimeout(ctx, timeout)
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

func (g *genericDatabase) queryTimeout(q *conf.Query) time.Duration {
	timeout := defaultTimeout

	switch {
	case q.Timeout > 0:
		timeout = q.Timeout
	case g.dbcfg.Timeout > 0:
		timeout = g.dbcfg.Timeout
	}

	return timeout
}

func (g *genericDatabase) logColumns(rows *sqlx.Rows, llog zerolog.Logger) error {
	if e := llog.Debug(); e.Enabled() {
		if cols, err := rows.Columns(); err == nil {
			e.Msgf("db: generic: columns from query: %+v", cols)
		} else {
			return fmt.Errorf("get columns error: %w", err)
		}
	}

	return nil
}

//---------------------------------------------------------------

func loggerFromCtx(ctx context.Context) zerolog.Logger {
	if llog := log.Ctx(ctx); llog != nil {
		return *llog
	}

	return log.Logger
}

// cloneMap create clone of `inp` map and optionally update it with values
// from extra maps.
func cloneMap[K comparable, V any](inp map[K]V, extra ...map[K]V) map[K]V {
	res := make(map[K]V, len(inp)+1)
	maps.Copy(res, inp)

	for _, e := range extra {
		maps.Copy(res, e)
	}

	return res
}
