//
// log.go
// Copyright (C) 2017 Karol Będkowski <Karol Będkowski@kntbk>
//
// Distributed under terms of the GPLv3 license.
//
// based on: github.com/prometheus/common/log

package support

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
	default:
		fmt.Printf("unknown log format; using logfmt")
		fallthrough
	case "logfmt":
		l = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			NoColor:    !outputIsConsole(),
			TimeFormat: time.RFC3339,
		})
	case "json":
		l = log.Logger
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
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	default:
		fmt.Printf("unknown log level '%s'; using debug", level)
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = l.With().Caller().Logger()
	Logger = log.Logger
	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)
}

func outputIsConsole() bool {
	fileInfo, _ := os.Stdout.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
