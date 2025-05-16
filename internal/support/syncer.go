package support

//
// syncer.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
)

// LockError is error returned when lock failed.
type LockError struct {
	err error
	msg string
}

func (l LockError) Error() string {
	msg := "lock error"
	if l.msg != "" {
		msg += "; locked by " + l.msg
	}

	if l.err != nil {
		msg += " (" + l.err.Error() + ")"
	}

	return msg
}

func (l LockError) Unwrap() error {
	return l.err
}

// Syncer is lock that support timeout & cancel by context.
type Syncer struct {
	locker chan struct{}
	info   string
}

// NewSyncer create new Syncer object.
func NewSyncer() Syncer {
	locker := make(chan struct{}, 1)
	locker <- struct{}{}

	return Syncer{
		locker: locker,
	}
}

// Lock try to acquire lock. Return LockError when failed.
func (s *Syncer) Lock(ctx context.Context, info ...string) error {
	select {
	case <-ctx.Done():
		return LockError{ctx.Err(), s.info}

	case <-s.locker:
		if len(info) == 0 {
			s.info = ""
		} else {
			s.info = info[0]
		}

		return nil
	}
}

// Unlock free lock.
func (s *Syncer) Unlock() {
	s.locker <- struct{}{}
}
