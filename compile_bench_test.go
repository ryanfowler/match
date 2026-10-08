package match

import (
	"math/rand/v2"
	"strconv"
	"testing"
)

func BenchmarkCompiledMatch(b *testing.B) {
	benchmarkCompiledMatch(b, false)
}

func BenchmarkCompiledMatchInto(b *testing.B) {
	benchmarkCompiledMatch(b, true)
}

func benchmarkCompiledMatch(b *testing.B, into bool) {
	for _, tc := range []struct {
		name   string
		routes []string
		paths  []string
	}{
		{"Static1000", generatedBenchmarkRoutes(1000), generatedBenchmarkRoutes(1000)},
		{"Dynamic1000", generatedDynamicBenchmarkRoutes(1000), compiledDynamicPaths(1000)},
		{"Dynamic1000WithAffix", append(generatedDynamicBenchmarkRoutes(1000), "/files/{name}.json"), compiledDynamicPaths(1000)},
		{"Dynamic1000MixedAffix", append(generatedDynamicBenchmarkRoutes(1000), "/files/{name}.json"), compiledAffixPaths(1000)},
		{"Mixed", mixedBenchmarkRoutes(), []string{"/health", "/users/42", "/users/42/posts/99", "/teams/core/members/ana", "/api/projects/alpha/releases/2026", "/assets/css/app.css", "/unknown/path", "/api/projects/alpha/releases/2026/extra"}},
		{"UnrelatedAffix", append(mixedBenchmarkRoutes(), "/files/{name}.json"), []string{"/api/projects/alpha/releases/2026", "/users/42/posts/99", "/files/report.json", "/missing"}},
	} {
		// Shuffle deterministically so large tables exercise different branches
		// rather than favoring one hot path or sequential route traversal.
		rng := rand.New(rand.NewPCG(1, 2))
		rng.Shuffle(len(tc.paths), func(i, j int) { tc.paths[i], tc.paths[j] = tc.paths[j], tc.paths[i] })
		router := benchmarkRouter(b, tc.routes)
		matcher := router.Compile()
		for _, matcherFirst := range []bool{false, true} {
			order := "RouterFirst"
			methods := []struct {
				name     string
				compiled bool
			}{{"Router", false}, {"Matcher", true}}
			if matcherFirst {
				order = "MatcherFirst"
				methods[0], methods[1] = methods[1], methods[0]
			}
			b.Run(tc.name+"/"+order, func(b *testing.B) {
				for _, method := range methods {
					b.Run(method.name, func(b *testing.B) {
						benchmarkCompiledLookup(b, router, matcher, tc.paths, into, method.compiled)
					})
				}
			})
		}
	}
}

func benchmarkCompiledLookup(b *testing.B, router *Router[string], matcher *Matcher[string], paths []string, into, compiled bool) {
	b.ReportAllocs()
	if into {
		var params Params
		if compiled {
			for i := 0; i < b.N; i++ {
				benchString, benchOK = matcher.MatchInto(paths[i%len(paths)], &params)
				benchParamLen = params.Len()
			}
		} else {
			for i := 0; i < b.N; i++ {
				benchString, benchOK = router.MatchInto(paths[i%len(paths)], &params)
				benchParamLen = params.Len()
			}
		}
		return
	}
	if compiled {
		for i := 0; i < b.N; i++ {
			benchString, benchParams, benchOK = matcher.Match(paths[i%len(paths)])
		}
	} else {
		for i := 0; i < b.N; i++ {
			benchString, benchParams, benchOK = router.Match(paths[i%len(paths)])
		}
	}
}

func compiledAffixPaths(n int) []string {
	paths := compiledDynamicPaths(n)
	// Keep the 10% misses and send a further 10% of requests through the
	// affixed branch itself instead of measuring only unrelated lookups.
	for i := 5; i < len(paths); i += 10 {
		paths[i] = "/files/report-" + strconv.Itoa(i) + ".json"
	}
	return paths
}

func compiledDynamicPaths(n int) []string {
	paths := make([]string, n)
	for i := range paths {
		paths[i] = "/route-" + strconv.Itoa(i) + "/value"
		if i%10 == 0 {
			paths[i] += "/missing"
		}
	}
	return paths
}

func BenchmarkCompile(b *testing.B) {
	for _, tc := range []struct {
		name   string
		routes []string
	}{
		{"Mixed", mixedBenchmarkRoutes()},
		{"Dynamic1000", generatedDynamicBenchmarkRoutes(1000)},
	} {
		router := benchmarkRouter(b, tc.routes)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchMatcher = router.Compile()
			}
		})
	}
}

var benchMatcher *Matcher[string]
