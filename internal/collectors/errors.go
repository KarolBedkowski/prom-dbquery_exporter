package collectors

// errors.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import "errors"

var (
	ErrUnknownDatabase     = errors.New("unknown database")
	ErrNoDatabases         = errors.New("no databases available")
	ErrCollectorUnavilable = errors.New("collector unavailable")
)

type InternalError string

func (i InternalError) Error() string {
	return string(i)
}
