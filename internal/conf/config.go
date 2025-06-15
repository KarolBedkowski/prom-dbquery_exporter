//
// config.go

package conf

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
)

type DatabaseProvider interface {
	Validate(d *Database) error
	IsSupported(d *Database) bool
}

// Configuration keep application configuration.
type Configuration struct {
	// Databases
	Database map[string]*Database
	// Queries
	Query map[string]*Query
	// background jobs configuration
	Jobs []*Job
	// Global application settings
	Global GlobalConf

	Groups map[string][]string `yaml:"-"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (c *Configuration) MarshalZerologObject(event *zerolog.Event) {
	event.Object("global", &c.Global).
		Interface("jobs", c.Jobs).
		Interface("groups", c.Groups)

	d := zerolog.Dict()

	for k, v := range c.Database {
		d.Object(k, v)
	}

	event.Dict("database", d)

	qd := zerolog.Dict()

	for k, v := range c.Query {
		qd.Object(k, v)
	}

	event.Dict("query", qd)
}

// LoadConfiguration from filename.
func Load(filename string, dbp DatabaseProvider) (*Configuration, error) {
	logger := log.Logger.With().Str("module", "config").Logger()
	conf := &Configuration{} //nolint:exhaustruct

	logger.Info().Msgf("Loading config file %q", filename)

	b, err := os.ReadFile(filename) // #nosec
	if err != nil {
		return nil, newConfigurationError("read file error").Wrap(err)
	}

	if err = yaml.Unmarshal(b, conf); err != nil {
		return nil, newConfigurationError("unmarshal file error").Wrap(err)
	}

	conf.Global.setup()

	for name, db := range conf.Database {
		db.setup(name)
	}

	for name, q := range conf.Query {
		q.setup(name)
	}

	for idx, j := range conf.Jobs {
		j.setup(idx + 1)
	}

	ctx := logger.WithContext(context.Background())
	if err = conf.validate(ctx, dbp); err != nil {
		return nil, newConfigurationError("validate error").Wrap(err)
	}

	conf.buildGroups()

	configReloadTime.SetToCurrentTime()

	return conf, nil
}

// ValidDatabases get iter of valid databases.
func (c *Configuration) ValidDatabases(yield func(*Database) bool) {
	for _, d := range c.Database {
		if d.Valid {
			if !yield(d) {
				return
			}
		}
	}
}

func (c *Configuration) validateJobs(ctx context.Context) error {
	var errs *multierror.Error

	validJobs := 0

	for i, job := range c.Jobs {
		if err := job.validate(ctx, c); err != nil {
			errs = multierror.Append(errs, newConfigurationError(
				fmt.Sprintf("validate job %d error", i+1)).Wrap(err))
		} else if job.IsValid {
			validJobs++
		}
	}

	if len(c.Jobs) > 0 && validJobs == 0 {
		log.Ctx(ctx).Warn().Msg("configuration: all jobs are invalid!")
	}

	return errs.ErrorOrNil()
}

func (c *Configuration) validate(ctx context.Context, dbp DatabaseProvider) error {
	var errs *multierror.Error

	if err := c.Global.validate(); err != nil {
		errs = multierror.Append(errs, newConfigurationError("validate global settings error").Wrap(err))
	}

	if len(c.Query) == 0 {
		errs = multierror.Append(errs, newConfigurationError("no query configured"))
	}

	for name, query := range c.Query {
		if err := query.validate(); err != nil {
			errs = multierror.Append(errs, newConfigurationError(
				fmt.Sprintf("validate query '%s' error", name)).Wrap(err))
		}
	}

	errs = multierror.Append(errs,
		c.validateDatabases(ctx, dbp),
		c.validateJobs(ctx))

	return errs.ErrorOrNil()
}

func (c *Configuration) validateDatabases(ctx context.Context, dbp DatabaseProvider) error {
	if len(c.Database) == 0 {
		return newConfigurationError("no database configured")
	}

	var (
		errs            *multierror.Error
		anyDbConfigured bool
	)

	for name, db := range c.Database {
		if dbp.IsSupported(db) {
			if err := db.validate(dbp); err != nil {
				errs = multierror.Append(errs, newConfigurationError(
					fmt.Sprintf("validate database %q error", name)).Wrap(err))
			} else {
				anyDbConfigured = true
			}
		} else {
			log.Ctx(ctx).Error().Str("database", name).Msgf("database %q is not supported", db.Driver)
		}
	}

	if !anyDbConfigured {
		errs = multierror.Append(errs, newConfigurationError("all databases have invalid configuration"))
	}

	return errs.ErrorOrNil()
}

func (c *Configuration) buildGroups() {
	groups := make(map[string][]string)

	for queryName, q := range c.Query {
		for _, group := range q.Groups {
			if groupQueries, ok := groups[group]; ok {
				// do no duplicate queries
				if !slices.Contains(groupQueries, queryName) {
					groups[group] = append(groupQueries, queryName)
				}
			} else {
				groups[group] = []string{queryName}
			}
		}
	}

	c.Groups = groups
}
