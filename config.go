//
// config.go

package main

import (
	"io/ioutil"
	"strings"
	"text/template"

	"github.com/pkg/errors"
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
		return errors.Errorf("no database configured")
	}

	if len(c.Query) == 0 {
		return errors.Errorf("no query configured")
	}

	for name, query := range c.Query {
		if query.SQL == "" {
			return errors.Errorf("missing SQL for query '%s'", name)
		}
		m := strings.TrimSpace(query.Metrics) + "\n"
		if m == "" {
			return errors.Errorf("missing metrics for query '%s'", name)
		}
		tmpl, err := template.New("main").Funcs(templateFuncsMap).Parse(m)
		if err != nil {
			return errors.Wrapf(err, "parsing metrics template for query '%s' error",
				name)
		}
		query.MetricTpl = tmpl
	}
	return nil
}

func loadConfiguration(filename string) (*Configuration, error) {
	c := &Configuration{}
	b, err := ioutil.ReadFile(filename)

	if err != nil {
		return nil, errors.Wrap(err, "read file error")
	}

	if err = yaml.Unmarshal(b, c); err != nil {
		return nil, errors.Wrap(err, "unmarshal file error")
	}

	if err = c.validate(); err != nil {
		return nil, errors.Wrap(err, "validate error")
	}

	return c, nil
}
