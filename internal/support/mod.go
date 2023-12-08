package support

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// InList check if val is in `list`.
func InList[T comparable](val T, list ...T) bool {
	for _, lv := range list {
		if val == lv {
			return true
		}
	}

	return false
}

// CloneMap create clone of `inp` map and optionally update it with values
// from extra maps.
func CloneMap[K comparable, V any](inp map[K]V, extra ...map[K]V) map[K]V {
	res := make(map[K]V, len(inp)+1)
	for k, v := range inp {
		res[k] = v
	}

	for _, e := range extra {
		for k, v := range e {
			res[k] = v
		}
	}

	return res
}
