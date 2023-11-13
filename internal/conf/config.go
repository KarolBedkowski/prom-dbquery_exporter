//
// config.go

package conf

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"
	"prom-dbquery_exporter.app/internal/support"
)

// MissingFieldError is error generated when `field` is missing in configuration.
type MissingFieldError struct {
	Field string
}

func (e MissingFieldError) Error() string {
	return fmt.Sprintf("missing field %s", e.Field)
}

// InvalidFieldError is error generated when validation of `field` with `value` failed.
type InvalidFieldError struct {
	Field   string
	Value   any
	Message string
}

func (e InvalidFieldError) Error() string {
	res := "invalid " + e.Field

	if e.Value != nil {
		res += fmt.Sprintf(" (%v)", e.Value)
	}

	if e.Message != "" {
		res += ": " + e.Message
	}

	return res
}

// WithMsg add message to InvalidFieldError.
func (e InvalidFieldError) WithMsg(msg string) InvalidFieldError {
	return InvalidFieldError{e.Field, e.Value, msg}
}

// NewInvalidFieldError create InvalidFieldError.
func NewInvalidFieldError(field string, value any) InvalidFieldError {
	return InvalidFieldError{field, value, ""}
}

// PoolConfiguration configure database connection pool.
type PoolConfiguration struct {
	MaxConnections     int `yaml:"max_connections"`
	MaxIdleConnections int `yaml:"max_idle_connections"`
	ConnMaxLifeTime    int `yaml:"conn_max_life_time"`
}

func (p *PoolConfiguration) validate() error {
	if p.MaxConnections < 0 {
		return NewInvalidFieldError("max_connections", p.MaxConnections)
	}

	if p.MaxIdleConnections < 0 {
		return NewInvalidFieldError("max_idle_connections", p.MaxIdleConnections)
	}

	if p.ConnMaxLifeTime < 0 {
		return NewInvalidFieldError("conn_max_life_time", p.MaxIdleConnections)
	}

	return nil
}

// Database define database connection.
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

	Pool *PoolConfiguration `yaml:"pool"`

	// Database name for internal use
	Name string `yaml:"-"`
}

func (d *Database) validate() error {
	if d.Driver == "" {
		return MissingFieldError{"driver"}
	}

	if d.Pool != nil {
		if err := d.Pool.validate(); err != nil {
			return err
		}
	}

	switch d.Driver {
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		if d.CheckConnectionParam("connstr") {
			return nil
		}

		if !d.CheckConnectionParam("database") && !d.CheckConnectionParam("dbname") {
			return MissingFieldError{"'database' or 'dbname'"}
		}

		if !d.CheckConnectionParam("user") {
			return MissingFieldError{"user"}
		}
	case "mysql", "mariadb", "tidb", "oracle", "oci8":
		for _, k := range []string{"database", "host", "port", "user", "password"} {
			if !d.CheckConnectionParam(k) {
				return MissingFieldError{k}
			}
		}
	case "sqlite3", "sqlite", "mssql":
		if !d.CheckConnectionParam("database") {
			return MissingFieldError{"database"}
		}
	default:
		return NewInvalidFieldError("database", "d.Driver").WithMsg("unknown database")
	}

	if port, ok := d.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			return NewInvalidFieldError("port", port)
		}
	}

	return nil
}

// CheckConnectionParam return true when parameter with key exists
// and is not empty.
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

// Query is definition of single query.
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

	// Groups define group names that query belong to
	Groups []string `yaml:"groups"`

	// Parsed template  (internal)
	MetricTpl *template.Template `yaml:"-"`
	// Query name for internal use
	Name string `yaml:"-"`
}

func (q *Query) validate() error {
	if q.SQL == "" {
		return MissingFieldError{"sql"}
	}

	m := strings.TrimSpace(q.Metrics) + "\n"
	if m == "" {
		return MissingFieldError{"metrics template"}
	}

	tmpl, err := support.TemplateCompile(q.Name, m)
	if err != nil {
		return NewInvalidFieldError("metrics template", "").WithMsg(err.Error())
		// fmt.Errorf("parsing error: %w", err))
	}

	q.MetricTpl = tmpl

	return nil
}

// Configuration keep application configuration.
type Configuration struct {
	// Databases
	Database map[string]*Database
	// Queries
	Query map[string]*Query
}

// GroupQueries return queries that belong to given group.
func (c *Configuration) GroupQueries(group string) []string {
	var queries []string
outerloop:
	for name, q := range c.Query {
		for _, gr := range q.Groups {
			if gr == group {
				queries = append(queries, name)
				continue outerloop
			}
		}
	}

	return queries
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

// LoadConfiguration from filename.
func LoadConfiguration(filename string) (*Configuration, error) {
	conf := &Configuration{}

	b, err := os.ReadFile(filename) // #nosec
	if err != nil {
		return nil, fmt.Errorf("read file error: %w", err)
	}

	if err = yaml.Unmarshal(b, conf); err != nil {
		return nil, fmt.Errorf("unmarshal file error: %w", err)
	}

	if err = conf.validate(); err != nil {
		return nil, fmt.Errorf("validate error: %w", err)
	}

	for name, db := range conf.Database {
		db.Name = name
	}

	for name, q := range conf.Query {
		q.Name = name
	}

	return conf, nil
}

const defaultConnectioTimeout = 15 * time.Second

// GetConnectTimeout return connection timeout from configuration or default.
func (d *Database) GetConnectTimeout() time.Duration {
	if d.ConnectTimeout > 0 {
		return time.Duration(d.ConnectTimeout) * time.Second
	}

	return defaultConnectioTimeout
}
