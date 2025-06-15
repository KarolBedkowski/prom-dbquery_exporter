package db

import "fmt"

// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

// InvalidConfigurationError is error generated when database configuration is invalid.
type InvalidConfigurationError string

func newInvalidConfigurationError(format string, a ...any) InvalidConfigurationError {
	return InvalidConfigurationError(fmt.Sprintf(format, a...))
}

func (i InvalidConfigurationError) Error() string {
	return string(i)
}

// -----------------------------------------------

// ErrNoDatabaseName is error generated when there is no configured database with given name.
var ErrNoDatabaseName = InvalidConfigurationError("no database name")

// -----------------------------------------------

type NotSupportedError string

func (i NotSupportedError) Error() string {
	return "Database " + string(i) + " is not supported; check compile flags"
}
