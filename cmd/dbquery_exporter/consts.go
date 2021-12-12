package main

//
// consts.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// CtxKey is type for keys in context
type CtxKey int

const (
	// CtxRequestID is key for request id in context
	CtxRequestID CtxKey = iota
)
