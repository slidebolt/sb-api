package inventory

import (
	"fmt"
	"strconv"
	"strings"

	storage "github.com/slidebolt/sb-storage-sdk"
)

// Matches reports whether a stored document satisfies a query, comparing
// whole values (case-insensitively) rather than words. A field that holds a
// list matches when any element does.
func Matches(key string, doc map[string]any, q storage.Query) bool {
	if !keyMatches(key, q.Pattern) {
		return false
	}
	for _, f := range q.Where {
		if !filterMatches(doc, f) {
			return false
		}
	}
	return true
}

// keyMatches applies storage key wildcards: "*" is one segment, ">" is the rest.
func keyMatches(key, pattern string) bool {
	if pattern == "" || pattern == ">" {
		return true
	}
	ks, ps := strings.Split(key, "."), strings.Split(pattern, ".")
	for i, p := range ps {
		if p == ">" {
			return len(ks) > i
		}
		if i >= len(ks) || (p != "*" && p != ks[i]) {
			return false
		}
	}
	return len(ks) == len(ps)
}

func lookup(doc map[string]any, path string) (any, bool) {
	var cur any = doc
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func values(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{v}
}

func equal(a, b any) bool {
	return strings.EqualFold(fmt.Sprint(a), fmt.Sprint(b))
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func filterMatches(doc map[string]any, f storage.Filter) bool {
	v, ok := lookup(doc, f.Field)
	if f.Op == storage.Exists {
		if !ok || v == nil {
			return false
		}
		if list, isList := v.([]any); isList {
			return len(list) > 0
		}
		return true
	}
	if f.Op == storage.Neq {
		f.Op = storage.Eq
		return !filterMatches(doc, f)
	}
	if !ok {
		return false
	}
	for _, have := range values(v) {
		switch f.Op {
		case storage.Eq:
			if equal(have, f.Value) {
				return true
			}
		case storage.In:
			for _, want := range values(f.Value) {
				if equal(have, want) {
					return true
				}
			}
		case storage.Contains:
			if strings.Contains(strings.ToLower(fmt.Sprint(have)), strings.ToLower(fmt.Sprint(f.Value))) {
				return true
			}
		case storage.Prefix:
			if strings.HasPrefix(strings.ToLower(fmt.Sprint(have)), strings.ToLower(fmt.Sprint(f.Value))) {
				return true
			}
		case storage.Gt, storage.Gte, storage.Lt, storage.Lte:
			h, ok1 := number(have)
			w, ok2 := number(f.Value)
			if ok1 && ok2 && compare(f.Op, h, w) {
				return true
			}
		}
	}
	return false
}

func compare(op string, h, w float64) bool {
	switch op {
	case storage.Gt:
		return h > w
	case storage.Gte:
		return h >= w
	case storage.Lt:
		return h < w
	}
	return h <= w
}
