package match

import (
	"errors"
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
				{"/about", "/{page}", Params{{"page", "about"}}},
				{"/posts/2021/01/rust", "/posts/{year}/{month}/{post}", Params{{"year", "2021"}, {"month", "01"}, {"post", "rust"}}},
				{"/posts/2021/01/index", "/posts/{year}/{month}/index", Params{{"year", "2021"}, {"month", "01"}}},
				{"/posts/2021/top", "/posts/{year}/top", Params{{"year", "2021"}}},
				{"/static/foo.png", "/static/{*path}", Params{{"path", "foo.png"}}},
				{"/favicon.ico", "/favicon.ico", nil},
			},
		},
		{
			name:   "wildcard suffix",
			routes: []string{"/", "/{foo}x", "/foox", "/{foo}x/bar", "/{foo}x/bar/baz"},
			matches: []matchCase{
				{"/", "/", nil},
				{"/foox", "/foox", nil},
				{"/barx", "/{foo}x", Params{{"foo", "bar"}}},
				{"/mx", "/{foo}x", Params{{"foo", "m"}}},
				{"/mx/bar", "/{foo}x/bar", Params{{"foo", "m"}}},
				{"/xfoox/bar/baz", "/{foo}x/bar/baz", Params{{"foo", "xfoo"}}},
			},
		},
		{
			name:   "catchall overlap",
			routes: []string{"/path/foo", "/path/{*rest}"},
			matches: []matchCase{
				{"/path/foo", "/path/foo", nil},
				{"/path/bar", "/path/{*rest}", Params{{"rest", "bar"}}},
				{"/path/foo/", "/path/{*rest}", Params{{"rest", "foo/"}}},
			},
		},
		{
			name:   "escaped",
			routes: []string{"/", "/{{", "/}}", "/{ba{{r}", "/baz/{xxx}/}}xy{{{{", "/{{/{x}"},
			matches: []matchCase{
				{"/", "/", nil},
				{"/{", "/{{", nil},
				{"/}", "/}}", nil},
				{"/foo", "/{ba{{r}", Params{{"ba{r", "foo"}}},
				{"/baz/x/}xy{{", "/baz/{xxx}/}}xy{{{{", Params{{"xxx", "x"}}},
				{"/{/{{", "/{{/{x}", Params{{"x", "{{"}}},
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
	}
}

func TestAt(t *testing.T) {
	var router Router[int]
	router.Insert("/users/{id}", 7)

	got, err := router.At("/users/42")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 7 || got.Params.Get("id") != "42" {
		t.Fatalf("At = %#v", got)
	}

	if _, err := router.At("/users/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("At miss error = %v, want %v", err, ErrNotFound)
	}
}

func TestMatchIntoReusesParams(t *testing.T) {
	var router Router[string]
	router.Insert("/teams/{team}/members/{member}", "member")

	buf := make(Params, 0, 2)
	got, params, ok := router.MatchInto("/teams/core/members/ana", buf)
	if !ok {
		t.Fatal("MatchInto did not match")
	}
	if got != "member" {
		t.Fatalf("value = %q, want member", got)
	}
	if !paramsEqual(params, Params{{"team", "core"}, {"member", "ana"}}) {
		t.Fatalf("params = %#v", params)
	}
	if len(buf) != 0 {
		t.Fatalf("input buffer length = %d, want 0", len(buf))
	}
	if cap(params) != cap(buf) {
		t.Fatalf("params cap = %d, want reused cap %d", cap(params), cap(buf))
	}
}

type matchCase struct {
	path   string
	route  string
	params Params
}

func paramsEqual(a, b Params) bool {
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
