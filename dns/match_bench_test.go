package dns

import (
	"strconv"
	"testing"
)

var (
	benchString string
	benchParams Params
	benchSuffix SuffixMatch[string]
	benchRouter Router[string]
	benchOK     bool
)

func BenchmarkMatch(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
		host     string
	}{
		{
			name:     "Static",
			patterns: []string{"example.com", "www.example.com", "api.example.com", "status.example.com"},
			host:     "status.example.com",
		},
		{
			name:     "Param",
			patterns: []string{"example.com", "www.example.com", "{tenant}.example.com"},
			host:     "api.example.com",
		},
		{
			name:     "CatchAll",
			patterns: []string{"wild.example.com", "www.wild.example.com", "{*subdomain}.wild.example.com"},
			host:     "api.us.wild.example.com",
		},
		{
			name:     "PrefixedCatchAll",
			patterns: []string{"wild.example.com", "svc-{*subdomain}.wild.example.com"},
			host:     "svc-api.us.wild.example.com",
		},
		{
			name:     "Mixed",
			patterns: mixedBenchmarkPatterns(),
			host:     "api-us.example.com",
		},
		{
			name:     "Many100",
			patterns: generatedBenchmarkPatterns(100),
			host:     "route-99.example.com",
		},
		{
			name:     "Many1000",
			patterns: generatedBenchmarkPatterns(1000),
			host:     "route-999.example.com",
		},
		{
			name:     "Many1000Uppercase",
			patterns: generatedBenchmarkPatterns(1000),
			host:     "ROUTE-999.EXAMPLE.COM",
		},
		{
			name:     "DynamicMany1000",
			patterns: generatedDynamicBenchmarkPatterns(1000),
			host:     "route-999.api.example.com",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.Match(bm.host)
			}
		})
	}
}

func BenchmarkMatchMiss(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
		host     string
	}{
		{
			name:     "Mixed",
			patterns: mixedBenchmarkPatterns(),
			host:     "missing.example.org",
		},
		{
			name:     "Many1000",
			patterns: generatedBenchmarkPatterns(1000),
			host:     "missing.example.com",
		},
		{
			name:     "DynamicMany1000",
			patterns: generatedDynamicBenchmarkPatterns(1000),
			host:     "missing.api.example.com",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.Match(bm.host)
			}
		})
	}
}

func BenchmarkMatchInto(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
		host     string
		capacity int
	}{
		{
			name:     "Param",
			patterns: []string{"example.com", "www.example.com", "{tenant}.example.com"},
			host:     "api.example.com",
			capacity: 1,
		},
		{
			name:     "ManyParams",
			patterns: []string{"{a}.{b}.{c}.{d}.{e}.example.com"},
			host:     "a.b.c.d.e.example.com",
			capacity: 5,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			params := NewParams(bm.capacity)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchString, benchParams, benchOK = router.MatchInto(bm.host, params)
			}
		})
	}
}

func BenchmarkMatchSuffix(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
		host     string
	}{
		{
			name:     "Static",
			patterns: []string{"com", "example.com", "v1.example.com"},
			host:     "api.v1.example.com",
		},
		{
			name:     "Param",
			patterns: []string{"example.com", "{tenant}.example.com"},
			host:     "api.tenant.example.com",
		},
		{
			name:     "CatchAll",
			patterns: []string{"wild.example.com", "{*subdomain}.wild.example.com"},
			host:     "api.us.wild.example.com",
		},
		{
			name:     "MissMany1000",
			patterns: generatedBenchmarkPatterns(1000),
			host:     "missing.example.com",
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchSuffix, benchOK = router.MatchSuffix(bm.host)
			}
		})
	}
}

func BenchmarkMatchSuffixInto(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
		host     string
		capacity int
	}{
		{
			name:     "Param",
			patterns: []string{"example.com", "{tenant}.example.com"},
			host:     "api.tenant.example.com",
			capacity: 1,
		},
		{
			name:     "ManyParams",
			patterns: []string{"{a}.{b}.{c}.{d}.{e}.example.com"},
			host:     "prefix.a.b.c.d.e.example.com",
			capacity: 5,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			params := NewParams(bm.capacity)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchSuffix, benchOK = router.MatchSuffixInto(bm.host, params)
			}
		})
	}
}

func BenchmarkInsert(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
	}{
		{name: "Mixed", patterns: mixedBenchmarkPatterns()},
		{name: "Many100", patterns: generatedBenchmarkPatterns(100)},
		{name: "Many1000", patterns: generatedBenchmarkPatterns(1000)},
		{name: "DynamicMany1000", patterns: generatedDynamicBenchmarkPatterns(1000)},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var router Router[string]
				for _, pattern := range bm.patterns {
					if err := router.TryInsert(pattern, pattern); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkClone(b *testing.B) {
	benchmarks := []struct {
		name     string
		patterns []string
	}{
		{name: "Mixed", patterns: mixedBenchmarkPatterns()},
		{name: "Many100", patterns: generatedBenchmarkPatterns(100)},
		{name: "Many1000", patterns: generatedBenchmarkPatterns(1000)},
		{name: "DynamicMany1000", patterns: generatedDynamicBenchmarkPatterns(1000)},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			router := benchmarkRouter(b, bm.patterns)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				benchRouter = router.Clone()
			}
		})
	}
}

func benchmarkRouter(b *testing.B, patterns []string) *Router[string] {
	b.Helper()

	var router Router[string]
	for _, pattern := range patterns {
		if err := router.TryInsert(pattern, pattern); err != nil {
			b.Fatal(err)
		}
	}
	return &router
}

func mixedBenchmarkPatterns() []string {
	return []string{
		"example.com",
		"www.example.com",
		"api.example.com",
		"{tenant}.example.com",
		"api-{region}.example.com",
		"status.service.internal",
		"{service}.svc.cluster.local",
		"wild.example.com",
		"{*subdomain}.wild.example.com",
	}
}

func generatedBenchmarkPatterns(n int) []string {
	patterns := make([]string, 0, n)
	for i := 0; i < n; i++ {
		patterns = append(patterns, "route-"+strconv.Itoa(i)+".example.com")
	}
	return patterns
}

func generatedDynamicBenchmarkPatterns(n int) []string {
	patterns := make([]string, 0, n)
	for i := 0; i < n; i++ {
		patterns = append(patterns, "route-"+strconv.Itoa(i)+".{id}.example.com")
	}
	return patterns
}
