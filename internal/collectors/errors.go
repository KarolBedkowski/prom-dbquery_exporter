package collectors

// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import "errors"

// InvalidConfigrationError is error generated when database configuration is invalid.
type InvalidConfigrationError string

func (i InvalidConfigrationError) Error() string {
	return string(i)
}

var (
	// ErrUnknownDatabase is generated when unknown database is requestes.
	ErrUnknownDatabase = InvalidConfigrationError("unknown database")
	// ErrLoaderStopped is generated on request to closed loader.
	ErrLoaderStopped = InvalidConfigrationError("loader stopped")
	// ErrAppNotConfigured is returned when there is application is not configured yet.
	ErrAppNotConfigured = errors.New("app not configured")
)
