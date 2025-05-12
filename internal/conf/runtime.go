package conf

import "flag"

//
// cli.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

type RuntimeArgs struct {
	ConfigFilename string

	// Flags

	DisableCache      bool
	ParallelScheduler bool
	ValidateOutput    bool
	ShowVersion       bool

	// webserver configuration

	ListenAddress string
	WebConfig     string
	EnableInfo    bool

	// logging

	LogLevel  string
	LogFormat string
}

func NewRuntimeArgs() RuntimeArgs {
	opts := RuntimeArgs{}

	flag.BoolVar(&opts.ShowVersion, "version", false, "Print version information.")

	flag.StringVar(&opts.ConfigFilename, "config.file", "dbquery.yaml",
		"Path to configuration file.")

	flag.StringVar(&opts.ListenAddress, "web.listen-address", ":9122",
		"Address to listen on for web interface and telemetry.")
	flag.StringVar(&opts.WebConfig, "web.config", "",
		"Path to config yaml file that can enable TLS or authentication.")
	flag.BoolVar(&opts.EnableInfo, "enable-info", false, "Enable /info endpoint")

	flag.StringVar(&opts.LogLevel, "log.level", "info",
		"Logging level (debug, info, warn, error, fatal)")
	flag.StringVar(&opts.LogFormat, "log.format", "logfmt",
		"Logging log format (logfmt, json).")

	flag.BoolVar(&opts.DisableCache, "no-cache", false, "Disable query result caching")
	flag.BoolVar(&opts.ParallelScheduler, "parallel-scheduler", false, "Run scheduler ask parallel")
	flag.BoolVar(&opts.ValidateOutput, "validate-output", false, "Enable output validation")

	flag.Parse()

	return opts
}
