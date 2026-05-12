// Package match provides a small generic path router.
//
// A Router maps path patterns to caller-provided values and matches paths
// against those patterns. The zero value is ready to use, and the stored value
// can be any Go type.
//
// # Route Grammar
//
// Routes are slash-separated patterns made from literal text, named parameters,
// and catch-all parameters. A route does not have to start with /, but it is
// matched exactly as registered; match does not clean paths, decode escapes, or
// add a leading slash.
//
// Literal text matches itself. A named parameter is written as {name} and
// captures one non-empty path segment. A parameter may have literal text before
// or after it in the same segment, such as /files/{name}.json or /user_{id}.
// Each path segment may contain at most one parameter.
//
// A catch-all parameter is written as {*name}. It captures the non-empty
// remainder of the path, including any slashes, and must appear at the end of
// the route. A catch-all may have a literal prefix in its final segment, such as
// /static/prefix-{*path}; the captured value starts after that prefix.
//
// Literal braces are escaped by doubling them: {{ matches a literal { and }}
// matches a literal }. Escaped braces may also appear inside parameter names.
//
// Parameter names must be non-empty. Names cannot contain /, and * is only
// valid as the first character of a catch-all parameter. Parameters and
// catch-all parameters capture non-empty text.
//
// # Matching and Conflicts
//
// When more than one route could match, match chooses the most specific route:
// exact literal segments beat parameter segments, parameter segments with more
// literal text are tried first, and catch-all routes are considered last.
//
// TryInsert returns an error for invalid, duplicate, or ambiguous routes.
// Invalid route syntax is reported with sentinel errors such as
// ErrInvalidParam, ErrInvalidParamSegment, and ErrInvalidCatchAll. Duplicate and
// ambiguous routes return *ConflictError. For example, /x/{id}/bar conflicts
// with /x/{name}/bar because both match the same set of paths. Insert panics on
// the same errors returned by TryInsert.
//
// Matching returns parameters in route order. Params is an opaque value type;
// use Len and At to iterate without allocation, Get or TryGet to look up named
// parameters, Seq for range-over-function iteration, Merge to concatenate
// parameter sets, and AppendTo or All when a []Param snapshot is needed. Match
// stores up to four parameters inline and allocates only when more storage is
// needed, while MatchInto reuses the caller-provided Params value.
//
// After routes are registered, a Router may be used by multiple goroutines for
// matching. Callers that insert routes while other goroutines use the router
// must synchronize access.
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
