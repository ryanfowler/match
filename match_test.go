package match

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestMatchitCoreMatches(t *testing.T) {
	tests := []struct {
		name    string
		routes  []string
		matches []matchCase
	}{
		{
			name:   "blog",
			routes: []string{"/{page}", "/posts/{year}/{month}/{post}", "/posts/{year}/{month}/index", "/posts/{year}/top", "/static/{*path}", "/favicon.ico"},
			matches: []matchCase{
				{"/about", "/{page}", ParamsOf(Param{"page", "about"})},
				{"/posts/2021/01/rust", "/posts/{year}/{month}/{post}", ParamsOf(Param{"year", "2021"}, Param{"month", "01"}, Param{"post", "rust"})},
				{"/posts/2021/01/index", "/posts/{year}/{month}/index", ParamsOf(Param{"year", "2021"}, Param{"month", "01"})},
				{"/posts/2021/top", "/posts/{year}/top", ParamsOf(Param{"year", "2021"})},
				{"/static/foo.png", "/static/{*path}", ParamsOf(Param{"path", "foo.png"})},
				{"/favicon.ico", "/favicon.ico", Params{}},
			},
		},
		{
			name:   "wildcard suffix",
			routes: []string{"/", "/{foo}x", "/foox", "/{foo}x/bar", "/{foo}x/bar/baz"},
			matches: []matchCase{
				{"/", "/", Params{}},
				{"/foox", "/foox", Params{}},
				{"/barx", "/{foo}x", ParamsOf(Param{"foo", "bar"})},
				{"/mx", "/{foo}x", ParamsOf(Param{"foo", "m"})},
				{"/mx/bar", "/{foo}x/bar", ParamsOf(Param{"foo", "m"})},
				{"/xfoox/bar/baz", "/{foo}x/bar/baz", ParamsOf(Param{"foo", "xfoo"})},
			},
		},
		{
			name:   "catchall overlap",
			routes: []string{"/path/foo", "/path/{*rest}"},
			matches: []matchCase{
				{"/path/foo", "/path/foo", Params{}},
				{"/path/bar", "/path/{*rest}", ParamsOf(Param{"rest", "bar"})},
				{"/path/foo/", "/path/{*rest}", ParamsOf(Param{"rest", "foo/"})},
			},
		},
		{
			name:   "escaped",
			routes: []string{"/", "/{{", "/}}", "/{ba{{r}", "/baz/{xxx}/}}xy{{{{", "/{{/{x}"},
			matches: []matchCase{
				{"/", "/", Params{}},
				{"/{", "/{{", Params{}},
				{"/}", "/}}", Params{}},
				{"/foo", "/{ba{{r}", ParamsOf(Param{"ba{r", "foo"})},
				{"/baz/x/}xy{{", "/baz/{xxx}/}}xy{{{{", ParamsOf(Param{"xxx", "x"})},
				{"/{/{{", "/{{/{x}", ParamsOf(Param{"x", "{{"})},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router[string]
			for _, route := range tt.routes {
				if err := router.TryInsert(route, route); err != nil {
					t.Fatalf("insert %q: %v", route, err)
				}
			}
			for _, m := range tt.matches {
				got, params, ok := router.Match(m.path)
				if !ok {
					t.Fatalf("match %q: not found", m.path)
				}
				if got != m.route {
					t.Fatalf("match %q route = %q, want %q", m.path, got, m.route)
				}
				if !paramsEqual(params, m.params) {
					t.Fatalf("match %q params = %#v, want %#v", m.path, params, m.params)
				}
			}
		})
	}
}

func TestMatchitMisses(t *testing.T) {
	var router Router[string]
	for _, route := range []string{"/y/{foo}", "/x/{foo}/z", "/z/{*foo}", "/a/x{foo}", "/b/{foo}x"} {
		if err := router.TryInsert(route, route); err != nil {
			t.Fatalf("insert %q: %v", route, err)
		}
	}
	for _, path := range []string{"/y/", "/x//z", "/z/", "/a/x", "/b/x"} {
		if got, _, ok := router.Match(path); ok {
			t.Fatalf("match %q = %q, want miss", path, got)
		}
	}
}

func TestMatchRootCatchAllFallbackWithAbsoluteRoutes(t *testing.T) {
	var router Router[string]
	router.Insert("{*path}", "catch-all")
	router.Insert("/fixed", "fixed")

	got, params, ok := router.Match("/other")
	if !ok {
		t.Fatal("match root catch-all: not found")
	}
	if got != "catch-all" {
		t.Fatalf("match root catch-all route = %q, want catch-all", got)
	}
	if !paramsEqual(params, ParamsOf(Param{"path", "/other"})) {
		t.Fatalf("match root catch-all params = %#v", params)
	}

	got, params, ok = router.Match("/fixed")
	if !ok {
		t.Fatal("match absolute route: not found")
	}
	if got != "fixed" {
		t.Fatalf("match absolute route = %q, want fixed", got)
	}
	if params.Len() != 0 {
		t.Fatalf("match absolute params length = %d, want 0", params.Len())
	}
}

func TestMatchitInsertErrors(t *testing.T) {
	tests := []struct {
		name  string
		route string
		err   error
	}{
		{"unnamed param", "/{}", ErrInvalidParam},
		{"unnamed catchall", "/src/{*}", ErrInvalidParam},
		{"double params", "/{foo}{bar}", ErrInvalidParamSegment},
		{"catchall suffix", "/src/{*filepath}x", ErrInvalidCatchAll},
		{"unmatched open", "x{y", ErrInvalidParam},
		{"unmatched close", "x}", ErrInvalidParam},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router[string]
			err := router.TryInsert(tt.route, tt.route)
			if !errors.Is(err, tt.err) {
				t.Fatalf("insert error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestMatchitConflicts(t *testing.T) {
	tests := []struct {
		first  string
		second string
	}{
		{"/", "/"},
		{"/x/{foo}/bar", "/x/{bar}/bar"},
		{"/src/{*filepath}", "/src/{file}"},
		{"/user_{name}", "/user_{bar}"},
	}

	for _, tt := range tests {
		var router Router[string]
		if err := router.TryInsert(tt.first, tt.first); err != nil {
			t.Fatalf("insert %q: %v", tt.first, err)
		}
		var conflict *ConflictError
		if err := router.TryInsert(tt.second, tt.second); !errors.As(err, &conflict) {
			t.Fatalf("insert %q error = %v, want conflict", tt.second, err)
		}
		if conflict.With != tt.first {
			t.Fatalf("conflict with = %q, want %q", conflict.With, tt.first)
		}
		if conflict.Route != tt.second {
			t.Fatalf("conflict route = %q, want %q", conflict.Route, tt.second)
		}
	}
}

func TestMatchitManyParameters(t *testing.T) {
	const paramCount = 300
	route := makeParamRoute("p", paramCount)
	conflicting := makeParamRoute("q", paramCount)
	path := makePath("v", paramCount)

	var router Router[string]
	if err := router.TryInsert(route, "many"); err != nil {
		t.Fatalf("insert many-param route: %v", err)
	}

	got, params, ok := router.Match(path)
	if !ok {
		t.Fatalf("match many-param path: not found")
	}
	if got != "many" {
		t.Fatalf("value = %q, want many", got)
	}
	if params.Len() != paramCount {
		t.Fatalf("params length = %d, want %d", params.Len(), paramCount)
	}
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		want := Param{Key: fmt.Sprintf("p%d", i), Val: fmt.Sprintf("v%d", i)}
		if param != want {
			t.Fatalf("params[%d] = %#v, want %#v", i, param, want)
		}
	}

	var conflict *ConflictError
	if err := router.TryInsert(conflicting, "conflicting"); !errors.As(err, &conflict) {
		t.Fatalf("insert conflicting many-param route error = %v, want conflict", err)
	}
}

func TestMatchitHighParameterOrdinalDoesNotCollideWithLiteral(t *testing.T) {
	dynamic := makeParamRoute("p", 257)
	static := makeParamRoute("q", 256) + "/{{a}}"
	path := makePath("v", 256) + "/{a}"

	var router Router[string]
	if err := router.TryInsert(dynamic, "dynamic"); err != nil {
		t.Fatalf("insert dynamic route: %v", err)
	}
	if err := router.TryInsert(static, "static"); err != nil {
		t.Fatalf("insert static route: %v", err)
	}

	got, _, ok := router.Match(path)
	if !ok {
		t.Fatalf("match high-ordinal literal path: not found")
	}
	if got != "static" {
		t.Fatalf("value = %q, want static", got)
	}
}

func TestMatchIntoReusesParams(t *testing.T) {
	var router Router[string]
	router.Insert("/teams/{team}/members/{member}", "member")

	buf := NewParams(2)
	got, params, ok := router.MatchInto("/teams/core/members/ana", buf)
	if !ok {
		t.Fatal("MatchInto did not match")
	}
	if got != "member" {
		t.Fatalf("value = %q, want member", got)
	}
	if !paramsEqual(params, ParamsOf(Param{"team", "core"}, Param{"member", "ana"})) {
		t.Fatalf("params = %#v", params)
	}
	if buf.Len() != 0 {
		t.Fatalf("input buffer length = %d, want 0", buf.Len())
	}
}

func TestMatchIntoReusesHeapParams(t *testing.T) {
	var router Router[string]
	router.Insert("/{a}/{b}/{c}/{d}/{e}", "many")

	buf := NewParams(5)
	allocs := testing.AllocsPerRun(100, func() {
		_, params, ok := router.MatchInto("/a/b/c/d/e", buf)
		if !ok {
			t.Fatal("MatchInto did not match")
		}
		if params.Len() != 5 {
			t.Fatalf("params length = %d, want 5", params.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per MatchInto = %v, want 0", allocs)
	}
}

func TestMergeParamsInline(t *testing.T) {
	a := ParamsOf(Param{"team", "core"}, Param{"member", "ana"})
	b := ParamsOf(Param{"role", "lead"}, Param{"team", "infra"})

	got := Merge(a, b)
	want := ParamsOf(
		Param{"team", "core"},
		Param{"member", "ana"},
		Param{"role", "lead"},
		Param{"team", "infra"},
	)
	if !paramsEqual(got, want) {
		t.Fatalf("Merge params = %#v, want %#v", got.All(), want.All())
	}

	allocs := testing.AllocsPerRun(100, func() {
		got := Merge(a, b)
		if got.Len() != 4 {
			t.Fatalf("merged length = %d, want 4", got.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per inline Merge = %v, want 0", allocs)
	}
}

func TestMergeParamsInlineToHeap(t *testing.T) {
	a := ParamsOf(
		Param{"a", "1"},
		Param{"b", "2"},
		Param{"c", "3"},
	)
	b := ParamsOf(Param{"d", "4"}, Param{"e", "5"})

	got := Merge(a, b)
	want := ParamsOf(
		Param{"a", "1"},
		Param{"b", "2"},
		Param{"c", "3"},
		Param{"d", "4"},
		Param{"e", "5"},
	)
	if !paramsEqual(got, want) {
		t.Fatalf("Merge params = %#v, want %#v", got.All(), want.All())
	}
}

func TestMergeParamsReusesHeapCapacity(t *testing.T) {
	a := NewParams(8)
	a = a.append("a", "1")
	a = a.append("b", "2")
	a = a.append("c", "3")
	a = a.append("d", "4")
	a = a.append("e", "5")
	b := ParamsOf(Param{"f", "6"}, Param{"g", "7"})

	allocs := testing.AllocsPerRun(100, func() {
		got := Merge(a, b)
		if got.Len() != 7 {
			t.Fatalf("merged length = %d, want 7", got.Len())
		}
		if got.At(5) != (Param{"f", "6"}) || got.At(6) != (Param{"g", "7"}) {
			t.Fatalf("merged tail = %#v, %#v", got.At(5), got.At(6))
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per heap-capacity Merge = %v, want 0", allocs)
	}
}

func TestMergeParamsEmpty(t *testing.T) {
	params := ParamsOf(Param{"team", "core"})

	if got := Merge(Params{}, params); !paramsEqual(got, params) {
		t.Fatalf("Merge empty first = %#v, want %#v", got.All(), params.All())
	}
	if got := Merge(params, Params{}); !paramsEqual(got, params) {
		t.Fatalf("Merge empty second = %#v, want %#v", got.All(), params.All())
	}
}

func TestParamsSeq(t *testing.T) {
	params := ParamsOf(Param{"team", "core"}, Param{"member", "ana"})
	var got []Param
	for key, val := range params.Seq() {
		got = append(got, Param{Key: key, Val: val})
	}
	if len(got) != params.Len() {
		t.Fatalf("seq length = %d, want %d", len(got), params.Len())
	}
	for i, param := range got {
		if param != params.At(i) {
			t.Fatalf("seq param %d = %#v, want %#v", i, param, params.At(i))
		}
	}
}

func TestParamsSeqStopsEarly(t *testing.T) {
	params := ParamsOf(Param{"team", "core"}, Param{"member", "ana"})
	var got []Param
	for key, val := range params.Seq() {
		got = append(got, Param{Key: key, Val: val})
		break
	}
	if len(got) != 1 {
		t.Fatalf("seq length after break = %d, want 1", len(got))
	}
	if got[0] != params.At(0) {
		t.Fatalf("seq param = %#v, want %#v", got[0], params.At(0))
	}
}

func makeParamRoute(prefix string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("/{")
		b.WriteString(fmt.Sprintf("%s%d", prefix, i))
		b.WriteByte('}')
	}
	return b.String()
}

func makePath(prefix string, count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteByte('/')
		b.WriteString(fmt.Sprintf("%s%d", prefix, i))
	}
	return b.String()
}

type matchCase struct {
	path   string
	route  string
	params Params
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
