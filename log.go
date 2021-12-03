//
// log.go
// Copyright (C) 2017 Karol Będkowski <Karol Będkowski@kntbk>
//
// Distributed under terms of the GPLv3 license.
//
// based on: github.com/prometheus/common/log

package main

import (
	"fmt"
	stdlog "log"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Logger is application global logger
var Logger zerolog.Logger

// InitializeLogger set log level and optional log filename
func InitializeLogger(level string, format string) {
	var l zerolog.Logger

	switch format {
	case "json":
		l = log.Logger
	default:
		fmt.Printf("unknown log format; using logfmt")
		fallthrough
	case "logfmt":
		l = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	}

	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		fmt.Printf("unknown log level '%s', using debug", level)
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = l.With().Caller().Logger()
	Logger = log.Logger
	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)
}
