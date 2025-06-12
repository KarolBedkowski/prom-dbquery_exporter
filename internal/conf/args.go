package conf

import (
	"flag"
	"strings"
)

//
// cli.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

type CliArguments struct {
	ConfigFilename string

	// webserver configuration
	ListenAddress string
	WebConfig     string
	Compression   string

	// logging
	LogLevel  string
	LogFormat string

	// Flags
	DisableCache      bool
	ParallelScheduler bool
	ValidateOutput    bool
	ShowVersion       bool
}

func (c *CliArguments) EnableCompression() bool {
	switch c.Compression {
	case "true":
		return true
	case "false":
		return false
	default:
		return strings.HasPrefix(c.ListenAddress, "127.0.0.1:") ||
			strings.HasPrefix(c.ListenAddress, "localhost:")
	}
}

var Args CliArguments

func ParseCliArgs() {
	flag.BoolVar(&Args.ShowVersion, "version", false, "Print version information.")

	flag.StringVar(&Args.ConfigFilename, "config.file", "dbquery.yaml",
		"Path to configuration file.")

	flag.StringVar(&Args.ListenAddress, "web.listen-address", ":9122",
		"Address to listen on for web interface and telemetry.")
	flag.StringVar(&Args.WebConfig, "web.config", "",
		"Path to config yaml file that can enable TLS or authentication.")
	flag.StringVar(&Args.Compression, "web.compression", "auto",
		"Enable/disable response compression (true/false/auto).")

	flag.StringVar(&Args.LogLevel, "log.level", "info",
		"Logging level (debug, info, warn, error, fatal)")
	flag.StringVar(&Args.LogFormat, "log.format", "logfmt",
		"Logging log format (logfmt, json).")

	flag.BoolVar(&Args.DisableCache, "no-cache", false, "Disable query result caching")
	flag.BoolVar(&Args.ParallelScheduler, "parallel-scheduler", false, "Run scheduler as parallel")
	flag.BoolVar(&Args.ValidateOutput, "validate-output", false, "Enable output validation")

	flag.Parse()

	if Args.Compression != "true" && Args.Compression != "false" {
		Args.Compression = "auto"
	}
}
