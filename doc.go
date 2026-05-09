// Package match provides a small generic path router.
//
// A Router maps route patterns to caller-provided values and matches request
// paths against those patterns. Routes are slash-separated paths made from
// literal text, named parameters, and catch-all parameters.
//
// # Route Grammar
//
// Literal text matches itself. A named parameter is written as {name} and
// captures one non-empty path segment. A parameter may have literal text before
// or after it in the same segment, such as /files/{name}.json or
// /user_{id}. Each path segment may contain at most one parameter.
//
// A catch-all parameter is written as {*name}. It captures the non-empty
// remainder of the path, including any slashes, and must appear at the end of
// the route. A catch-all may have a literal prefix in its final segment, such as
// /static/prefix-{*path}.
//
// Literal braces are escaped by doubling them: {{ matches a literal { and }}
// matches a literal }. Escaped braces may also appear inside parameter names.
//
// Parameter names must be non-empty. Names cannot contain /, and * is only
// valid as the first character of a catch-all parameter.
//
// # Matching and Conflicts
//
// Exact literal segments are preferred over parameter segments, and catch-all
// routes are considered after more specific segment matches.
//
// Insert rejects duplicate and ambiguous routes with ConflictError. For
// example, /x/{id}/bar conflicts with /x/{name}/bar because both match the same
// set of paths. Insert panics on invalid or conflicting routes; TryInsert
// returns the error.
//
// Matching returns parameters in route order. Params is an opaque value type;
// use Len and At to iterate without allocation, Get or TryGet to look up named
// parameters, Seq for range-over-function iteration, and AppendTo or All when a
// []Param snapshot is needed. Match allocates parameter storage as needed after
// a small inline buffer is exhausted, while MatchInto reuses the caller-provided
// Params value.
//
// # Examples
//
//	var router match.Router[string]
//	router.Insert("/posts/{year}/{slug}", "post")
//	router.Insert("/static/{*path}", "asset")
//
//	value, params, ok := router.Match("/posts/2026/route-grammar")
//	_ = value              // "post"
//	_ = params.Get("year") // "2026"
//	_ = ok                 // true
package match
