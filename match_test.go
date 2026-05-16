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

func TestMatchitDivergentParamRoutesKeepParamNames(t *testing.T) {
	var router Router[string]
	if err := router.TryInsert("/{id}/foo", "foo"); err != nil {
		t.Fatalf("insert id route: %v", err)
	}
	if err := router.TryInsert("/{name}/bar", "bar"); err != nil {
		t.Fatalf("insert name route: %v", err)
	}

	got, params, ok := router.Match("/abc/bar")
	if !ok {
		t.Fatal("match /abc/bar: not found")
	}
	if got != "bar" {
		t.Fatalf("value = %q, want bar", got)
	}
	if params.Get("id") != "" {
		t.Fatalf("Param(id) = %q, want empty", params.Get("id"))
	}
	if params.Get("name") != "abc" {
		t.Fatalf("Param(name) = %q, want abc", params.Get("name"))
	}
}

func TestTrieReferencesCanonicalRouteEntriesAfterRouteGrowth(t *testing.T) {
	var router Router[int]
	router.Insert("/first", 1)
	router.Insert("/files/{*path}", 2)

	for i := 0; i < 128; i++ {
		router.Insert(fmt.Sprintf("/route-%03d", i), i+3)
	}

	staticEntry, ok := router.root.root.matchPath("/first", 0)
	if !ok {
		t.Fatal("match static route: not found")
	}
	if staticEntry != router.root.routes[0] {
		t.Fatal("static trie entry does not reference canonical route entry")
	}

	catchAllEntry, ok := router.root.root.matchPath("/files/app.css", 0)
	if !ok {
		t.Fatal("match catch-all route: not found")
	}
	if catchAllEntry != router.root.routes[1] {
		t.Fatal("catch-all trie entry does not reference canonical route entry")
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

func TestMatchExactRouteShapes(t *testing.T) {
	tests := []struct {
		name  string
		route string
		path  string
	}{
		{"empty route", "", ""},
		{"relative route", "relative/{id}", "relative/42"},
		{"empty middle segment", "/a//b", "/a//b"},
		{"trailing slash", "/a/", "/a/"},
		{"long segment", "/aaaaaaaaaaaaaaaaa/{id}", "/aaaaaaaaaaaaaaaaa/42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var router Router[string]
			router.Insert(tt.route, tt.route)

			got, _, ok := router.Match(tt.path)
			if !ok {
				t.Fatalf("match %q: not found", tt.path)
			}
			if got != tt.route {
				t.Fatalf("match %q route = %q, want %q", tt.path, got, tt.route)
			}
		})
	}
}

func TestMatchExactRouteShapeMisses(t *testing.T) {
	var router Router[string]
	router.Insert("", "empty")
	router.Insert("relative", "relative")
	router.Insert("/a//b", "empty-segment")
	router.Insert("/a/", "trailing")

	for _, path := range []string{"/", "/relative", "relative/", "/a/b", "/a"} {
		if got, _, ok := router.Match(path); ok {
			t.Fatalf("match %q = %q, want miss", path, got)
		}
	}
}

func TestMatchStaticChildrenAfterIndexThreshold(t *testing.T) {
	var router Router[string]
	for i := 0; i < 12; i++ {
		route := fmt.Sprintf("/static-%02d", i)
		router.Insert(route, route)
	}

	got, params, ok := router.Match("/static-11")
	if !ok {
		t.Fatal("match indexed static child: not found")
	}
	if got != "/static-11" {
		t.Fatalf("route = %q, want /static-11", got)
	}
	if params.Len() != 0 {
		t.Fatalf("params length = %d, want 0", params.Len())
	}
}

func TestMatchPrefixedCatchAll(t *testing.T) {
	var router Router[string]
	router.Insert("/static/prefix-{*path}", "catch-all")
	router.Insert("/static/prefix-file", "static")

	tests := []matchCase{
		{"/static/prefix-file", "static", Params{}},
		{"/static/prefix-assets/app.css", "catch-all", ParamsOf(Param{"path", "assets/app.css"})},
	}

	for _, tt := range tests {
		got, params, ok := router.Match(tt.path)
		if !ok {
			t.Fatalf("match %q: not found", tt.path)
		}
		if got != tt.route {
			t.Fatalf("match %q route = %q, want %q", tt.path, got, tt.route)
		}
		if !paramsEqual(params, tt.params) {
			t.Fatalf("match %q params = %#v, want %#v", tt.path, params.All(), tt.params.All())
		}
	}

	for _, path := range []string{"/static/prefix-", "/static/other"} {
		if got, _, ok := router.Match(path); ok {
			t.Fatalf("match %q = %q, want miss", path, got)
		}
	}
}

func TestMatchChoosesMoreSpecificParamRoute(t *testing.T) {
	for _, routes := range [][]string{
		{"/{id}/view", "/user-{id}/view"},
		{"/user-{id}/view", "/{id}/view"},
	} {
		var router Router[string]
		for _, route := range routes {
			router.Insert(route, route)
		}

		got, params, ok := router.Match("/user-42/view")
		if !ok {
			t.Fatal("match specific param route: not found")
		}
		if got != "/user-{id}/view" {
			t.Fatalf("route = %q, want /user-{id}/view", got)
		}
		if !paramsEqual(params, ParamsOf(Param{"id", "42"})) {
			t.Fatalf("params = %#v, want id=42", params.All())
		}
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
		{"catchall before slash", "/src/{*filepath}/extra", ErrInvalidCatchAll},
		{"unmatched open", "x{y", ErrInvalidParam},
		{"unmatched close", "x}", ErrInvalidParam},
		{"slash in param", "/{foo/bar}", ErrInvalidParam},
		{"star in param name", "/{foo*bar}", ErrInvalidParam},
		{"unescaped open brace in param", "/{a{b}", ErrInvalidParam},
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

func TestInsertPanicsOnInvalidRoute(t *testing.T) {
	defer func() {
		r := recover()
		err, ok := r.(error)
		if !ok || !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("panic = %v, want ErrInvalidParam", r)
		}
	}()

	var router Router[string]
	router.Insert("/{}", "invalid")
}

func TestMatchitConflicts(t *testing.T) {
	tests := []struct {
		first  string
		second string
	}{
		{"/", "/"},
		{"/x/{foo}/bar", "/x/{bar}/bar"},
		{"/src/{*filepath}", "/src/{file}"},
		{"/static/prefix-{*path}", "/static/{file}"},
		{"/user_{name}", "/user_{bar}"},
		{"/files/{name}.json", "/files/report.{ext}"},
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

func TestMatchitAllowsDisjointAmbiguousSegments(t *testing.T) {
	tests := []struct {
		name    string
		first   string
		second  string
		matches []matchCase
	}{
		{
			name:   "later static segment disambiguates affixed params",
			first:  "/files/{name}.json/a",
			second: "/files/report.{ext}/b",
			matches: []matchCase{
				{"/files/report.json/a", "/files/{name}.json/a", ParamsOf(Param{"name", "report"})},
				{"/files/report.json/b", "/files/report.{ext}/b", ParamsOf(Param{"ext", "json"})},
			},
		},
		{
			name:   "incompatible catch all prefixes",
			first:  "/static/css-{*path}",
			second: "/static/js-{*path}",
			matches: []matchCase{
				{"/static/css-app.css", "/static/css-{*path}", ParamsOf(Param{"path", "app.css"})},
				{"/static/js-app.js", "/static/js-{*path}", ParamsOf(Param{"path", "app.js"})},
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
				t.Fatalf("insert %q error = %v, want nil", tt.second, err)
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
					t.Fatalf("match %q params = %#v, want %#v", m.path, params.All(), m.params.All())
				}
			}
		})
	}
}

func TestCatchAllPrefixConflictsWithOverlappingDynamicRoutes(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
	}{
		{
			name:   "catch all before dynamic",
			first:  "/src/foo/{*path}",
			second: "/src/{id}/bar",
		},
		{
			name:   "dynamic before catch all",
			first:  "/src/{id}/bar",
			second: "/src/foo/{*path}",
		},
		{
			name:   "catch all segment prefix before dynamic",
			first:  "/src/foo{*path}",
			second: "/src/{id}/bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		})
	}
}

func TestCatchAllPrefixAllowsStaticAndEmptyRemainderRoutes(t *testing.T) {
	t.Run("static route under catch all", func(t *testing.T) {
		var router Router[string]
		if err := router.TryInsert("/src/foo/{*path}", "catch"); err != nil {
			t.Fatalf("insert catch all: %v", err)
		}
		if err := router.TryInsert("/src/foo/bar", "static"); err != nil {
			t.Fatalf("insert static route error = %v, want nil", err)
		}

		got, _, ok := router.Match("/src/foo/bar")
		if !ok {
			t.Fatal("match static route: not found")
		}
		if got != "static" {
			t.Fatalf("value = %q, want static", got)
		}
	})

	t.Run("dynamic route with empty catch all remainder", func(t *testing.T) {
		var router Router[string]
		if err := router.TryInsert("/src/foo/{*path}", "catch"); err != nil {
			t.Fatalf("insert catch all: %v", err)
		}
		if err := router.TryInsert("/src/{id}/", "dynamic"); err != nil {
			t.Fatalf("insert dynamic route error = %v, want nil", err)
		}
	})
}

func TestConflictErrorString(t *testing.T) {
	err := &ConflictError{Route: "/new", With: "/existing"}
	want := "insertion failed due to conflict with previously registered route: /existing"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestNonConflictingDynamicAffixes(t *testing.T) {
	var router Router[string]
	router.Insert("/files/{name}.json", "json")
	router.Insert("/files/{name}.xml", "xml")

	tests := []matchCase{
		{"/files/report.json", "json", ParamsOf(Param{"name", "report"})},
		{"/files/report.xml", "xml", ParamsOf(Param{"name", "report"})},
	}

	for _, tt := range tests {
		got, params, ok := router.Match(tt.path)
		if !ok {
			t.Fatalf("match %q: not found", tt.path)
		}
		if got != tt.route {
			t.Fatalf("match %q route = %q, want %q", tt.path, got, tt.route)
		}
		if !paramsEqual(params, tt.params) {
			t.Fatalf("match %q params = %#v, want %#v", tt.path, params.All(), tt.params.All())
		}
	}
}

func TestConflictWithEscapedParamNames(t *testing.T) {
	var router Router[string]
	if err := router.TryInsert("/{ba{{r}", "first"); err != nil {
		t.Fatalf("insert escaped param route: %v", err)
	}

	var conflict *ConflictError
	if err := router.TryInsert("/{other}", "second"); !errors.As(err, &conflict) {
		t.Fatalf("insert conflicting param route error = %v, want conflict", err)
	}
	if conflict.With != "/{ba{r}" {
		t.Fatalf("conflict with = %q, want /{ba{r}", conflict.With)
	}
	if conflict.Route != "/{other}" {
		t.Fatalf("conflict route = %q, want /{other}", conflict.Route)
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

func TestMatchPreallocatesHeapParams(t *testing.T) {
	const paramCount = 9
	route := makeParamRoute("p", paramCount)
	path := makePath("v", paramCount)

	var router Router[string]
	if err := router.TryInsert(route, "many"); err != nil {
		t.Fatalf("insert many-param route: %v", err)
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, params, ok := router.Match(path)
		if !ok {
			t.Fatal("Match did not match")
		}
		if params.Len() != paramCount {
			t.Fatalf("params length = %d, want %d", params.Len(), paramCount)
		}
		if params.heap == nil || cap(params.heap) < paramCount {
			t.Fatalf("params heap capacity = %d, want at least %d", cap(params.heap), paramCount)
		}
	})
	if allocs != 1 {
		t.Fatalf("allocs per Match = %v, want 1", allocs)
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

func TestMatchMissReturnsZeroValueAndEmptyParams(t *testing.T) {
	var router Router[int]
	router.Insert("/found/{id}", 42)

	got, params, ok := router.Match("/missing")
	if ok {
		t.Fatal("Match matched unexpected path")
	}
	if got != 0 {
		t.Fatalf("value = %d, want zero", got)
	}
	if params.Len() != 0 {
		t.Fatalf("params length = %d, want 0", params.Len())
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

func TestMatchIntoMissResetsInlineParams(t *testing.T) {
	var router Router[string]
	router.Insert("/{team}/{member}", "member")

	buf := ParamsOf(Param{"stale", "value"})
	_, params, ok := router.MatchInto("/missing", buf)
	if ok {
		t.Fatal("MatchInto matched unexpected path")
	}
	if params.Len() != 0 {
		t.Fatalf("miss params length = %d, want 0", params.Len())
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

func TestMatchIntoMissPreservesHeapParams(t *testing.T) {
	var router Router[string]
	router.Insert("/{a}/{b}/{c}/{d}/{e}", "many")

	buf := NewParams(5)
	_, params, ok := router.MatchInto("/miss", buf)
	if ok {
		t.Fatal("MatchInto matched unexpected path")
	}
	if params.Len() != 0 {
		t.Fatalf("miss params length = %d, want 0", params.Len())
	}
	if params.heap == nil || cap(params.heap) < 5 {
		t.Fatalf("miss params heap capacity = %d, want at least 5", cap(params.heap))
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, matchedParams, ok := router.MatchInto("/a/b/c/d/e", params)
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

func TestMatchIntoHeapParamsSurviveBacktracking(t *testing.T) {
	t.Run("failed later branch", func(t *testing.T) {
		var router Router[string]
		router.Insert("/{id}", "id")
		router.Insert("/{name}/bar", "bar")

		got, params, ok := router.MatchInto("/abc", NewParams(5))
		if !ok {
			t.Fatal("MatchInto did not match")
		}
		if got != "id" {
			t.Fatalf("value = %q, want id", got)
		}
		if !paramsEqual(params, ParamsOf(Param{"id", "abc"})) {
			t.Fatalf("params = %#v, want id=abc", params.All())
		}
	})

	t.Run("less specific later branch", func(t *testing.T) {
		var router Router[string]
		router.Insert("/user-{id}/view", "specific")
		router.Insert("/{id}/view", "generic")

		got, params, ok := router.MatchInto("/user-42/view", NewParams(5))
		if !ok {
			t.Fatal("MatchInto did not match")
		}
		if got != "specific" {
			t.Fatalf("value = %q, want specific", got)
		}
		if !paramsEqual(params, ParamsOf(Param{"id", "42"})) {
			t.Fatalf("params = %#v, want id=42", params.All())
		}
	})
}

func TestMatchPrefixMatchesRootPrefix(t *testing.T) {
	var router Router[string]
	router.Insert("/", "root")
	router.Insert("/api", "api")

	tests := []struct {
		path string
		want string
		rest string
	}{
		{"/", "root", "/"},
		{"/users/42", "root", "/users/42"},
		{"//users/42", "root", "/users/42"},
		{"/api", "api", "/"},
		{"/api/", "api", "/"},
		{"/api/users", "api", "/users"},
		{"/api//users", "api", "/users"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := router.MatchPrefix(tt.path)
			if !ok {
				t.Fatalf("MatchPrefix(%q): not found", tt.path)
			}
			if got.Value != tt.want || got.Rest != tt.rest {
				t.Fatalf("MatchPrefix(%q) = value %q rest %q, want value %q rest %q", tt.path, got.Value, got.Rest, tt.want, tt.rest)
			}
			if got.Params.Len() != 0 {
				t.Fatalf("params length = %d, want 0", got.Params.Len())
			}
		})
	}
}

func TestMatchPrefixDoesNotMatchPartialSegment(t *testing.T) {
	var router Router[string]
	router.Insert("/api", "api")

	if got, ok := router.MatchPrefix("/apix/users"); ok {
		t.Fatalf("MatchPrefix matched value %q rest %q, want miss", got.Value, got.Rest)
	}
}

func TestMatchPrefixChoosesLongestPrefix(t *testing.T) {
	for _, routes := range [][]string{
		{"/api", "/api/v1"},
		{"/api/v1", "/api"},
	} {
		var router Router[string]
		for _, route := range routes {
			router.Insert(route, route)
		}

		got, ok := router.MatchPrefix("/api/v1/users/42")
		if !ok {
			t.Fatal("MatchPrefix did not match")
		}
		if got.Value != "/api/v1" || got.Rest != "/users/42" {
			t.Fatalf("MatchPrefix = value %q rest %q, want /api/v1 /users/42", got.Value, got.Rest)
		}
	}
}

func TestMatchPrefixMergesParamsAndRest(t *testing.T) {
	var router Router[string]
	router.Insert("/api/{version}", "api")

	got, ok := router.MatchPrefix("/api/v1/users/42")
	if !ok {
		t.Fatal("MatchPrefix did not match")
	}
	if got.Value != "api" || got.Rest != "/users/42" {
		t.Fatalf("MatchPrefix = value %q rest %q, want api /users/42", got.Value, got.Rest)
	}
	if !paramsEqual(got.Params, ParamsOf(Param{"version", "v1"})) {
		t.Fatalf("params = %#v, want version=v1", got.Params.All())
	}
}

func TestMatchPrefixChoosesMoreSpecificParamPrefix(t *testing.T) {
	for _, routes := range [][]string{
		{"/{id}/view", "/user-{id}/view"},
		{"/user-{id}/view", "/{id}/view"},
	} {
		var router Router[string]
		for _, route := range routes {
			router.Insert(route, route)
		}

		got, ok := router.MatchPrefix("/user-42/view/details")
		if !ok {
			t.Fatal("MatchPrefix did not match")
		}
		if got.Value != "/user-{id}/view" || got.Rest != "/details" {
			t.Fatalf("MatchPrefix = value %q rest %q, want /user-{id}/view /details", got.Value, got.Rest)
		}
		if !paramsEqual(got.Params, ParamsOf(Param{"id", "42"})) {
			t.Fatalf("params = %#v, want id=42", got.Params.All())
		}
	}
}

func TestMatchPrefixBacktracksAcrossParamBranches(t *testing.T) {
	var router Router[string]
	router.Insert("/{id}/foo", "foo")
	router.Insert("/{name}/bar", "bar")

	got, ok := router.MatchPrefix("/abc/bar/users")
	if !ok {
		t.Fatal("MatchPrefix did not match")
	}
	if got.Value != "bar" || got.Rest != "/users" {
		t.Fatalf("MatchPrefix = value %q rest %q, want bar /users", got.Value, got.Rest)
	}
	if got.Params.Get("id") != "" {
		t.Fatalf("id param = %q, want empty", got.Params.Get("id"))
	}
	if got.Params.Get("name") != "abc" {
		t.Fatalf("name param = %q, want abc", got.Params.Get("name"))
	}
}

func TestMatchPrefixCatchAllConsumesRemainder(t *testing.T) {
	var router Router[string]
	router.Insert("/files", "files")
	router.Insert("/files/{*path}", "catch-all")

	got, ok := router.MatchPrefix("/files/css/app.css")
	if !ok {
		t.Fatal("MatchPrefix did not match")
	}
	if got.Value != "catch-all" || got.Rest != "/" {
		t.Fatalf("MatchPrefix = value %q rest %q, want catch-all /", got.Value, got.Rest)
	}
	if !paramsEqual(got.Params, ParamsOf(Param{"path", "css/app.css"})) {
		t.Fatalf("params = %#v, want path=css/app.css", got.Params.All())
	}

	got, ok = router.MatchPrefix("/files")
	if !ok {
		t.Fatal("MatchPrefix /files did not match")
	}
	if got.Value != "files" || got.Rest != "/" {
		t.Fatalf("MatchPrefix /files = value %q rest %q, want files /", got.Value, got.Rest)
	}
}

func TestMatchPrefixIntoReusesParams(t *testing.T) {
	var router Router[string]
	router.Insert("/{a}/{b}/{c}/{d}/{e}", "many")

	buf := NewParams(5)
	allocs := testing.AllocsPerRun(100, func() {
		got, ok := router.MatchPrefixInto("/a/b/c/d/e/rest", buf)
		if !ok {
			t.Fatal("MatchPrefixInto did not match")
		}
		if got.Value != "many" || got.Rest != "/rest" {
			t.Fatalf("MatchPrefixInto = value %q rest %q, want many /rest", got.Value, got.Rest)
		}
		if got.Params.Len() != 5 {
			t.Fatalf("params length = %d, want 5", got.Params.Len())
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per MatchPrefixInto = %v, want 0", allocs)
	}
}

func TestMatchPrefixIntoMissPreservesHeapParams(t *testing.T) {
	var router Router[string]
	router.Insert("/{a}/{b}/{c}/{d}/{e}", "many")

	buf := NewParams(5)
	got, ok := router.MatchPrefixInto("/miss", buf)
	if ok {
		t.Fatalf("MatchPrefixInto matched value %q, want miss", got.Value)
	}
	if got.Params.Len() != 0 {
		t.Fatalf("miss params length = %d, want 0", got.Params.Len())
	}
	if got.Params.heap == nil || cap(got.Params.heap) < 5 {
		t.Fatalf("miss params heap capacity = %d, want at least 5", cap(got.Params.heap))
	}
}

func TestMatchPrefixIntoHeapParamsSurviveBacktracking(t *testing.T) {
	t.Run("failed later branch", func(t *testing.T) {
		var router Router[string]
		router.Insert("/{id}", "id")
		router.Insert("/{name}/bar", "bar")

		got, ok := router.MatchPrefixInto("/abc/baz", NewParams(5))
		if !ok {
			t.Fatal("MatchPrefixInto did not match")
		}
		if got.Value != "id" || got.Rest != "/baz" {
			t.Fatalf("MatchPrefixInto = value %q rest %q, want id /baz", got.Value, got.Rest)
		}
		if !paramsEqual(got.Params, ParamsOf(Param{"id", "abc"})) {
			t.Fatalf("params = %#v, want id=abc", got.Params.All())
		}
	})

	t.Run("less specific later branch", func(t *testing.T) {
		var router Router[string]
		router.Insert("/user-{id}/view", "specific")
		router.Insert("/{id}/view", "generic")

		got, ok := router.MatchPrefixInto("/user-42/view/details", NewParams(5))
		if !ok {
			t.Fatal("MatchPrefixInto did not match")
		}
		if got.Value != "specific" || got.Rest != "/details" {
			t.Fatalf("MatchPrefixInto = value %q rest %q, want specific /details", got.Value, got.Rest)
		}
		if !paramsEqual(got.Params, ParamsOf(Param{"id", "42"})) {
			t.Fatalf("params = %#v, want id=42", got.Params.All())
		}
	})
}

func TestParamsAccessors(t *testing.T) {
	params := ParamsOf(Param{"empty", ""}, Param{"team", "core"}, Param{"team", "infra"})

	if got := params.Get("missing"); got != "" {
		t.Fatalf("Get missing = %q, want empty", got)
	}
	if got := params.Get("empty"); got != "" {
		t.Fatalf("Get empty = %q, want empty", got)
	}
	if got, ok := params.TryGet("empty"); !ok || got != "" {
		t.Fatalf("TryGet empty = %q, %v, want empty true", got, ok)
	}
	if got, ok := params.TryGet("team"); !ok || got != "core" {
		t.Fatalf("TryGet duplicate = %q, %v, want core true", got, ok)
	}
	if got, ok := params.TryGet("missing"); ok || got != "" {
		t.Fatalf("TryGet missing = %q, %v, want empty false", got, ok)
	}
}

func TestParamsAtPanicsOutOfRange(t *testing.T) {
	params := ParamsOf(Param{"team", "core"})
	for _, index := range []int{-1, 1} {
		t.Run(fmt.Sprintf("index %d", index), func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("At did not panic")
				}
			}()
			_ = params.At(index)
		})
	}
}

func TestParamsAppendToPreservesPrefix(t *testing.T) {
	params := ParamsOf(Param{"team", "core"}, Param{"member", "ana"})
	dst := []Param{{Key: "prefix", Val: "keep"}}

	got := params.AppendTo(dst)
	want := []Param{
		{Key: "prefix", Val: "keep"},
		{Key: "team", Val: "core"},
		{Key: "member", Val: "ana"},
	}
	if !paramSlicesEqual(got, want) {
		t.Fatalf("AppendTo = %#v, want %#v", got, want)
	}
}

func TestParamsAppendToHeapAndAllSnapshot(t *testing.T) {
	params := ParamsOf(
		Param{"a", "1"},
		Param{"b", "2"},
		Param{"c", "3"},
		Param{"d", "4"},
		Param{"e", "5"},
	)

	got := params.AppendTo([]Param{{Key: "prefix", Val: "keep"}})
	want := []Param{
		{Key: "prefix", Val: "keep"},
		{Key: "a", Val: "1"},
		{Key: "b", Val: "2"},
		{Key: "c", Val: "3"},
		{Key: "d", Val: "4"},
		{Key: "e", Val: "5"},
	}
	if !paramSlicesEqual(got, want) {
		t.Fatalf("AppendTo heap = %#v, want %#v", got, want)
	}

	all := params.All()
	all[0] = Param{Key: "changed", Val: "changed"}
	if params.At(0) != (Param{Key: "a", Val: "1"}) {
		t.Fatalf("All did not return a snapshot; params[0] = %#v", params.At(0))
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

func TestMergeParamsGrowsHeap(t *testing.T) {
	a := NewParams(5)
	for _, param := range []Param{
		{"a", "1"},
		{"b", "2"},
		{"c", "3"},
		{"d", "4"},
		{"e", "5"},
	} {
		a = a.append(param.Key, param.Val)
	}
	b := ParamsOf(Param{"f", "6"})

	got := Merge(a, b)
	want := ParamsOf(
		Param{"a", "1"},
		Param{"b", "2"},
		Param{"c", "3"},
		Param{"d", "4"},
		Param{"e", "5"},
		Param{"f", "6"},
	)
	if !paramsEqual(got, want) {
		t.Fatalf("Merge grown heap = %#v, want %#v", got.All(), want.All())
	}
}

func TestMergeParamsCopiesHeapSecond(t *testing.T) {
	a := ParamsOf(Param{"a", "1"})
	b := ParamsOf(
		Param{"b", "2"},
		Param{"c", "3"},
		Param{"d", "4"},
		Param{"e", "5"},
		Param{"f", "6"},
	)

	got := Merge(a, b)
	want := ParamsOf(
		Param{"a", "1"},
		Param{"b", "2"},
		Param{"c", "3"},
		Param{"d", "4"},
		Param{"e", "5"},
		Param{"f", "6"},
	)
	if !paramsEqual(got, want) {
		t.Fatalf("Merge heap second = %#v, want %#v", got.All(), want.All())
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

func paramSlicesEqual(a, b []Param) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
