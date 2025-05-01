//
// config.go

package conf

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
)

// Configuration keep application configuration.
type Configuration struct {
	// Databases
	Database map[string]*Database
	// Queries
	Query map[string]*Query
	Jobs  []*Job
	// Global application settings
	Global GlobalConf

	// Runtime configuration

	ConfigFilename    string `yaml:"-"`
	DisableCache      bool   `yaml:"-"`
	ParallelScheduler bool   `yaml:"-"`
	ValidateOutput    bool   `yaml:"-"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (c *Configuration) MarshalZerologObject(event *zerolog.Event) {
	event.Object("global", &c.Global).
		Interface("jobs", c.Jobs)

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

	event.Dict("cli", zerolog.Dict().
		Str("config_filename", c.ConfigFilename).
		Bool("disable_cache", c.DisableCache).
		Bool("parallel_scheduler", c.ParallelScheduler).
		Bool("validateOutput", c.ValidateOutput))
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

// LoadConfiguration from filename.
func LoadConfiguration(filename string) (*Configuration, error) {
	logger := log.Logger.With().Str("module", "config").Logger()
	conf := &Configuration{
		ConfigFilename: filename,
	}

	logger.Info().Msgf("Loading config file %s", filename)

	b, err := os.ReadFile(filename) // #nosec
	if err != nil {
		return nil, newConfigurationError("read file error").Wrap(err)
	}

	if err = yaml.Unmarshal(b, conf); err != nil {
		return nil, newConfigurationError("unmarshal file error").Wrap(err)
	}

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
	if err = conf.validate(ctx); err != nil {
		return nil, newConfigurationError("validate error").Wrap(err)
	}

	return conf, nil
}

func (c *Configuration) CopyRuntimeOptions(oldcfg *Configuration) {
	c.DisableCache = oldcfg.DisableCache
	c.ParallelScheduler = oldcfg.ParallelScheduler
	c.ValidateOutput = oldcfg.ValidateOutput
}

func (c *Configuration) SetCliOptions(disableCache, parallelScheduler, validateOutput *bool) {
	if disableCache != nil {
		c.DisableCache = *disableCache
	}

	if parallelScheduler != nil {
		c.ParallelScheduler = *parallelScheduler
	}

	if validateOutput != nil {
		c.ValidateOutput = *validateOutput
	}
}

func (c *Configuration) validateJobs(ctx context.Context) error {
	var errs *multierror.Error

	validJobs := 0

	for i, job := range c.Jobs {
		if err := job.validate(ctx, c); err != nil {
			errs = multierror.Append(errs, newConfigurationError(
				fmt.Sprintf("validate job %d error", i+1)).Wrap(err))
		}

		if job.IsValid {
			validJobs++
		}
	}

	if len(c.Jobs) > 0 && validJobs == 0 {
		logger := log.Ctx(ctx)
		logger.Warn().Msgf("configuration: all jobs are invalid!")
	}

	return errs.ErrorOrNil()
}

func (c *Configuration) validate(ctx context.Context) error {
	var errs *multierror.Error

	if len(c.Database) == 0 {
		errs = multierror.Append(errs, newConfigurationError("no database configured"))
	}

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

	for name, db := range c.Database {
		if err := db.validate(); err != nil {
			errs = multierror.Append(errs, newConfigurationError(
				fmt.Sprintf("validate database '%s' error", name)).Wrap(err))
		}
	}

	errs = multierror.Append(errs, c.validateJobs(ctx))

	return errs.ErrorOrNil()
}
