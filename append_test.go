package match

import (
	"strconv"
	"testing"
)

// seedParams returns a Params value with count parameters and the given
// capacity hint, so append tests cover inline, spilled, and preallocated
// storage.
func seedParams(count, capacity int) Params {
	params := NewParams(capacity)
	for i := range count {
		params.Append("seed"+strconv.Itoa(i), "value"+strconv.Itoa(i))
	}
	return params
}

// checkAppendMatch verifies that the append variants of router and matcher
// agree with Match and MatchPrefix for path, keep the parameters already in
// the buffer, and leave the buffer unchanged on a miss.
func checkAppendMatch(t *testing.T, router *Router[string], matcher *Matcher[string], path string) {
	t.Helper()
	want, wantParams, wantOK := router.Match(path)
	wantPrefix, wantPrefixOK := router.MatchPrefix(path)

	for _, seed := range []struct{ count, capacity int }{{0, 0}, {1, 0}, {3, 0}, {4, 0}, {6, 0}, {2, 16}} {
		seeded := seedParams(seed.count, seed.capacity)
		exact := []struct {
			name  string
			match func(string, *Params) (string, bool)
		}{
			{"Router.MatchAppend", router.MatchAppend},
			{"Matcher.MatchAppend", matcher.MatchAppend},
		}
		for _, tc := range exact {
			params := seedParams(seed.count, seed.capacity)
			got, ok := tc.match(path, &params)
			if got != want || ok != wantOK || !paramsEqual(params, Merge(seeded, wantParams)) {
				t.Fatalf("%s(%q) with %d seeded = %q %v %v; want %q %v %v", tc.name, path, seed.count, got, params.All(), ok, want, Merge(seeded, wantParams).All(), wantOK)
			}
		}

		prefix := []struct {
			name  string
			match func(string, *Params) (string, string, bool)
		}{
			{"Router.MatchPrefixAppend", router.MatchPrefixAppend},
			{"Matcher.MatchPrefixAppend", matcher.MatchPrefixAppend},
		}
		for _, tc := range prefix {
			params := seedParams(seed.count, seed.capacity)
			got, rest, ok := tc.match(path, &params)
			if got != wantPrefix.Value || rest != wantPrefix.Rest || ok != wantPrefixOK || !paramsEqual(params, Merge(seeded, wantPrefix.Params)) {
				t.Fatalf("%s(%q) with %d seeded = %q %q %v %v; want %q %q %v %v", tc.name, path, seed.count, got, rest, params.All(), ok, wantPrefix.Value, wantPrefix.Rest, Merge(seeded, wantPrefix.Params).All(), wantPrefixOK)
			}
		}
	}
}

func TestMatchAppendNestedLevels(t *testing.T) {
	var mounts, routes Router[string]
	mounts.Insert("/orgs/{org}", "mount")
	routes.Insert("/repos/{repo}/issues/{issue}", "issue")

	for _, compiled := range []bool{false, true} {
		matchPrefix, match := mounts.MatchPrefixAppend, routes.MatchAppend
		if compiled {
			matchPrefix, match = mounts.Compile().MatchPrefixAppend, routes.Compile().MatchAppend
		}

		var params Params
		value, rest, ok := matchPrefix("/orgs/acme/repos/match/issues/7", &params)
		if !ok || value != "mount" || rest != "/repos/match/issues/7" {
			t.Fatalf("MatchPrefixAppend = %q %q %v", value, rest, ok)
		}
		if value, ok := match(rest, &params); !ok || value != "issue" {
			t.Fatalf("MatchAppend(%q) = %q %v", rest, value, ok)
		}
		want := ParamsOf(Param{"org", "acme"}, Param{"repo", "match"}, Param{"issue", "7"})
		if !paramsEqual(params, want) {
			t.Fatalf("params = %v; want %v", params.All(), want.All())
		}
	}
}

func TestMatchAppendRadixBacktrackKeepsExistingParams(t *testing.T) {
	var router Router[string]
	router.Insert("/x/{a}/{b}/{c}/{d}/{e}/end", "static")
	router.Insert("/{p}/{q}/{r}/{s}/{t}/{u}/other", "param")

	for _, compiled := range []bool{false, true} {
		match := router.MatchAppend
		if compiled {
			match = router.Compile().MatchAppend
		}
		for _, tc := range []struct {
			path, value string
			want        []Param
		}{
			{"/x/1/2/3/4/5/end", "static", []Param{{"a", "1"}, {"b", "2"}, {"c", "3"}, {"d", "4"}, {"e", "5"}}},
			// The static branch appends five values before it fails.
			{"/x/1/2/3/4/5/other", "param", []Param{{"p", "x"}, {"q", "1"}, {"r", "2"}, {"s", "3"}, {"t", "4"}, {"u", "5"}}},
			{"/x/1/2/3/4/5/miss", "", nil},
			{"/x/1", "", nil},
		} {
			for _, seedCount := range []int{0, 2, 4, 5} {
				seeded := seedParams(seedCount, 0)
				params := seedParams(seedCount, 0)
				value, ok := match(tc.path, &params)
				if value != tc.value || ok != (tc.value != "") || !paramsEqual(params, Merge(seeded, ParamsOf(tc.want...))) {
					t.Fatalf("MatchAppend(%q) with %d seeded = %q %v %v", tc.path, seedCount, value, params.All(), ok)
				}
			}
		}
	}
}

func TestMatchAppendReusesHeapParams(t *testing.T) {
	var mounts, routes Router[string]
	mounts.Insert("/{a}/{b}/{c}", "mount")
	routes.Insert("/{d}/{e}/{f}", "route")
	mountMatcher, routeMatcher := mounts.Compile(), routes.Compile()

	params := NewParams(6)
	allocs := testing.AllocsPerRun(100, func() {
		params.Reset()
		_, rest, ok := mountMatcher.MatchPrefixAppend("/1/2/3/4/5/6", &params)
		if !ok {
			t.Fatal("MatchPrefixAppend did not match")
		}
		if _, ok := routeMatcher.MatchAppend(rest, &params); !ok || params.Len() != 6 {
			t.Fatalf("MatchAppend(%q) = %v with %d params", rest, ok, params.Len())
		}
		params.Truncate(3)
		if _, ok := routes.MatchAppend(rest, &params); !ok || params.Len() != 6 {
			t.Fatalf("Router.MatchAppend(%q) = %v with %d params", rest, ok, params.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per append match = %v, want 0", allocs)
	}
}

func TestParamsTruncate(t *testing.T) {
	for _, count := range []int{3, 6} {
		params := seedParams(count, 0)
		params.Truncate(count)
		if params.Len() != count {
			t.Fatalf("Truncate(%d) changed length to %d", count, params.Len())
		}
		params.Truncate(1)
		params.Append("next", "value")
		want := ParamsOf(Param{"seed0", "value0"}, Param{"next", "value"})
		if !paramsEqual(params, want) {
			t.Fatalf("params after Truncate and Append = %v; want %v", params.All(), want.All())
		}
		params.Truncate(0)
		if params.Len() != 0 {
			t.Fatalf("Truncate(0) left %d params", params.Len())
		}
	}
}

func TestParamsSetVal(t *testing.T) {
	for _, count := range []int{3, 6} {
		params := seedParams(count, 0)
		params.SetVal(count-1, "a/b")
		for i := range count {
			want := Param{"seed" + strconv.Itoa(i), "value" + strconv.Itoa(i)}
			if i == count-1 {
				want.Val = "a/b"
			}
			if got := params.At(i); got != want {
				t.Fatalf("At(%d) = %v; want %v", i, got, want)
			}
		}
	}
}

func TestParamsEditorsPanicOutOfRange(t *testing.T) {
	for name, edit := range map[string]func(*Params){
		"Truncate(-1)":    func(p *Params) { p.Truncate(-1) },
		"Truncate(Len+1)": func(p *Params) { p.Truncate(3) },
		"SetVal(-1)":      func(p *Params) { p.SetVal(-1, "") },
		"SetVal(Len)":     func(p *Params) { p.SetVal(2, "") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s did not panic", name)
				}
			}()
			params := seedParams(2, 0)
			edit(&params)
		}()
	}
}
