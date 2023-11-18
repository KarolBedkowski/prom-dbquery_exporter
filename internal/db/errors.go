// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

// InvalidConfigrationError is error generated when database configuration is invalid.
type InvalidConfigrationError string

func (i InvalidConfigrationError) Error() string {
	return string(i)
}

var (
	ErrUnknownDatabase = InvalidConfigrationError("unknown database")
	ErrLoaderStopped   = InvalidConfigrationError("loader stopped")
	ErrNoDatabaseName  = InvalidConfigrationError("no database name")
)
