//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/common/log"
)

type (
	// Record is one record (row) loaded from database
	Record map[string]interface{}

	// Loader load data from database
	Loader interface {
		// Query execute sql and returns records or error. Open connection when necessary.
		Query(q *Query, params map[string]string) (*queryResult, error)
		// Close db connection.
		Close()
	}

	queryResult struct {
		records  []Record
		duration float64
		start    time.Time
		params   map[string]interface{}
	}

	genericLoader struct {
		connStr string
		driver  string
		conn    *sqlx.DB
	}
)

func (g *genericLoader) Query(q *Query, params map[string]string) (*queryResult, error) {
	var err error

	if g.conn != nil {
		// test existing connection
		err = g.conn.Ping()
		if err != nil {
			g.conn.Close()
			g.conn = nil
		}
	}

	// connect to database
	if g.conn == nil {
		log.With("driver", g.driver).
			Debugf("genericQuery connect to '%s'", g.connStr)

		g.conn, err = sqlx.Connect(g.driver, g.connStr)
		if err != nil {
			return nil, err
		}
	}

	log.With("driver", g.driver).
		Debugf("genericQuery execute '%v', '%v'", q.SQL, q.Params)

	p := make(map[string]interface{})
	if q.Params != nil {
		for k, v := range q.Params {
			p[k] = v
		}
	}
	if params != nil {
		for k, v := range params {
			p[k] = v
		}
	}

	result := &queryResult{
		start:  time.Now(),
		params: p,
	}

	var rows *sqlx.Rows
	// query
	if len(p) > 0 {
		rows, err = g.conn.NamedQuery(q.SQL, p)
	} else {
		rows, err = g.conn.Queryx(q.SQL)
	}

	if err != nil {
		return nil, err
	}

	if cols, err := rows.Columns(); err == nil {
		log.With("driver", g.driver).
			Debugf("genericQuery columns: %v", cols)
	} else {
		return nil, err
	}

	// load records
	for rows.Next() {
		rec := Record{}
		if err := rows.MapScan(rec); err != nil {
			return nil, err
		}

		// convert []byte to string
		for k, v := range rec {
			switch v.(type) {
			case []byte:
				rec[k] = string(v.([]byte))
			}
		}
		result.records = append(result.records, rec)
	}

	result.duration = float64(time.Since(result.start).Seconds())
	return result, nil
}

// Close database connection
func (g *genericLoader) Close() {
	if g.conn != nil {
		log.With("driver", g.driver).Debugf("genericQuery disconnect from %s'", g.connStr)
		g.conn.Close()
		g.conn = nil
	}
}

func (g *genericLoader) String() string {
	return fmt.Sprintf("genericLoader driver='%s' connstr='%v' connected=%v",
		g.driver, g.connStr, g.conn != nil)
}

func newPostgresLoader(d *Database) (Loader, error) {
	p := make([]string, 0, len(d.Connection))
	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("'%v'", v)
		}
		p = append(p, k+"="+vstr)
	}

	l := &genericLoader{
		connStr: strings.Join(p, " "),
		driver:  "postgres",
	}

	log.Debugf("created loader: %s", l.String())

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
		return nil, fmt.Errorf("missing database")
	}

	l := &genericLoader{connStr: dbname, driver: "sqlite3"}
	if len(p) > 0 {
		l.connStr += "?" + p.Encode()
	}
	log.Debugf("created loader: %s", l.String())

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
		return nil, fmt.Errorf("missing database")
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

	l := &genericLoader{connStr: connstr, driver: "mysql"}
	log.Debugf("created loader: %s", l.String())

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
		return nil, fmt.Errorf("missing database")
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

	l := &genericLoader{connStr: connstr, driver: "oci8"}
	log.Debugf("created loader: %s", l.String())

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
		return nil, fmt.Errorf("missing database")
	}

	connstr := p.Encode()

	l := &genericLoader{connStr: connstr, driver: "mssql"}
	log.Debugf("created loader: %s", l.String())

	return l, nil
}

// GetLoader returns configured Loader for given configuration.
func GetLoader(d *Database) (Loader, error) {
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
		return newMysqlLoader(d)
	}
	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}
