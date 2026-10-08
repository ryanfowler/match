package dns

import (
	"strings"
	"testing"
)

func TestSingleCaptureOffsets(t *testing.T) {
	for _, tc := range []struct {
		pattern, host, value string
	}{
		{"api.pre-{region}-post.example.com", "API.PRE-US-POST.Example.COM.", "US"},
		{"{tenant}.example.com", "Tenant.Example.COM", "Tenant"},
		{"api.example.{tld}", "API.EXAMPLE.COM", "COM"},
		{"svc-{*region}.example.com", "SVC-US.East.Example.COM", "US.East"},
		{"{{api}}.{tenant}.example.com", "{API}.Tenant.Example.COM", "Tenant"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			var router Router[string]
			router.Insert(tc.pattern, tc.pattern)
			for _, r := range []Router[string]{router, router.Clone()} {
				got, params, ok := r.Match(tc.host)
				if !ok || got != tc.pattern || params.Len() != 1 || params.At(0).Val != tc.value {
					t.Fatalf("Match(%q) = %q %v %v", tc.host, got, params.All(), ok)
				}
				reused := NewParams(8)
				if got, ok := r.MatchInto(tc.host, &reused); !ok || got != tc.pattern || !paramsEqual(params, reused) {
					t.Fatalf("MatchInto(%q) = %q %v %v", tc.host, got, reused.All(), ok)
				}
				match, ok := r.MatchSuffix(tc.host)
				if !ok || !paramsEqual(params, match.Params) || match.Prefix != "" {
					t.Fatalf("MatchSuffix(%q) = %+v %v", tc.host, match, ok)
				}
				if !strings.Contains(tc.pattern, "{*") {
					match, ok = r.MatchSuffixInto("extra."+tc.host, &reused)
					if !ok || !paramsEqual(params, match.Params) || match.Prefix != "extra" {
						t.Fatalf("MatchSuffixInto(%q) = %+v %v", tc.host, match, ok)
					}
				}
			}
		})
	}
}

func TestStaticFoldedLookupPreservesBytes(t *testing.T) {
	maxHost := hostnameWithLabelLengths(63, 63, 63, 61)
	for _, tc := range []struct{ pattern, host string }{
		{"é.example.com", "é.Example.COM."},
		{"{{literal}}.example.com", "{LITERAL}.EXAMPLE.COM"},
		{maxHost, strings.ToUpper(maxHost) + "."},
	} {
		var router Router[string]
		router.Insert(tc.pattern, "value")
		for _, r := range []Router[string]{router, router.Clone()} {
			if got, params, ok := r.Match(tc.host); !ok || got != "value" || params.Len() != 0 {
				t.Fatalf("Match(%q) = %q %v %v", tc.host, got, params.All(), ok)
			}
			params := NewParams(8)
			if got, ok := r.MatchInto(tc.host, &params); !ok || got != "value" || params.Len() != 0 {
				t.Fatalf("MatchInto(%q) = %q %v %v", tc.host, got, params.All(), ok)
			}
		}
	}
}

func TestFoldedLookupMissesAndMalformedHostnames(t *testing.T) {
	var router Router[string]
	router.Insert("example.com", "zone")
	router.Insert("www.example.com", "www")
	for _, host := range []string{
		"", ".", "..", "EXAMPLE.ORG", "WWW.EXAMPLE.ORG",
		"a..b", ".EXAMPLE.COM", "EXAMPLE..COM", "EXAMPLE.COM..",
		strings.Repeat("A", 64) + ".EXAMPLE.COM",
		strings.ToUpper(hostnameWithLabelLengths(63, 63, 63, 62)) + ".",
	} {
		t.Run(host, func(t *testing.T) {
			for _, r := range []Router[string]{router, router.Clone()} {
				if got, params, ok := r.Match(host); ok || got != "" || params.Len() != 0 {
					t.Fatalf("Match(%q) = %q %v %v, want empty miss", host, got, params.All(), ok)
				}
				params := NewParams(8)
				params.Append("stale", "value")
				if got, ok := r.MatchInto(host, &params); ok || got != "" || params.Len() != 0 {
					t.Fatalf("MatchInto(%q) = %q %v %v, want empty miss", host, got, params.All(), ok)
				}
				if got, ok := r.MatchSuffix(host); ok || got.Value != "" || got.Prefix != "" || got.Params.Len() != 0 {
					t.Fatalf("MatchSuffix(%q) = %+v %v, want empty miss", host, got, ok)
				}
				params.Append("stale", "value")
				if got, ok := r.MatchSuffixInto(host, &params); ok || got.Value != "" || got.Prefix != "" || params.Len() != 0 || got.Params.Len() != 0 {
					t.Fatalf("MatchSuffixInto(%q) = %+v %v, want empty miss", host, got, ok)
				}
			}
		})
	}
}

func TestSingleCatchAllSuffixCapturesExtraPrefix(t *testing.T) {
	var router Router[string]
	router.Insert("{*region}.example.com", "region")
	for _, r := range []Router[string]{router, router.Clone()} {
		want := ParamsOf(Param{Key: "region", Val: "Extra.US.East"})
		match, ok := r.MatchSuffix("Extra.US.East.Example.COM.")
		if !ok || match.Value != "region" || match.Prefix != "" || !paramsEqual(match.Params, want) {
			t.Fatalf("MatchSuffix = %+v %v, want full capture %v", match, ok, want.All())
		}
		params := NewParams(8)
		params.Append("stale", "value")
		match, ok = r.MatchSuffixInto("Extra.US.East.Example.COM.", &params)
		if !ok || match.Value != "region" || match.Prefix != "" || !paramsEqual(match.Params, want) || !paramsEqual(params, want) {
			t.Fatalf("MatchSuffixInto = %+v %v, want full capture %v", match, ok, want.All())
		}
	}
}
