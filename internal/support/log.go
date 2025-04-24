//
// log.go
// Copyright (C) 2017 Karol Będkowski <Karol Będkowski@kntbk>
//
// Distributed under terms of the GPLv3 license.
//
// based on: github.com/prometheus/common/log

package support

import (
	stdlog "log"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitializeLogger set log level and optional log filename.
func InitializeLogger(level string, format string) {
	var llog zerolog.Logger

	switch format {
	default:
		log.Error().Msg("logger: unknown log format; using logfmt")

		fallthrough
	case "logfmt":
		llog = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			NoColor:    !outputIsConsole(),
			TimeFormat: time.RFC3339,
		})
	case "json":
		llog = log.Logger
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
		log.Error().Msgf("logger: unknown log level '%s'; using debug", level)
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = llog.With().Caller().Logger()

	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)
}

func outputIsConsole() bool {
	fileInfo, _ := os.Stdout.Stat()

	return fileInfo != nil && (fileInfo.Mode()&os.ModeCharDevice) != 0
}
