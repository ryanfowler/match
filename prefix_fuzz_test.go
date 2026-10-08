package match

import "testing"

// Enumerate whole-segment prefixes and match each with the recursive segment
// trie, independently of the iterative prefix traversal.
func referencePrefix(router *Router[string], path string) (PrefixMatch[string], bool) {
	finish := func(entry *routeEntry[string], restIndex int) (PrefixMatch[string], bool) {
		var params Params
		collectParams(entry, path, &params)
		return PrefixMatch[string]{Value: entry.value, Params: params, Rest: remainingPrefixPath(path, restIndex)}, true
	}
	if entry, ok := router.root.root.matchPath(path, 0); ok {
		return finish(entry, -1)
	}
	for i := len(path) - 1; i > 0; i-- {
		if path[i] != '/' {
			continue
		}
		if entry, ok := router.root.root.matchPath(path[:i], 0); ok {
			return finish(entry, i+1)
		}
	}
	if len(path) != 0 && path[0] == '/' && router.root.rootPrefix != nil {
		return finish(router.root.rootPrefix, 1)
	}
	return PrefixMatch[string]{}, false
}

func FuzzMatchPrefix(f *testing.F) {
	routeSets := [][]string{
		{"", "/", "/api", "/api/v1", "/api/{version}/users/{id}", "/assets/{*path}", "/assets/logo"},
		{"", "/", "/chain/{a}/{b}/{c}/{d}/{e}", "/choice/static/short", "/choice/{name}/long/deep", "/choice/user-{id}/specific", "/choice/{name}/fallback", "relative/{id}", "/empty/"},
	}
	var routers []Router[string]
	for _, routes := range routeSets {
		var router Router[string]
		for _, route := range routes {
			router.Insert(route, route)
		}
		routers = append(routers, router, router.Clone())
	}
	for _, path := range []string{"", "/", "//rest", "///", "/api/v1/users/42/more", "/api/v1/users//more", "/api/v2/other", "/api/v1extra", "/assets/logo/extra", "/assets/a/b", "/chain/a/b/c/d/e/rest", "/chain/a/b/c//e/rest", "/choice/static/long/deep/rest", "/choice/user-42/specific/rest", "/choice/user-42/fallback/rest", "/empty//rest", "relative/42/rest", "/missing"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		for i := range routers {
			router := &routers[i]
			want, wantOK := referencePrefix(router, path)
			check := func(got PrefixMatch[string], ok bool) {
				if ok != wantOK || got.Value != want.Value || got.Rest != want.Rest || !paramsEqual(got.Params, want.Params) {
					t.Fatalf("prefix(%q) = %q %q %v %v; want %q %q %v %v", path, got.Value, got.Rest, got.Params.All(), ok, want.Value, want.Rest, want.Params.All(), wantOK)
				}
			}
			got, ok := router.MatchPrefix(path)
			check(got, ok)
			for _, capacity := range []int{0, 8} {
				params := NewParams(capacity)
				params.Append("stale", "value")
				got, ok = router.MatchPrefixInto(path, &params)
				check(got, ok)
				if !paramsEqual(params, want.Params) {
					t.Fatalf("reused prefix params = %v, want %v", params.All(), want.Params.All())
				}
			}
		}
	})
}
