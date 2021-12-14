//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/rs/zerolog/log"
)

// Record is one record (row) loaded from database
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
	// Loader load data from database
	Loader interface {
		// Query execute sql and returns records or error. Open connection when necessary.
		Query(ctx context.Context, q *Query, params map[string]string) (*QueryResult, error)
		// Close db connection.
		Close(ctx context.Context) error
		// Human-friendly info
		String() string
		// ConfChanged return true when given configuration is differ than used
		ConfChanged(db *Database) bool
	}

	// QueryResult is result of Loader.Query
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
		connStr    string
		driver     string
		conn       *sqlx.DB
		initialSQL []string
		dbConf     *Database
		lock       sync.RWMutex
	}
)

func (g *genericLoader) openConnection(ctx context.Context) (err error) {
	// lock loader for write
	g.lock.Lock()
	defer g.lock.Unlock()

	l := log.Ctx(ctx)
	l.Debug().Str("connstr", g.connStr).Str("driver", g.driver).
		Msg("genericQuery connecting")

	if g.conn, err = sqlx.Open(g.driver, g.connStr); err != nil {
		return fmt.Errorf("open error: %w", err)
	}

	g.conn.SetConnMaxLifetime(600 * time.Second)
	g.conn.SetMaxOpenConns(10)
	g.conn.SetMaxIdleConns(1)

	if p := g.dbConf.Pool; p != nil {
		if p.MaxConnections > 0 {
			l.Debug().Int("max-conn", p.MaxConnections).Msg("max connection set")
			g.conn.SetMaxOpenConns(p.MaxConnections)
		}
		if p.MaxIdleConnections > 0 {
			l.Debug().Int("max-idle", p.MaxIdleConnections).Msg("max idle connection set")
			g.conn.SetMaxIdleConns(p.MaxIdleConnections)
		}
		if p.ConnMaxLifeTime > 0 {
			l.Debug().Int("conn-max-life-time", p.ConnMaxLifeTime).Msg("connection max life time set")
			g.conn.SetConnMaxLifetime(time.Duration(p.ConnMaxLifeTime) * time.Second)
		}
	}

	// check is database is working
	lctx, cancel := context.WithTimeout(ctx, g.dbConf.connectTimeout())
	defer cancel()
	if err := g.conn.PingContext(lctx); err != nil {
		return fmt.Errorf("ping error: %w", err)
	}

	l.Debug().Msg("genericQuery connected")
	return nil
}

func (g *genericLoader) getConnection(ctx context.Context) (*sqlx.Conn, error) {
	l := log.Ctx(ctx)
	l.Debug().Interface("conn", g.conn).Msg("conn")

	if g.conn == nil {
		// connect to database if not connected
		if err := g.openConnection(ctx); err != nil {
			return nil, fmt.Errorf("open connection error: %w", err)
		}
	}

	conn, err := g.conn.Connx(ctx)
	if err != nil {
		return nil, fmt.Errorf("get connection error: %w", err)
	}
	// launch initial sqls if defined
	for _, sql := range g.initialSQL {
		l.Debug().Str("sql", sql).Msg("genericQuery execute initial sql")
		lctx, cancel := context.WithTimeout(ctx, g.dbConf.connectTimeout())
		defer cancel()
		if _, err := conn.QueryxContext(lctx, sql); err != nil {
			conn.Close()
			return nil, fmt.Errorf("execute initial sql error: %w", err)
		}
	}

	return conn, nil
}

// Query get data from database
func (g *genericLoader) Query(ctx context.Context, q *Query, params map[string]string) (*QueryResult, error) {
	var err error
	l := log.Ctx(ctx).With().Str("db", g.dbConf.Name).Str("query", q.Name).Logger()
	ctx = l.WithContext(ctx)

	conn, err := g.getConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	g.lock.RLock()
	defer g.lock.RUnlock()

	// prepare query parameters; combine parameters from query and params
	p := prepareParams(q, params)
	result := &QueryResult{
		Start:  time.Now(),
		Params: p,
	}

	timeout := g.queryTimeout(q)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	l.Debug().Dur("timeout", timeout).Str("sql", q.SQL).Interface("params", q.Params).
		Msg("genericQuery start execute")

	var rows *sqlx.Rows
	// query
	if len(p) > 0 {
		// sqlx.Conn not support NamedQuery...
		sql, params, err2 := sqlx.Named(q.SQL, p)
		if err2 != nil {
			return nil, fmt.Errorf("prepare sql error: %w", err2)
		}
		rows, err = conn.QueryxContext(ctx, sql, params...)
	} else {
		rows, err = conn.QueryxContext(ctx, q.SQL)
	}

	if err != nil {
		return nil, fmt.Errorf("execute query error: %w", err)
	}

	if cols, err := rows.Columns(); err == nil {
		l.Debug().Interface("cols", cols).Msg("genericQuery columns")
	} else {
		return nil, fmt.Errorf("get columns error: %w", err)
	}

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

// Close database connection
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
	return err
}

func (g *genericLoader) ConfChanged(db *Database) bool {
	return !reflect.DeepEqual(g.dbConf, db)
}

func (g *genericLoader) String() string {
	return fmt.Sprintf("genericLoader name='%s' driver='%s' connstr='%v' connected=%v",
		g.dbConf.Name, g.driver, g.connStr, g.conn != nil)
}

func (g *genericLoader) queryTimeout(q *Query) time.Duration {
	if q.Timeout > 0 {
		return time.Duration(q.Timeout) * time.Second
	}

	if g.dbConf.Timeout > 0 {
		return time.Duration(g.dbConf.Timeout) * time.Second
	}

	return 5 * time.Minute
}

func newPostgresLoader(d *Database) (Loader, error) {
	var connStr string
	if val, ok := d.Connection["connstr"]; ok && val != "" {
		connStr = val.(string)
	} else {
		p := make([]string, 0, len(d.Connection))
		for k, v := range d.Connection {
			vstr := ""
			if v != nil {
				vstr = fmt.Sprintf("'%v'", v)
			}
			p = append(p, k+"="+vstr)
		}
		connStr = strings.Join(p, " ")
	}

	l := &genericLoader{
		connStr:    connStr,
		driver:     "postgres",
		initialSQL: d.InitialQuery,
		dbConf:     d,
	}
	return l, nil
}

func newSqliteLoader(d *Database) (Loader, error) {
	p := url.Values{}
	var dbname string
	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}
		if k == "database" {
			dbname = vstr
		} else {
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
	}

	l := &genericLoader{connStr: dbname, driver: "sqlite3", initialSQL: d.InitialQuery,
		dbConf: d,
	}
	if len(p) > 0 {
		l.connStr += "?" + p.Encode()
	}
	return l, nil
}

func newMysqlLoader(d *Database) (Loader, error) {
	p := url.Values{}
	host := "localhost"
	port := "3306"
	var dbname, user, pass string

	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}
		switch k {
		case "database":
			dbname = vstr
		case "host":
			host = vstr
		case "port":
			port = vstr
		case "user":
			user = vstr
		case "password":
			pass = vstr
		default:
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
	}

	var connstr string
	if user != "" {
		if pass != "" {
			connstr = user + ":" + pass + "@"
		} else {
			connstr = user + "@"
		}
	}
	connstr += "tcp(" + host + ":" + port + ")/" + dbname
	if len(p) > 0 {
		connstr += "?" + p.Encode()
	}

	l := &genericLoader{connStr: connstr, driver: "mysql", initialSQL: d.InitialQuery,
		dbConf: d,
	}
	return l, nil
}

func newOracleLoader(d *Database) (Loader, error) {
	p := url.Values{}
	var dbname, user, pass, host, port string
	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}
		switch k {
		case "database":
			dbname = vstr
		case "host":
			host = vstr
		case "port":
			port = vstr
		case "user":
			user = vstr
		case "password":
			pass = vstr
		default:
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
	}

	var connstr string
	if user != "" {
		if pass != "" {
			connstr = user + "/" + pass + "@"
		} else {
			connstr = user + "@"
		}
	}
	connstr += host
	if port != "" {
		connstr += ":" + port
	}
	connstr += "/" + dbname
	if len(p) > 0 {
		connstr += "?" + p.Encode()
	}

	l := &genericLoader{connStr: connstr, driver: "oci8", initialSQL: d.InitialQuery,
		dbConf: d,
	}
	return l, nil
}

func newMssqlLoader(d *Database) (Loader, error) {
	p := url.Values{}
	databaseConfigured := false
	for k, v := range d.Connection {
		if v != nil {
			vstr := fmt.Sprintf("%v", v)
			p.Add(k, vstr)
			if k == "database" {
				databaseConfigured = true
			}
		}
	}

	if !databaseConfigured {
		return nil, errors.New("missing database")
	}

	connstr := p.Encode()

	l := &genericLoader{connStr: connstr, driver: "mssql", initialSQL: d.InitialQuery,
		dbConf: d,
	}
	return l, nil
}

// newLoader returns configured Loader for given configuration.
func newLoader(d *Database) (Loader, error) {
	switch d.Driver {
	case "postgresql":
	case "postgres":
		return newPostgresLoader(d)
	case "sqlite3":
	case "sqlite":
		return newSqliteLoader(d)
	case "mysql":
	case "mariadb":
	case "tidb":
		return newMysqlLoader(d)
	case "oracle":
	case "oci8":
		return newOracleLoader(d)
	case "mssql":
		return newMssqlLoader(d)
	}
	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}

func prepareParams(q *Query, params map[string]string) map[string]interface{} {
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

// loadersPool keep database loaders
type loadersPool struct {
	// map of loader instances
	loaders map[string]Loader
	lock    sync.Mutex
}

var lp loadersPool = loadersPool{
	loaders: make(map[string]Loader),
}

// GetLoader create or return existing loader according to configuration
func GetLoader(d *Database) (Loader, error) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	if loader, ok := lp.loaders[d.Name]; ok {
		return loader, nil
	}

	Logger.Debug().Str("name", d.Name).Msg("creating new loader")
	loader, err := newLoader(d)
	if err == nil {
		lp.loaders[d.Name] = loader
	}

	return loader, err
}

// UpdateConfiguration update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func UpdateConfiguration(c *Configuration) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	logger := Logger
	ctx := logger.WithContext(context.Background())

	var dbToClose []string
	for k, l := range lp.loaders {
		if newConf, ok := c.Database[k]; !ok {
			dbToClose = append(dbToClose, k)
		} else if l.ConfChanged(newConf) {
			logger.Info().Str("db", k).Msg("configuration changed")
			dbToClose = append(dbToClose, k)
		}
	}

	for _, name := range dbToClose {
		l := lp.loaders[name]
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := l.Close(cctx); err != nil {
			logger.Error().Err(err).Msg("close loader error")
		}
		cancel()
		delete(lp.loaders, name)
	}
}

// CloseLoaders close all active loaders in pool
func CloseLoaders() {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	Logger.Debug().Interface("loaders", lp.loaders).Msg("")

	ctx := Logger.WithContext(context.Background())

	for _, l := range lp.loaders {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := l.Close(cctx); err != nil {
			Logger.Error().Err(err).Msg("close loader error")
		}
		cancel()
	}
}
