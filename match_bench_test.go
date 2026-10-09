package match

import (
	"strconv"
	"testing"
)

var (
	benchString   string
	benchParams   Params
	benchParamLen int
	benchPrefix   PrefixMatch[string]
	benchRouter   Router[string]
	benchOK       bool
)

func BenchmarkMatch(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "Static",
			routes: []string{"/", "/home", "/about", "/contact"},
			path:   "/contact",
		},
		{
			name:   "Param",
			routes: []string{"/", "/users/{id}", "/users/{id}/posts", "/assets/{*path}"},
			path:   "/users/978",
		},
		{
			name:   "CatchAll",
			routes: []string{"/", "/users/{id}", "/assets/{*path}", "/favicon.ico"},
			path:   "/assets/css/app.css",
		},
		{
			name:   "Mixed",
			routes: mixedBenchmarkRoutes(),
			path:   "/api/projects/alpha/releases/2026",
		},
		{
			name:   "Many100",
			routes: generatedBenchmarkRoutes(100),
			path:   "/route/99/detail",
		},
		{
			name:   "Many1000",
			routes: generatedBenchmarkRoutes(1000),
			path:   "/route/999/detail",
		},
		{
			name:   "DynamicMany1000",
			routes: generatedDynamicBenchmarkRoutes(1000),
			path:   "/route-999/value",
		},
		{
			name:   "AffixedParam",
			routes: []string{"/", "/files/{name}.json", "/assets/{*path}"},
			path:   "/files/report.json",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.Match(bm.path)
			}
		})
	}
}

func BenchmarkMatchMiss(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "Mixed",
			routes: mixedBenchmarkRoutes(),
			path:   "/api/projects/alpha/releases/2026/extra",
		},
		{
			name:   "Many1000",
			routes: generatedBenchmarkRoutes(1000),
			path:   "/missing/999/detail",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.Match(bm.path)
			}
		})
	}
}

// Vary requests so measurements include different radix branches and misses.
func BenchmarkMatchVaryingPaths(b *testing.B) {
	for _, dynamic := range []bool{false, true} {
		name := "Static1000"
		routes := generatedBenchmarkRoutes(1000)
		paths := append([]string(nil), routes...)
		if dynamic {
			name = "Dynamic1000"
			routes = generatedDynamicBenchmarkRoutes(1000)
			for i := range paths {
				paths[i] = "/route-" + strconv.Itoa(i) + "/value"
			}
		}
		for i := 0; i < len(paths); i += 10 {
			paths[i] += "/missing"
		}
		b.Run(name, func(b *testing.B) {
			router := benchmarkRouter(b, routes)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.Match(paths[i%len(paths)])
			}
		})
	}
}

func BenchmarkMatchInto(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "Param",
			routes: []string{"/", "/users/{id}", "/users/{id}/posts", "/assets/{*path}"},
			path:   "/users/978",
		},
		{
			name:   "Mixed",
			routes: mixedBenchmarkRoutes(),
			path:   "/api/projects/alpha/releases/2026",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			params := NewParams(4)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchOK = router.MatchInto(bm.path, &params)
				benchParamLen = params.Len()
			}
		})
	}
}

func BenchmarkMatchPrefix(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "Static",
			routes: []string{"/", "/api", "/api/v1", "/assets"},
			path:   "/api/v1/users/42",
		},
		{
			name:   "Param",
			routes: []string{"/", "/api/{version}", "/assets/{*path}"},
			path:   "/api/v1/users/42",
		},
		{
			name:   "CatchAll",
			routes: []string{"/", "/api", "/assets/{*path}"},
			path:   "/assets/css/app.css",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchPrefix, benchOK = router.MatchPrefix(bm.path)
			}
		})
	}
}

func BenchmarkMatchPrefixInto(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
		path   string
	}{
		{
			name:   "Param",
			routes: []string{"/", "/api/{version}", "/assets/{*path}"},
			path:   "/api/v1/users/42",
		},
		{
			name:   "ManyParams",
			routes: []string{"/{a}/{b}/{c}/{d}/{e}"},
			path:   "/a/b/c/d/e/rest",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			params := NewParams(5)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchPrefix, benchOK = router.MatchPrefixInto(bm.path, &params)
			}
		})
	}
}

// BenchmarkMatchAppend measures nested dispatch: a prefix match selects a
// mount, then an exact match runs on the remaining path. Append collects the
// captures of both levels in one shared buffer. IntoMerge is the equivalent
// without the append methods: one buffer per level, then Merge.
func BenchmarkMatchAppend(b *testing.B) {
	benchmarks := []struct {
		name   string
		mounts []string
		routes []string
		path   string
	}{
		{
			name:   "Param",
			mounts: []string{"/", "/api/{version}", "/assets/{*path}"},
			routes: []string{"/", "/users/{id}", "/users/{id}/posts"},
			path:   "/api/v1/users/42",
		},
		{
			name:   "ManyParams",
			mounts: []string{"/", "/orgs/{org}/teams/{team}", "/assets/{*path}"},
			routes: []string{"/", "/repos/{repo}/issues/{issue}", "/repos/{repo}"},
			path:   "/orgs/acme/teams/core/repos/match/issues/7",
		},
	}

	for _, bm := range benchmarks {
		mountRouter := benchmarkRouter(b, bm.mounts)
		routeRouter := benchmarkRouter(b, bm.routes)
		mountMatcher, routeMatcher := mountRouter.Compile(), routeRouter.Compile()
		levels := []struct {
			name            string
			matchPrefix     func(string, *Params) (string, string, bool)
			match           func(string, *Params) (string, bool)
			matchPrefixInto func(string, *Params) (PrefixMatch[string], bool)
			matchInto       func(string, *Params) (string, bool)
		}{
			{"Router", mountRouter.MatchPrefixAppend, routeRouter.MatchAppend, mountRouter.MatchPrefixInto, routeRouter.MatchInto},
			{"Matcher", mountMatcher.MatchPrefixAppend, routeMatcher.MatchAppend, mountMatcher.MatchPrefixInto, routeMatcher.MatchInto},
		}

		for _, level := range levels {
			b.Run(bm.name+"/"+level.name+"/Append", func(b *testing.B) {
				var params Params
				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					params.Reset()
					var rest string
					_, rest, benchOK = level.matchPrefix(bm.path, &params)
					benchString, benchOK = level.match(rest, &params)
					benchParamLen = params.Len()
				}
			})

			b.Run(bm.name+"/"+level.name+"/IntoMerge", func(b *testing.B) {
				var mountParams, routeParams Params
				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					benchPrefix, benchOK = level.matchPrefixInto(bm.path, &mountParams)
					benchString, benchOK = level.matchInto(benchPrefix.Rest, &routeParams)
					benchParams = Merge(benchPrefix.Params, routeParams)
					benchParamLen = benchParams.Len()
				}
			})
		}
	}
}

func BenchmarkInsert(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
	}{
		{name: "Mixed", routes: mixedBenchmarkRoutes()},
		{name: "Many100", routes: generatedBenchmarkRoutes(100)},
		{name: "Many1000", routes: generatedBenchmarkRoutes(1000)},
		{name: "DynamicMany1000", routes: generatedDynamicBenchmarkRoutes(1000)},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var router Router[string]
				for _, route := range bm.routes {
					if err := router.TryInsert(route, route); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkClone(b *testing.B) {
	benchmarks := []struct {
		name   string
		routes []string
	}{
		{name: "Mixed", routes: mixedBenchmarkRoutes()},
		{name: "Many100", routes: generatedBenchmarkRoutes(100)},
		{name: "Many1000", routes: generatedBenchmarkRoutes(1000)},
		{name: "DynamicMany1000", routes: generatedDynamicBenchmarkRoutes(1000)},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.routes)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchRouter = router.Clone()
			}
		})
	}
}

func benchmarkRouter(b *testing.B, routes []string) *Router[string] {
	b.Helper()

	var router Router[string]
	for _, route := range routes {
		if err := router.TryInsert(route, route); err != nil {
			b.Fatal(err)
		}
	}
	return &router
}

func mixedBenchmarkRoutes() []string {
	return []string{
		"/",
		"/health",
		"/metrics",
		"/favicon.ico",
		"/users",
		"/users/{id}",
		"/users/{id}/settings",
		"/users/{id}/posts",
		"/users/{id}/posts/{post}",
		"/teams",
		"/teams/{team}",
		"/teams/{team}/members",
		"/teams/{team}/members/{member}",
		"/api/projects",
		"/api/projects/{project}",
		"/api/projects/{project}/releases",
		"/api/projects/{project}/releases/{year}",
		"/api/projects/{project}/releases/{year}/notes",
		"/api/search/{query}",
		"/assets/{*path}",
		"/static/{*path}",
		"/docs/{*path}",
	}
}

func generatedBenchmarkRoutes(n int) []string {
	routes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		routes = append(routes, "/route/"+strconv.Itoa(i)+"/detail")
	}
	return routes
}

func generatedDynamicBenchmarkRoutes(n int) []string {
	routes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		routes = append(routes, "/route-"+strconv.Itoa(i)+"/{id}")
	}
	return routes
}
