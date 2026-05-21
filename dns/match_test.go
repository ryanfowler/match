package dns

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMatchCore(t *testing.T) {
	var router Router[string]
	for _, pattern := range []string{
		"example.com",
		"www.example.com",
		"{tenant}.example.com",
		"api-{region}.example.com",
		"{{literal}}.example.com",
	} {
		if err := router.TryInsert(pattern, pattern); err != nil {
			t.Fatalf("insert %q: %v", pattern, err)
		}
	}

	tests := []matchCase{
		{"EXAMPLE.com.", "example.com", Params{}},
		{"WWW.Example.Com", "www.example.com", Params{}},
		{"admin.example.com", "{tenant}.example.com", ParamsOf(Param{"tenant", "admin"})},
		{"api-US.example.com", "api-{region}.example.com", ParamsOf(Param{"region", "US"})},
		{"{literal}.example.com", "{{literal}}.example.com", Params{}},
	}

	for _, tt := range tests {
		got, params, ok := router.Match(tt.host)
		if !ok {
			t.Fatalf("match %q: not found", tt.host)
		}
		if got != tt.pattern {
			t.Fatalf("match %q pattern = %q, want %q", tt.host, got, tt.pattern)
		}
		if !paramsEqual(params, tt.params) {
			t.Fatalf("match %q params = %#v, want %#v", tt.host, params.All(), tt.params.All())
		}
	}
}

func TestMatchMissesMalformedHostnames(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "apex")
	router.Insert("{tenant}.example.com", "tenant")

	for _, host := range []string{"", ".", "example..com", ".example.com", strings.Repeat("a", 64) + ".com"} {
		if got, _, ok := router.Match(host); ok {
			t.Fatalf("match %q = %q, want miss", host, got)
		}
	}
}

func TestCatchAllMatchesLeadingLabels(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "apex")
	router.Insert("www.example.com", "www")
	router.Insert("{*subdomain}.example.com", "subdomain")

	tests := []matchCase{
		{"example.com", "apex", Params{}},
		{"www.example.com", "www", Params{}},
		{"api.example.com", "subdomain", ParamsOf(Param{"subdomain", "api"})},
		{"A.B.Example.COM.", "subdomain", ParamsOf(Param{"subdomain", "A.B"})},
	}

	for _, tt := range tests {
		got, params, ok := router.Match(tt.host)
		if !ok {
			t.Fatalf("match %q: not found", tt.host)
		}
		if got != tt.pattern {
			t.Fatalf("match %q pattern = %q, want %q", tt.host, got, tt.pattern)
		}
		if !paramsEqual(params, tt.params) {
			t.Fatalf("match %q params = %#v, want %#v", tt.host, params.All(), tt.params.All())
		}
	}
}

func TestPrefixedCatchAll(t *testing.T) {
	var router Router[string]
	router.Insert("svc-{*subdomain}.example.com", "svc")

	got, params, ok := router.Match("svc-api.us.example.com")
	if !ok {
		t.Fatal("match prefixed catch-all: not found")
	}
	if got != "svc" {
		t.Fatalf("value = %q, want svc", got)
	}
	if !paramsEqual(params, ParamsOf(Param{"subdomain", "api.us"})) {
		t.Fatalf("params = %#v, want subdomain=api.us", params.All())
	}

	if got, _, ok := router.Match("api.us.example.com"); ok {
		t.Fatalf("match without prefix = %q, want miss", got)
	}
}

func TestCatchAllCollectsSuffixParams(t *testing.T) {
	var router Router[string]
	router.Insert("{*subdomain}.{zone}.com", "zone")

	got, params, ok := router.Match("api.us.example.com")
	if !ok {
		t.Fatal("match catch-all with suffix param: not found")
	}
	if got != "zone" {
		t.Fatalf("value = %q, want zone", got)
	}
	if !paramsEqual(params, ParamsOf(Param{"subdomain", "api.us"}, Param{"zone", "example"})) {
		t.Fatalf("params = %#v, want subdomain and zone", params.All())
	}
}

func TestMatchChoosesRightmostLiteralSpecificity(t *testing.T) {
	for _, patterns := range [][]string{
		{"{host}.api.example.com", "foo.{env}.example.com"},
		{"foo.{env}.example.com", "{host}.api.example.com"},
	} {
		var router Router[string]
		for _, pattern := range patterns {
			router.Insert(pattern, pattern)
		}

		got, params, ok := router.Match("foo.api.example.com")
		if !ok {
			t.Fatal("match overlapping deterministic patterns: not found")
		}
		if got != "{host}.api.example.com" {
			t.Fatalf("pattern = %q, want {host}.api.example.com", got)
		}
		if !paramsEqual(params, ParamsOf(Param{"host", "foo"})) {
			t.Fatalf("params = %#v, want host=foo", params.All())
		}
	}
}

func TestInsertErrors(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		err     error
	}{
		{"empty", "", ErrInvalidHostname},
		{"root", ".", ErrInvalidHostname},
		{"empty middle label", "example..com", ErrInvalidHostname},
		{"leading dot", ".example.com", ErrInvalidHostname},
		{"long literal label", strings.Repeat("a", 64) + ".com", ErrInvalidHostname},
		{"unnamed param", "{}.example.com", ErrInvalidParam},
		{"unnamed catchall", "{*}.example.com", ErrInvalidParam},
		{"double params", "{foo}{bar}.example.com", ErrInvalidParamLabel},
		{"catchall not leftmost", "api.{*sub}.example.com", ErrInvalidCatchAll},
		{"catchall suffix", "svc-{*sub}x.example.com", ErrInvalidCatchAll},
		{"unmatched open", "{tenant.example.com", ErrInvalidParam},
		{"unmatched close", "tenant}.example.com", ErrInvalidParam},
		{"dot in param", "{tenant.name}.example.com", ErrInvalidParam},
		{"star in param name", "{tenant*name}.example.com", ErrInvalidParam},
		{"unescaped open brace in param", "{a{b}.example.com", ErrInvalidParam},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router[string]
			err := router.TryInsert(tt.pattern, tt.pattern)
			if !errors.Is(err, tt.err) {
				t.Fatalf("insert error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestConflicts(t *testing.T) {
	tests := []struct {
		first  string
		second string
	}{
		{"Example.COM.", "example.com"},
		{"{tenant}.example.com", "{name}.example.com"},
		{"api-{region}.example.com", "{service}-prod.example.com"},
		{"{*subdomain}.example.com", "{tenant}.example.com"},
		{"{*subdomain}.example.com", "{*subdomain}.api.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.first+" then "+tt.second, func(t *testing.T) {
			var router Router[string]
			if err := router.TryInsert(tt.first, tt.first); err != nil {
				t.Fatalf("insert %q: %v", tt.first, err)
			}
			var conflict *ConflictError
			if err := router.TryInsert(tt.second, tt.second); !errors.As(err, &conflict) {
				t.Fatalf("insert %q error = %v, want conflict", tt.second, err)
			}
			if conflict.With != trimRootDot(tt.first) {
				t.Fatalf("conflict with = %q, want %q", conflict.With, trimRootDot(tt.first))
			}
			if conflict.Pattern != trimRootDot(tt.second) {
				t.Fatalf("conflict pattern = %q, want %q", conflict.Pattern, trimRootDot(tt.second))
			}
		})
	}
}

func TestAllowsDisjointDynamicPatterns(t *testing.T) {
	tests := []struct {
		name    string
		first   string
		second  string
		matches []matchCase
	}{
		{
			name:   "affixed params with disjoint prefixes",
			first:  "api-{region}.example.com",
			second: "web-{region}.example.com",
			matches: []matchCase{
				{"api-us.example.com", "api-{region}.example.com", ParamsOf(Param{"region", "us"})},
				{"web-eu.example.com", "web-{region}.example.com", ParamsOf(Param{"region", "eu"})},
			},
		},
		{
			name:   "catchalls with disjoint prefixes",
			first:  "api-{*subdomain}.example.com",
			second: "web-{*subdomain}.example.com",
			matches: []matchCase{
				{"api-us.foo.example.com", "api-{*subdomain}.example.com", ParamsOf(Param{"subdomain", "us.foo"})},
				{"web-eu.foo.example.com", "web-{*subdomain}.example.com", ParamsOf(Param{"subdomain", "eu.foo"})},
			},
		},
		{
			name:   "static under catchall",
			first:  "{*subdomain}.example.com",
			second: "www.example.com",
			matches: []matchCase{
				{"www.example.com", "www.example.com", Params{}},
				{"api.example.com", "{*subdomain}.example.com", ParamsOf(Param{"subdomain", "api"})},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router[string]
			if err := router.TryInsert(tt.first, tt.first); err != nil {
				t.Fatalf("insert %q: %v", tt.first, err)
			}
			if err := router.TryInsert(tt.second, tt.second); err != nil {
				t.Fatalf("insert %q: %v", tt.second, err)
			}
			for _, m := range tt.matches {
				got, params, ok := router.Match(m.host)
				if !ok {
					t.Fatalf("match %q: not found", m.host)
				}
				if got != m.pattern {
					t.Fatalf("match %q pattern = %q, want %q", m.host, got, m.pattern)
				}
				if !paramsEqual(params, m.params) {
					t.Fatalf("match %q params = %#v, want %#v", m.host, params.All(), m.params.All())
				}
			}
		})
	}
}

func TestMatchSuffixChoosesLongestSuffix(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "zone")
	router.Insert("{tenant}.example.com", "tenant")
	router.Insert("v1.example.com", "v1")

	tests := []struct {
		host   string
		value  string
		prefix string
		params Params
	}{
		{"api.example.com", "tenant", "", ParamsOf(Param{"tenant", "api"})},
		{"foo.api.example.com", "tenant", "foo", ParamsOf(Param{"tenant", "api"})},
		{"foo.v1.example.com", "v1", "foo", Params{}},
		{"foo.bar.example.com", "tenant", "foo", ParamsOf(Param{"tenant", "bar"})},
	}

	for _, tt := range tests {
		got, ok := router.MatchSuffix(tt.host)
		if !ok {
			t.Fatalf("MatchSuffix(%q): not found", tt.host)
		}
		if got.Value != tt.value || got.Prefix != tt.prefix {
			t.Fatalf("MatchSuffix(%q) = value %q prefix %q, want %q %q", tt.host, got.Value, got.Prefix, tt.value, tt.prefix)
		}
		if !paramsEqual(got.Params, tt.params) {
			t.Fatalf("MatchSuffix(%q) params = %#v, want %#v", tt.host, got.Params.All(), tt.params.All())
		}
	}
}

func TestMatchSuffixCatchAllConsumesPrefix(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "zone")
	router.Insert("{*subdomain}.example.com", "subdomain")

	got, ok := router.MatchSuffix("api.us.example.com")
	if !ok {
		t.Fatal("MatchSuffix catch-all: not found")
	}
	if got.Value != "subdomain" || got.Prefix != "" {
		t.Fatalf("MatchSuffix = value %q prefix %q, want subdomain empty", got.Value, got.Prefix)
	}
	if !paramsEqual(got.Params, ParamsOf(Param{"subdomain", "api.us"})) {
		t.Fatalf("params = %#v, want subdomain=api.us", got.Params.All())
	}
}

func TestMatchSuffixMiss(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "zone")

	for _, host := range []string{"other.test", "example..com"} {
		if got, ok := router.MatchSuffix(host); ok {
			t.Fatalf("MatchSuffix(%q) = value %q prefix %q, want miss", host, got.Value, got.Prefix)
		}
	}
}

func TestRouterCloneCopiesState(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "zone")
	router.Insert("{tenant}.example.com", "tenant")
	for i := 0; i < 12; i++ {
		router.Insert(fmt.Sprintf("static-%02d.test", i), fmt.Sprintf("static-%02d", i))
	}

	clone := router.Clone()
	clone.Insert("clone-only.test", "clone")
	router.Insert("original-only.test", "original")

	got, params, ok := clone.Match("api.example.com")
	if !ok {
		t.Fatal("clone match tenant: not found")
	}
	if got != "tenant" || !paramsEqual(params, ParamsOf(Param{"tenant", "api"})) {
		t.Fatalf("clone tenant = %q %#v", got, params.All())
	}

	if got, _, ok := clone.Match("static-11.test"); !ok || got != "static-11" {
		t.Fatalf("clone indexed static = %q, %v; want static-11 true", got, ok)
	}
	if got, _, ok := clone.Match("clone-only.test"); !ok || got != "clone" {
		t.Fatalf("clone-only = %q, %v; want clone true", got, ok)
	}
	if got, _, ok := router.Match("original-only.test"); !ok || got != "original" {
		t.Fatalf("original-only = %q, %v; want original true", got, ok)
	}
	if got, _, ok := router.Match("clone-only.test"); ok {
		t.Fatalf("original matched clone-only with value %q", got)
	}
	if got, _, ok := clone.Match("original-only.test"); ok {
		t.Fatalf("clone matched original-only with value %q", got)
	}
}

func TestMatchIntoReusesParams(t *testing.T) {
	var router Router[string]
	router.Insert("{a}.{b}.{c}.{d}.{e}.example.com", "many")

	buf := NewParams(5)
	allocs := testing.AllocsPerRun(100, func() {
		got, params, ok := router.MatchInto("a.b.c.d.e.example.com", buf)
		if !ok {
			t.Fatal("MatchInto did not match")
		}
		if got != "many" {
			t.Fatalf("value = %q, want many", got)
		}
		if params.Len() != 5 {
			t.Fatalf("params length = %d, want 5", params.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per MatchInto = %v, want 0", allocs)
	}
}

func TestMatchIntoMissPreservesHeapParams(t *testing.T) {
	var router Router[string]
	router.Insert("{a}.{b}.{c}.{d}.{e}.example.com", "many")

	buf := NewParams(5)
	_, params, ok := router.MatchInto("missing.example.com", buf)
	if ok {
		t.Fatal("MatchInto matched unexpected hostname")
	}
	if params.Len() != 0 {
		t.Fatalf("miss params length = %d, want 0", params.Len())
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, matchedParams, ok := router.MatchInto("a.b.c.d.e.example.com", params)
		if !ok {
			t.Fatal("MatchInto did not match")
		}
		if matchedParams.Len() != 5 {
			t.Fatalf("params length = %d, want 5", matchedParams.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per MatchInto after miss = %v, want 0", allocs)
	}
}

func TestMatchSuffixIntoReusesParams(t *testing.T) {
	var router Router[string]
	router.Insert("{tenant}.example.com", "tenant")

	buf := NewParams(1)
	allocs := testing.AllocsPerRun(100, func() {
		got, ok := router.MatchSuffixInto("api.tenant.example.com", buf)
		if !ok {
			t.Fatal("MatchSuffixInto did not match")
		}
		if got.Value != "tenant" || got.Prefix != "api" {
			t.Fatalf("MatchSuffixInto = value %q prefix %q, want tenant api", got.Value, got.Prefix)
		}
		if !paramsEqual(got.Params, ParamsOf(Param{"tenant", "tenant"})) {
			t.Fatalf("params = %#v, want tenant=tenant", got.Params.All())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per MatchSuffixInto = %v, want 0", allocs)
	}
}

type matchCase struct {
	host    string
	pattern string
	params  Params
}

func paramsEqual(a, b Params) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if a.At(i) != b.At(i) {
			return false
		}
	}
	return true
}
