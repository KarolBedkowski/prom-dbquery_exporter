package support

import "maps"

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// CloneMap create clone of `inp` map and optionally update it with values
// from extra maps.
func CloneMap[K comparable, V any](inp map[K]V, extra ...map[K]V) map[K]V {
	res := make(map[K]V, len(inp)+1)
	maps.Copy(res, inp)

	for _, e := range extra {
		maps.Copy(res, e)
	}

	return res
}
