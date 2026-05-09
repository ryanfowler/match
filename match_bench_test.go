package match

import (
	"strconv"
	"testing"
)

var (
	benchString string
	benchParams Params
	benchOK     bool
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
				benchString, benchParams, benchOK = router.MatchInto(bm.path, params)
			}
		})
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
