package collectors

// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import "errors"

// InvalidConfigurationError is error generated when database configuration is invalid.
type InvalidConfigurationError string

func (i InvalidConfigurationError) Error() string {
	return string(i)
}

// ErrLoaderStopped is generated on request to closed loader.
var ErrAppNotConfigured = errors.New("not configured")

type InternalError string

func (i InternalError) Error() string {
	return string(i)
}
