package match

import (
	"strconv"
	"sync"
	"testing"
)

func TestCompileSnapshot(t *testing.T) {
	var router Router[string]
	for _, route := range []string{"", "/", "/fixed", "/users/{id}", "/files/{name}.json", "/assets/{*path}"} {
		router.Insert(route, route)
	}
	matcher := router.Compile()
	copiedBeforeGrowth := *matcher
	router.Insert("/users/new", "new")
	router.Insert("/files/report.json", "report")
	for i := range 100 {
		router.Insert("/growth/"+strconv.Itoa(i), "growth")
	}
	copiedAfterGrowth := *matcher
	router.Insert("/later", "later")
	for _, m := range []*Matcher[string]{matcher, &copiedBeforeGrowth, &copiedAfterGrowth} {
		for _, tc := range []struct{ path, value, rest string }{
			{"/users/new", "/users/{id}", "/"},
			{"/files/report.json", "/files/{name}.json", "/"},
			{"/users/new/more", "/users/{id}", "/more"},
		} {
			got, ok := m.MatchPrefix(tc.path)
			if !ok || got.Value != tc.value || got.Rest != tc.rest {
				t.Fatalf("compiled prefix(%q) = %#v %v", tc.path, got, ok)
			}
		}
		if got, _, ok := m.Match("/users/new"); !ok || got != "/users/{id}" {
			t.Fatalf("compiled match changed after insertion: %q %v", got, ok)
		}
		if _, _, ok := m.Match("/later"); ok {
			t.Fatal("compiled snapshot contains later route")
		}
	}
	updated := router.Compile()
	if got, _, ok := updated.Match("/users/new"); !ok || got != "new" {
		t.Fatalf("recompiled match = %q %v", got, ok)
	}
	if got, _, ok := router.Compile().Match("/later"); !ok || got != "later" {
		t.Fatalf("chained compiled match = %q %v", got, ok)
	}
}

func TestCompileZeroValue(t *testing.T) {
	var router Router[int]
	compiled := router.Compile()
	for _, m := range []Matcher[int]{{}, *compiled} {
		for _, path := range []string{"", "/", "relative", "/missing"} {
			if value, params, ok := m.Match(path); value != 0 || params.Len() != 0 || ok {
				t.Fatalf("empty Match(%q) = %v %v %v", path, value, params.All(), ok)
			}
			params := ParamsOf(Param{"stale", "value"})
			if value, ok := m.MatchInto(path, &params); value != 0 || ok || params.Len() != 0 {
				t.Fatalf("empty MatchInto(%q) = %v %v %v", path, value, params.All(), ok)
			}
			if got, ok := m.MatchPrefix(path); ok || got.Value != 0 || got.Rest != "" || got.Params.Len() != 0 {
				t.Fatalf("empty prefix(%q) = %#v %v", path, got, ok)
			}
			params.Append("stale", "value")
			if got, ok := m.MatchPrefixInto(path, &params); ok || got.Params.Len() != 0 || params.Len() != 0 {
				t.Fatalf("empty prefix into(%q) = %#v %v", path, got, ok)
			}
		}
	}
}

func compiledRouteSets() [][]string {
	return [][]string{
		{"", "/", "/fixed", "/users/new"},
		{"/{root}", "/users/{id}", "relative/{id}", "{{literal}}/{id}", "/empty//{id}"},
		{"{id}"},
		mixedBenchmarkRoutes(),
		{"/users/{id}", "/files/{name}.json", "/files/{id}", "/files/user-{id}/more", "relative/{id}", "rel-{name}/more", "/many/{a}/{b}/{c}/{d}/{e}"},
		{"/specific/{id}", "/{section}/tail", "/files/{name}.json"},
		{"/specific/{id}", "/{section}/tail", "/files/{name}.json", "prefix-{*path}"},
		{"/fixed", "{*path}"},
		{"/specific/{id}", "/files/{name}.json", "/assets/prefix-{*path}", "//{id}/tail", "relative/{id}"},
		{"/{name}x/foo", "/a{name}/bar", "/{name}/plain"},
		{"/user-42/{id}", "/user-{id}/more", "/users/{id}/posts/{post}", "rel-fixed/{id}", "rel-{id}/more"},
		{"docs/{name}.md", "{section}/tail", "relative/{id}"},
		{"docs/{name}.md", "rel/{a}x", "relative/{id}"},
		{"/docs/{name}.md", "docs/{name}.md", "/rel/{a}x", "rel/{a}x", "relative/{id}"},
	}
}

func checkCompiledMatch(t *testing.T, router *Router[string], matcher *Matcher[string], path string) {
	t.Helper()
	var want string
	var wantParams Params
	entry, wantOK := router.root.root.matchPath(path, 0)
	if wantOK {
		want = entry.value
		collectParams(entry, path, &wantParams)
	}
	routerValue, routerParams, routerOK := router.Match(path)
	if routerValue != want || routerOK != wantOK || !paramsEqual(routerParams, wantParams) {
		t.Fatalf("Router.Match(%q) = %q %v %v; want %q %v %v", path, routerValue, routerParams.All(), routerOK, want, wantParams.All(), wantOK)
	}
	got, params, ok := matcher.Match(path)
	if got != want || ok != wantOK || !paramsEqual(params, wantParams) {
		t.Fatalf("compiled Match(%q) = %q %v %v; want %q %v %v", path, got, params.All(), ok, want, wantParams.All(), wantOK)
	}
	wantPrefix, wantPrefixOK := referencePrefix(router, path)
	checkPrefix := func(got PrefixMatch[string], ok bool) {
		t.Helper()
		if got.Value != wantPrefix.Value || got.Rest != wantPrefix.Rest || ok != wantPrefixOK || !paramsEqual(got.Params, wantPrefix.Params) {
			t.Fatalf("compiled prefix(%q) = %q %q %v %v; want %q %q %v %v", path, got.Value, got.Rest, got.Params.All(), ok, wantPrefix.Value, wantPrefix.Rest, wantPrefix.Params.All(), wantPrefixOK)
		}
	}
	prefix, ok := matcher.MatchPrefix(path)
	checkPrefix(prefix, ok)
	for _, capacity := range []int{0, 8} {
		params := NewParams(capacity)
		params.Append("stale", "value")
		routerValue, routerOK := router.MatchInto(path, &params)
		if routerValue != want || routerOK != wantOK || !paramsEqual(params, wantParams) {
			t.Fatalf("Router.MatchInto(%q) = %q %v %v; want %q %v %v", path, routerValue, params.All(), routerOK, want, wantParams.All(), wantOK)
		}
		params.Append("stale", "value")
		got, ok := matcher.MatchInto(path, &params)
		if got != want || ok != wantOK || !paramsEqual(params, wantParams) {
			t.Fatalf("compiled MatchInto(%q) = %q %v %v; want %q %v %v", path, got, params.All(), ok, want, wantParams.All(), wantOK)
		}
		params.Append("stale", "value")
		prefix, ok := matcher.MatchPrefixInto(path, &params)
		checkPrefix(prefix, ok)
		if !paramsEqual(params, wantPrefix.Params) {
			t.Fatalf("compiled prefix storage(%q) = %v; want %v", path, params.All(), wantPrefix.Params.All())
		}
	}
	checkAppendMatch(t, router, matcher, path)
}

func TestCompileMatching(t *testing.T) {
	paths := []string{"", "/", "//", "///", "/fixed", "/fixed/more", "/users/new", "/users/42/posts/99", "/users/", "/users//", "/files/report.json", "/files/.json", "/files/user-42/more", "/files/tail", "/specific/tail", "/other/tail", "/specific/tail/more", "/assets/prefix-css/site.css", "/assets/prefix-", "/many/a/b/c/d/e", "/many/a/b/c/d/e/more", "/many/a/b/c//e", "relative/value", "rel-value/more", "rel-fixed/more", "{literal}/value", "/empty//value", "//value/tail", "/ax/foo", "/ax/bar", "/ax/plain", "/user-42/more", "/user-43/more", "value", "value/extra", "/missing/path"}
	paths = append(paths, "docs/report.md", "docs/.md", "docs/tail", "docs/report.md/more", "rel/42x", "rel/x", "rel/", "/docs/report.md", "/rel/42x")
	for i, routes := range compiledRouteSets() {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				var router Router[string]
				for i := range routes {
					index := i
					if reverse {
						index = len(routes) - i - 1
					}
					router.Insert(routes[index], routes[index])
				}
				matcher := router.Compile()
				for _, path := range paths {
					checkCompiledMatch(t, &router, matcher, path)
				}
			}
		})
	}
}

func TestCompiledTerminalTable(t *testing.T) {
	var router Router[string]
	for i := range 1000 {
		route := "/route-" + strconv.Itoa(i) + "/{id" + strconv.Itoa(i) + "}"
		router.Insert(route, route)
	}
	router.Insert("/route-0/new", "new")
	for _, affixed := range []bool{false, true} {
		if affixed {
			router.Insert("/files/{name}.json", "json")
		}
		matcher := router.Compile()
		for i := range 1000 {
			path := "/route-" + strconv.Itoa(i) + "/value"
			checkCompiledMatch(t, &router, matcher, path)
			checkCompiledMatch(t, &router, matcher, path+"/missing")
		}
		checkCompiledMatch(t, &router, matcher, "/route-0/new")
		checkCompiledMatch(t, &router, matcher, "/files/report.json")
	}
}

func TestCompileConcurrentSnapshot(t *testing.T) {
	var router Router[int]
	router.Insert("/users/{id}", 1)
	router.Insert("/files/{name}.json", 2)
	matcher := router.Compile()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var params Params
			for range 100 {
				if value, params, ok := matcher.Match("/files/report.json"); !ok || value != 2 || params.Get("name") != "report" {
					t.Error("compiled match changed during source insertion")
				}
				if value, ok := matcher.MatchInto("/users/42", &params); !ok || value != 1 || params.Get("id") != "42" {
					t.Error("compiled match changed during source insertion")
				}
				if got, ok := matcher.MatchPrefix("/files/report.json/more"); !ok || got.Value != 2 || got.Rest != "/more" {
					t.Error("compiled prefix changed during source insertion")
				}
				if got, ok := matcher.MatchPrefixInto("/users/42/more", &params); !ok || got.Value != 1 || got.Rest != "/more" || params.Get("id") != "42" {
					t.Error("compiled prefix storage changed during source insertion")
				}
				if _, _, ok := matcher.Match("/assets/new"); ok {
					t.Error("compiled snapshot contains later catch-all")
				}
			}
		}()
	}
	close(start)
	router.Insert("/users/{x}/y", 3)
	router.Insert("/assets/{*path}", 4)
	for i := range 100 {
		router.Insert("/later/"+strconv.Itoa(i), i)
		router.Insert("/later/"+strconv.Itoa(i)+"/{id}", i)
		router.Insert("/files/{n}.txt"+strconv.Itoa(i), i)
	}
	wg.Wait()
}

func TestCompileAllocs(t *testing.T) {
	var router Router[int]
	router.Insert("/users/{id}", 1)
	router.Insert("/files/{name}.json", 2)
	router.Insert("/many/{a}/{b}/{c}/{d}/{e}", 3)
	matcher := router.Compile()
	params := NewParams(8)
	for _, path := range []string{"/users/42", "/files/report.json", "/many/a/b/c/d/e", "/missing"} {
		if allocs := testing.AllocsPerRun(100, func() { _, _ = matcher.MatchInto(path, &params) }); allocs != 0 {
			t.Fatalf("MatchInto(%q) allocs = %v", path, allocs)
		}
	}
	for _, path := range []string{"/users/42", "/files/report.json", "/missing"} {
		if allocs := testing.AllocsPerRun(100, func() { _, _, _ = matcher.Match(path) }); allocs != 0 {
			t.Fatalf("Match(%q) allocs = %v", path, allocs)
		}
	}
}

func FuzzCompiledMatch(f *testing.F) {
	var routers []Router[string]
	var matchers []*Matcher[string]
	for _, routes := range compiledRouteSets() {
		var router Router[string]
		for _, route := range routes {
			router.Insert(route, route)
		}
		routers = append(routers, router)
		matchers = append(matchers, router.Compile())
	}
	for _, path := range []string{"", "/", "//", "/users/42", "/files/report.json", "/files/tail", "/specific/tail", "/other/tail", "/many/a/b/c/d/e/more", "/ax/foo", "/ax/bar", "/user-42/more", "/user-43/more", "rel-fixed/more", "relative/value", "{literal}/value", "/assets/prefix-css/site.css", "docs/report.md", "docs/tail", "rel/42x", "/docs/report.md", "/rel/42x"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		for i := range routers {
			checkCompiledMatch(t, &routers[i], matchers[i], path)
		}
	})
}
