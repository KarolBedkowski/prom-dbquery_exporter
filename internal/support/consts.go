package support

//
// consts.go
// Copyright (C) 2021-2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// CtxKey is type of keys in context.
type CtxKey int

const (
	// CtxWriteMutexID is key for mutex that block parallel write http response.
	CtxWriteMutexID CtxKey = iota
)
