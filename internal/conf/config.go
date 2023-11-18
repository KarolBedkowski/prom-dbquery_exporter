//
// config.go

package conf

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

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
		return newConfigurationError("no database configured")
	}

	if len(c.Query) == 0 {
		return newConfigurationError("no query configured")
	}

	for name, query := range c.Query {
		if err := query.validate(); err != nil {
			return newConfigurationError(
				fmt.Sprintf("validate query '%s' error", name)).Wrap(err)
		}
	}

	for name, db := range c.Database {
		if err := db.validate(); err != nil {
			return newConfigurationError(
				fmt.Sprintf("validate database '%s' error", name)).Wrap(err)
		}
	}

	return nil
}

// LoadConfiguration from filename.
func LoadConfiguration(filename string) (*Configuration, error) {
	conf := &Configuration{}

	b, err := os.ReadFile(filename) // #nosec
	if err != nil {
		return nil, newConfigurationError("read file error").Wrap(err)
	}

	if err = yaml.Unmarshal(b, conf); err != nil {
		return nil, newConfigurationError("unmarshal file error").Wrap(err)
	}

	if err = conf.validate(); err != nil {
		return nil, newConfigurationError("validate error").Wrap(err)
	}

	for name, db := range conf.Database {
		db.Name = name
	}

	for name, q := range conf.Query {
		q.Name = name
	}

	return conf, nil
}
