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

type (
	// Query is definition of single query
	Query struct {
		// SQL script to launch
		SQL string
		// Template to generate from query result
		Metrics string
		// Query params
		Params map[string]interface{}
		// Result caching time
		CachingTime int `yaml:"caching_time"`
		// Parsed template  (internal)
		MetricTpl *template.Template `yaml:"-"`

		// Max time for query result
		Timeout int `yaml:"timeout"`

		// Query name for internal use
		Name string `yaml:"-"`
	}

	// Database define database connection
	Database struct {
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
		Timeout int `yaml:"timeout"`

		// Connection and ping timeout
		ConnectTimeout uint `yaml:"connect_timeout"`

		// Database name for internal use
		Name string `yaml:"-"`
	}

	// Configuration keep application configuration
	Configuration struct {
		// Databases
		Database map[string]*Database
		// Queries
		Query map[string]*Query
	}
)

func (c *Configuration) validate() error {
	if len(c.Database) == 0 {
		return errors.New("no database configured")
	}

	if len(c.Query) == 0 {
		return errors.New("no query configured")
	}

	for name, query := range c.Query {
		if query.SQL == "" {
			return fmt.Errorf("missing SQL for query '%s'", name)
		}
		m := strings.TrimSpace(query.Metrics) + "\n"
		if m == "" {
			return fmt.Errorf("missing metrics for query '%s'", name)
		}
		tmpl, err := template.New("main").Funcs(templateFuncsMap).Parse(m)
		if err != nil {
			return fmt.Errorf("parsing metrics template for query '%s' error: %w",
				name, err)
		}
		query.MetricTpl = tmpl
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
