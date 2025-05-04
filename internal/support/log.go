//
// log.go
// Copyright (C) 2017 Karol Będkowski <Karol Będkowski@kntbk>
//
// Distributed under terms of the GPLv3 license.
//
// based on: github.com/prometheus/common/log

package support

import (
	"context"
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

	if l, err := zerolog.ParseLevel(level); err == nil {
		zerolog.SetGlobalLevel(l)
	} else {
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

func GetLoggerFromCtx(ctx context.Context) zerolog.Logger {
	if llog := log.Ctx(ctx); llog != nil {
		return *llog
	}

	return log.Logger
}
