package dns

import "testing"

// Try suffixes from longest to shortest with the recursive exact matcher.
func referenceSuffix(router *Router[string], hostname string) (SuffixMatch[string], bool) {
	host, ok := hostnameWithinBounds(hostname)
	if !ok || !validHostnameLabels(host) {
		return SuffixMatch[string]{}, false
	}
	for start := 0; start < len(host); start++ {
		if start != 0 && host[start-1] != '.' {
			continue
		}
		if entry, ok := router.root.root.matchHost(host[start:], len(host)-start); ok {
			var params Params
			collectParams(entry, host[start:], 0, &params)
			prefix := ""
			if start > 0 {
				prefix = host[:start-1]
			}
			return SuffixMatch[string]{Value: entry.value, Params: params, Prefix: prefix}, true
		}
	}
	return SuffixMatch[string]{}, false
}

func FuzzMatchSuffix(f *testing.F) {
	patternSets := [][]string{
		{"com", "example.com", "www.example.com", "api.{tenant}.example.com", "api-{region}.example.com", "{*subdomain}.wild.test", "www.wild.test"},
		{"{a}.{b}.{c}.{d}.{e}.chain.test", "short.static.choice.test", "deep.long.{tenant}.choice.test", "specific.user-{id}.choice.test", "fallback.{tenant}.choice.test", "{{literal}}.{tenant}.escaped.test"},
		{"com", "example.com", "v1.example.com"},
	}
	var routers []Router[string]
	for _, patterns := range patternSets {
		var router Router[string]
		for _, pattern := range patterns {
			router.Insert(pattern, pattern)
		}
		routers = append(routers, router, router.Clone())
	}
	for _, host := range []string{"", ".", "..", "EXTRA.API.Tenant.Example.COM.", "API-US.Example.COM", "extra.www.example.com", "extra.www.wild.test", "a.b.wild.test", "extra.a.b.c.d.e.chain.test", "extra.deep.long.static.choice.test", "extra.specific.user-42.choice.test", "extra.fallback.user-42.choice.test", "extra.{LITERAL}.Tenant.escaped.test", "extra.v1.example.com", "bad..v1.example.com", ".api.tenant.example.com", "example.com..", "missing.other.test"} {
		f.Add(host)
	}
	f.Fuzz(func(t *testing.T, host string) {
		for i := range routers {
			router := &routers[i]
			want, wantOK := referenceSuffix(router, host)
			check := func(got SuffixMatch[string], ok bool) {
				if ok != wantOK || got.Value != want.Value || got.Prefix != want.Prefix || !paramsEqual(got.Params, want.Params) {
					t.Fatalf("suffix(%q) = %q %q %v %v; want %q %q %v %v", host, got.Value, got.Prefix, got.Params.All(), ok, want.Value, want.Prefix, want.Params.All(), wantOK)
				}
			}
			got, ok := router.MatchSuffix(host)
			check(got, ok)
			for _, capacity := range []int{0, 8} {
				params := NewParams(capacity)
				params.Append("stale", "value")
				got, ok = router.MatchSuffixInto(host, &params)
				check(got, ok)
				if !paramsEqual(params, want.Params) {
					t.Fatalf("reused suffix params = %v, want %v", params.All(), want.Params.All())
				}
			}
		}
	})
}
