//
// config.go

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"
)

// Database define database connection
type Database struct {
	// Driver name - see sql-agent
	Driver string
	// Connection params - see sql-agent
	Connection map[string]interface{}
	// Labels configured per database; may be used in templates
	Labels map[string]interface{}
	// InitialQuery allow run custom query or set some parameters after
	// connect and before target query
	InitialQuery []string `yaml:"initial_query"`

	// Default timeout for all queries
	Timeout uint `yaml:"timeout"`

	// Connection and ping timeout
	ConnectTimeout uint `yaml:"connect_timeout"`

	// Database name for internal use
	Name string `yaml:"-"`

	// Timestamp is configuration load time; internal
	Timestamp time.Time `yaml:"-"`
}

func (d *Database) validate() error {
	if d.Driver == "" {
		return errors.New("missing driver")
	}

	switch d.Driver {
	case "postgresql":
	case "postgres":
		if d.CheckConnectionParam("connstr") {
			return nil
		}
		if !d.CheckConnectionParam("database") && !d.CheckConnectionParam("dbname") {
			return fmt.Errorf("missing 'database' or 'dbname' parameter")
		}
		if !d.CheckConnectionParam("user") {
			return fmt.Errorf("missing 'user' parameter")
		}
	case "sqlite3":
	case "sqlite":
	case "mysql":
	case "mariadb":
	case "tidb":
	case "oracle":
	case "oci8":
		for _, k := range []string{"database", "host", "port", "user", "password"} {
			if !d.CheckConnectionParam(k) {
				return fmt.Errorf("missing '%s' parameter", k)
			}
		}
	case "mssql":
		if !d.CheckConnectionParam("database") {
			return errors.New("missing 'database' parameter")
		}
	default:
		return fmt.Errorf("unknown database '%s'", d.Driver)
	}

	if port, ok := d.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			return errors.New("invalid 'port'")
		}
	}

	return nil
}

// CheckConnectionParam return true when parameter with key exists
// and is not empty
func (d *Database) CheckConnectionParam(key string) bool {
	val, ok := d.Connection[key]
	if !ok {
		return false
	}
	switch val := val.(type) {
	case string:
		return strings.TrimSpace(val) != ""
	default:
	}

	return true
}

// Query is definition of single query
type Query struct {
	// SQL script to launch
	SQL string
	// Template to generate from query result
	Metrics string
	// Query params
	Params map[string]interface{}
	// Result caching time
	CachingTime uint `yaml:"caching_time"`
	// Max time for query result
	Timeout uint `yaml:"timeout"`

	// Parsed template  (internal)
	MetricTpl *template.Template `yaml:"-"`
	// Query name for internal use
	Name string `yaml:"-"`
}

func (q *Query) validate() error {
	if q.SQL == "" {
		return errors.New("missing SQL")
	}

	m := strings.TrimSpace(q.Metrics) + "\n"
	if m == "" {
		return errors.New("missing or empty metrics template")
	}

	tmpl, err := template.New("main").Funcs(templateFuncsMap).Parse(m)
	if err != nil {
		return fmt.Errorf("parsing metrics template error: %w", err)
	}
	q.MetricTpl = tmpl

	return nil
}

// Configuration keep application configuration
type Configuration struct {
	// Databases
	Database map[string]*Database
	// Queries
	Query map[string]*Query
}

func (c *Configuration) validate() error {
	if len(c.Database) == 0 {
		return errors.New("no database configured")
	}

	if len(c.Query) == 0 {
		return errors.New("no query configured")
	}

	for name, query := range c.Query {
		if err := query.validate(); err != nil {
			return fmt.Errorf("validate query '%s' error: %w", name, err)
		}
	}

	for name, db := range c.Database {
		if err := db.validate(); err != nil {
			return fmt.Errorf("validate database '%s' error: %w", name, err)
		}

	}
	return nil
}

func loadConfiguration(filename string) (*Configuration, error) {
	c := &Configuration{}
	b, err := ioutil.ReadFile(filename) // #nosec

	if err != nil {
		return nil, fmt.Errorf("read file error: %w", err)
	}

	if err = yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("unmarshal file error: %w", err)
	}

	if err = c.validate(); err != nil {
		return nil, fmt.Errorf("validate error: %w", err)
	}

	for name, db := range c.Database {
		db.Name = name
		db.Timestamp = time.Now()
	}

	for name, q := range c.Query {
		q.Name = name
	}

	return c, nil
}

func (d *Database) connectTimeout() time.Duration {
	if d.ConnectTimeout > 0 {
		return time.Duration(d.ConnectTimeout) * time.Second
	}

	return 15 * time.Second
}
