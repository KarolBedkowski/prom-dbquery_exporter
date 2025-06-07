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

const defaultRequestTimeout = time.Duration(15) * time.Minute

// GlobalConf is application global configuration.
type GlobalConf struct {
	// RequestTimeout is maximum processing request time.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	MaxRequestInFlight uint `yaml:"max_request_in_flight"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (g *GlobalConf) MarshalZerologObject(e *zerolog.Event) {
	e.Dur("request_timeout", g.RequestTimeout)
	e.Uint("max_request_in_flight", g.MaxRequestInFlight)
}

func (g *GlobalConf) setup() {
	if g.RequestTimeout == 0 {
		g.RequestTimeout = defaultRequestTimeout
	}
}

//nolint:unparam
func (g *GlobalConf) validate() error {
	if g.RequestTimeout.Seconds() < 1 && g.RequestTimeout > 0 {
		log.Logger.Warn().Msgf("configuration: global request_timeout < 1s: %v", g.RequestTimeout)
	}

	return nil
}
