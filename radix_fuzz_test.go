package match

import (
	"strconv"
	"testing"
)

// The segment trie independently matches the same simple routes, making it a
// reference for radix splits, backtracking, capture order, and cloned indexes.
func FuzzRadixMatch(f *testing.F) {
	routes := []string{"", "/", "/users/new", "/users/{id}/posts/{post}", "/users/{name}/settings", "/assets/{*path}", "/many/{a}/{b}/{c}/{d}/{e}"}
	for i := 0; i < 32; i++ {
		routes = append(routes, "/route-"+strconv.Itoa(i)+"/{id}")
	}
	var routers [3]Router[string]
	for _, route := range routes {
		routers[0].Insert(route, route)
	}
	for i := len(routes) - 1; i >= 0; i-- {
		// Reverse insertion order also exercises splits at existing edges.
		routers[1].Insert(routes[i], routes[i])
	}
	routers[2] = routers[1].Clone()
	for _, path := range []string{"", "/", "/users/new", "/users/new/posts/42", "/users/new/settings", "/users/new/posts/42/missing", "/assets/a/b", "/assets/", "/many/a/b/c/d/e", "/many/a/b/c/d/e/missing", "/many/a/b/c//e"} {
		f.Add(path)
	}
	for i := 0; i < 32; i++ {
		f.Add("/route-" + strconv.Itoa(i) + "/value")
	}
	f.Fuzz(func(t *testing.T, path string) {
		for i := range routers {
			router := &routers[i]
			entry, wantOK := router.root.root.matchPath(path, 0)
			var want string
			var wantParams Params
			if wantOK {
				want = entry.value
				collectParams(entry, path, &wantParams)
			}
			got, params, ok := router.Match(path)
			if got != want || ok != wantOK || !paramsEqual(params, wantParams) {
				t.Fatalf("Match(%q) = %q %v %v; want %q %v %v", path, got, params.All(), ok, want, wantParams.All(), wantOK)
			}
			for _, capacity := range []int{0, 8} {
				params = NewParams(capacity)
				params.Append("stale", "value")
				got, ok = router.MatchInto(path, &params)
				if got != want || ok != wantOK || !paramsEqual(params, wantParams) {
					t.Fatalf("MatchInto(%q, capacity=%d) = %q %v %v; want %q %v %v", path, capacity, got, params.All(), ok, want, wantParams.All(), wantOK)
				}
			}
		}
	})
}
