package conf

// global.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"time"

	"github.com/rs/zerolog"
)

// GlobalConf is application global configuration.
type GlobalConf struct {
	// RequestTimeout is maximum processing request time.
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (g GlobalConf) MarshalZerologObject(e *zerolog.Event) {
	e.Dur("request_timeout", g.RequestTimeout)
}

func (g *GlobalConf) validate() error {
	return nil
}
