//go:build tmpl_extra_func
// +build tmpl_extra_func

package templates

//
// templates_test.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"testing"
)

func addRow(value float64, keyval ...string) map[string]any {
	res := make(map[string]any)
	res["value"] = value

	for i := 1; i < len(keyval); i += 2 {
		res[keyval[i-1]] = keyval[i]
	}

	return res
}

func checkRes(t *testing.T, res []map[string]any, idx int, bucket string, cnt int) {
	t.Helper()

	val, ok := res[idx]["count"]
	if !ok {
		t.Fatalf("no count in row %d: %v", idx, res[idx])

		return
	}

	val, ok = val.(int)
	if !ok {
		t.Fatalf("invalid count type in row %d: %v", idx, res[idx])

		return
	}

	if val != cnt {
		t.Fatalf("invalid val in row %d: %v; exp: %d", idx, res[idx], cnt)

		return
	}

	if res[idx]["le"] != bucket {
		t.Fatalf("invalid bucket in row %d: %v; exp: %q", idx, res[idx], bucket)
	}
}

func TestBuckets(t *testing.T) {
	t.Parallel()

	var inp []map[string]any
	inp = append(inp, addRow(1, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(1.1, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(2.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(2.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(3, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(4.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(5.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(10.0, "key1", "val1", "key2", "val2"))

	t.Logf("inp: %v", inp)

	res := buckets(inp, "value", 1.0, 2.5, 4.0, 7.0, 10.0)

	if len(res) != 6 {
		t.Errorf("invalid number of results: %v", res)
	}

	t.Logf("res: %v", res)

	checkRes(t, res, 0, "1.00", 1)
	checkRes(t, res, 1, "2.50", 4)
	checkRes(t, res, 2, "4.00", 6)
	checkRes(t, res, 3, "7.00", 7)
	checkRes(t, res, 4, "10.00", 8)
	checkRes(t, res, 5, "+Inf", 8)
}

func TestBucketsInt(t *testing.T) {
	t.Parallel()

	var inp []map[string]any
	inp = append(inp, addRow(0.1, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(1.1, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(2.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(2, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(3.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(4.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(4.9, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(5, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(5.2, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(7.1, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(10.0, "key1", "val1", "key2", "val2"))
	inp = append(inp, addRow(15, "key1", "val1", "key2", "val2"))

	t.Logf("inp: %v", inp)

	res := bucketsInt(inp, "value", 1, 2, 4, 7, 10)

	if len(res) != 6 {
		t.Errorf("invalid number of results: %v", res)
	}

	t.Logf("res: %v", res)

	checkRes(t, res, 0, "1", 1)
	checkRes(t, res, 1, "2", 4)
	checkRes(t, res, 2, "4", 6)
	checkRes(t, res, 3, "7", 9)
	checkRes(t, res, 4, "10", 11)
	checkRes(t, res, 5, "+Inf", 12)
}
