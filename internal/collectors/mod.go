package collectors

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
)

// Collectors is collection of all configured databases.
type Collectors struct {
	log        zerolog.Logger
	cfg        *conf.Configuration
	collectors map[string]*collector
	sync.Mutex
}

// newCollectors create new Databases object.
func newCollectors() *Collectors {
	return &Collectors{
		collectors: make(map[string]*collector),
		cfg:        nil,
		log:        log.Logger.With().Str("module", "databases").Logger(),
	}
}

func (cs *Collectors) createCollector(dbName string) (*collector, error) {
	if cs.cfg == nil {
		return nil, ErrAppNotConfigured
	}

	dconf, ok := cs.cfg.Database[dbName]
	if !ok {
		return nil, ErrUnknownDatabase
	}

	c, err := newCollector(dbName, dconf)
	if err != nil {
		return nil, fmt.Errorf("collectors: create dbloader error: %w", err)
	}

	cs.collectors[dbName] = c

	return c, nil
}

// ScheduleTask schedule new task to process in this database.
func (cs *Collectors) ScheduleTask(task *Task) error {
	cs.Lock()
	defer cs.Unlock()

	dbName := task.DBName

	dbloader, ok := cs.collectors[dbName]
	if !ok {
		var err error

		cs.log.Debug().Str("dbname", dbName).Msg("collectors: creating collector")

		dbloader, err = cs.createCollector(dbName)
		if err != nil {
			return err
		}

		if dbloader == nil {
			return ErrAppNotConfigured
		}
	}

	dbloader.addTask(task)

	return nil
}

// UpdateConf update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func (cs *Collectors) UpdateConf(cfg *conf.Configuration) {
	cs.Lock()
	defer cs.Unlock()

	if cfg == nil {
		return
	}

	cs.log.Debug().Msg("collectors: update configuration begin")

	// update existing
	for k, dbConf := range cfg.Database {
		if db, ok := cs.collectors[k]; ok {
			if db.updateConf(dbConf) {
				cs.log.Info().Str("db", k).Msg("collectors: database configuration changed")
			}
		}
	}

	var toDel []string

	// stop not existing anymore
	for k, db := range cs.collectors {
		if _, ok := cfg.Database[k]; !ok {
			cs.log.Info().Str("db", k).Msgf("collectors: database %s not found in new conf; removing", k)

			_ = db.stop()

			toDel = append(toDel, k)
		}
	}

	for _, k := range toDel {
		delete(cs.collectors, k)
	}

	cs.cfg = cfg

	cs.log.Debug().Msg("collectors: update configuration finished")
}

// Close database.
func (cs *Collectors) Close() {
	cs.Lock()
	defer cs.Unlock()

	cs.log.Debug().Msg("collectors: closing databases")

	for k, db := range cs.collectors {
		cs.log.Info().Str("db", k).Msg("collectors: stopping database")

		_ = db.stop()
	}
}

// collectorsLen return number or loaders in pool.
func (cs *Collectors) collectorsLen() float64 {
	if cs == nil {
		return 0
	}

	cs.Lock()
	defer cs.Unlock()

	return float64(len(cs.collectors))
}

// stats return stats for each loaders.
func (cs *Collectors) stats() []collectorStats {
	cs.Lock()
	defer cs.Unlock()

	stats := make([]collectorStats, 0, len(cs.collectors))

	for _, l := range cs.collectors {
		stats = append(stats, l.stats())
	}

	return stats
}

// CollectorsPool is global handler for all db queries.
var CollectorsPool = newCollectors()

// Init db subsystem.
func Init() {
	initMetrics()
	initTemplates()
}
