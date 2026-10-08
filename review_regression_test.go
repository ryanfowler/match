package match

import (
	"errors"
	"strings"
	"testing"
)

func TestRadixCloneAfterEdgeSplits(t *testing.T) {
	originalRoutes := []string{"/abc/{id}", "/abx/{id}", "/b/{id}", "/c/{id}", "/d/{id}", "/e/{id}"}
	addedRoutes := []string{"/ab/{id}", "/abcd/{id}", "/abq/{id}", "/f/{id}", "/foo/{id}"}
	var original Router[string]
	for _, route := range originalRoutes {
		original.Insert(route, route)
	}
	cloned := original.Clone()
	for _, route := range addedRoutes {
		cloned.Insert(route, route)
	}
	for _, route := range append(originalRoutes, addedRoutes...) {
		path := strings.ReplaceAll(route, "{id}", "42")
		got, params, ok := cloned.Match(path)
		if !ok || got != route || !paramsEqual(params, ParamsOf(Param{Key: "id", Val: "42"})) {
			t.Fatalf("clone.Match(%q) = %q %v %v", path, got, params.All(), ok)
		}
	}
	for _, route := range originalRoutes {
		path := strings.ReplaceAll(route, "{id}", "42")
		got, params, ok := original.Match(path)
		if !ok || got != route || params.Get("id") != "42" {
			t.Fatalf("original.Match(%q) = %q %v %v", path, got, params.All(), ok)
		}
	}
	for _, route := range addedRoutes {
		path := strings.ReplaceAll(route, "{id}", "42")
		if got, params, ok := original.Match(path); ok || got != "" || params.Len() != 0 {
			t.Fatalf("original matched clone-only route %q: %q %v %v", path, got, params.All(), ok)
		}
	}
}

func TestCatchAllConflictsWithParamDescendant(t *testing.T) {
	// A plain param and catch-all cannot coexist at the same radix node through
	// the public API, including when the parameter route has more segments.
	for _, routes := range [][2]string{
		{"/files/{name}/meta", "/files/{*rest}"},
		{"/files/{*rest}", "/files/{name}/meta"},
	} {
		var router Router[string]
		router.Insert(routes[0], routes[0])
		var conflict *ConflictError
		if err := router.TryInsert(routes[1], routes[1]); !errors.As(err, &conflict) {
			t.Fatalf("insert %q after %q: got %v, want conflict", routes[1], routes[0], err)
		}
		if conflict.With != routes[0] {
			t.Fatalf("conflict with = %q, want %q", conflict.With, routes[0])
		}
	}
}
