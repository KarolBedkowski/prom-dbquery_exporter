package db

// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

// InvalidConfigrationError is error generated when database configuration is invalid.
type InvalidConfigrationError string

func (i InvalidConfigrationError) Error() string {
	return string(i)
}

// ErrNoDatabaseName is error generated when there is no configured database with given name.
var ErrNoDatabaseName = InvalidConfigrationError("no database name")
