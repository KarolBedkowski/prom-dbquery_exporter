package support

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
	// CtxWriteMutexID is key for mutex that block parallel write http response
	CtxWriteMutexID CtxKey = iota
)

// MetricsNamespace is namespace for prometheus metrics
const MetricsNamespace = "dbquery_exporter"
