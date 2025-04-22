package conf

// global.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// GlobalConf is application global configuration.
type GlobalConf struct {
	// RequestTimeout is maximum processing request time.
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (g *GlobalConf) MarshalZerologObject(e *zerolog.Event) {
	e.Dur("request_timeout", g.RequestTimeout)
}

//nolint:unparam
func (g *GlobalConf) validate() error {
	if g.RequestTimeout.Seconds() < 1 && g.RequestTimeout > 0 {
		log.Logger.Warn().Msgf("FGlobal request_timeout < 1s: %v", g.RequestTimeout)
	}

	return nil
}
