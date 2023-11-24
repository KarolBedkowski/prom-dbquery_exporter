// global.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package conf

// GlobalConf is application global configuration.
type GlobalConf struct {
	// RequestTimeout is maximum processing request time.
	RequestTimeout int `yaml:"request_timeout"`
}

func (g *GlobalConf) validate() error {
	return nil
}
