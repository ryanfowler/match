// Package match provides a minimal, high-performance generic path router.
//
// A Router maps slash-separated route patterns to caller-provided values, then
// returns the matched value and any captured parameters. It is intentionally
// narrower than an HTTP framework: it does not know about methods, middleware,
// redirects, request objects, URL decoding, or path cleaning. That makes it
// useful anywhere a path-like string needs to resolve to typed application
// data, such as HTTP handler lookup, command dispatch, API route tables, asset
// paths, virtual filesystems, or nested routers.
//
// The zero value of Router is ready to use. The stored value can be any Go type,
// and after routes are registered a Router may be shared by multiple goroutines
// for matching.
//
// # Quick Start
//
//	var router match.Router[string]
//	router.Insert("/posts/{year}/{slug}", "post")
//	router.Insert("/static/{*path}", "asset")
//
//	value, params, ok := router.Match("/posts/2026/route-grammar")
//	_ = value              // "post"
//	_ = params.Get("year") // "2026"
//	_ = params.Get("slug") // "route-grammar"
//	_ = ok                 // true
//
// # Path Semantics
//
// Routes and paths are plain strings with / as the segment separator. match
// does not normalize either side before matching: absolute and relative paths
// are distinct, empty segments are significant, trailing slashes are
// significant, escaped URL bytes are not decoded, and . or .. segments are not
// cleaned. If you are matching net/http requests, apply whatever URL or path
// normalization your application wants before calling Match.
//
// # Route Grammar
//
// Routes are slash-separated patterns made from literal text, named parameters,
// and catch-all parameters. A route does not have to start with /.
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
// # API Overview
//
// Insert registers trusted route definitions and panics on invalid, duplicate,
// or ambiguous routes. TryInsert registers routes from configuration, plugins,
// or other input that should produce a regular error.
//
// Match looks up an exact path. MatchInto is the same operation using a
// caller-provided Params value as reusable storage. MatchPrefix and
// MatchPrefixInto return the best whole-segment route prefix plus the remaining
// path, which is useful for mounts and nested dispatch. Rest is / when a prefix
// match consumes the full path.
//
// # Matching Behavior and Conflicts
//
// When more than one route could match, match chooses the most specific route:
// exact literal segments beat parameter segments, parameter segments with more
// literal text are tried first, and catch-all routes are considered last. Prefix
// matching uses the same route grammar, but chooses the route that consumes the
// most path.
//
// TryInsert returns an error for invalid, duplicate, or ambiguous routes.
// Invalid route syntax is reported with sentinel errors such as
// ErrInvalidParam, ErrInvalidParamSegment, and ErrInvalidCatchAll. Duplicate and
// ambiguous routes return *ConflictError. For example, /x/{id}/bar conflicts
// with /x/{name}/bar because both match the same set of paths. Insert panics on
// the same errors returned by TryInsert.
//
// # Params
//
// Matching returns parameters in route order. Params is an opaque value type;
// use Len and At to iterate without allocation, Get or TryGet to look up named
// parameters, Seq for range-over-function iteration, Merge to concatenate
// parameter sets, and AppendTo or All when a []Param snapshot is needed.
//
// # Internals
//
// Routes are parsed once during insertion. The parser turns a route string into
// tokens, splits those tokens into segment patterns, records capture names in
// route order, and builds a normalized route shape used to detect duplicates
// even when parameter names differ.
//
// The matcher is a segment trie. Each node can have static edges, parameter
// edges, catch-all edges, and an optional route value. Static edges are tried
// first. Parameter edges are sorted by specificity, with more literal text
// before less literal text, so /user-{id} is preferred over /{id} for
// "/user-42". Catch-all edges are checked after static and parameter edges.
// Nodes with many static children add a small lookup map while preserving
// compact storage for small route tables.
//
// TryInsert also maintains a conflict index. Dynamic routes are grouped by
// segment count and first definitely-static segment, with separate tracking for
// catch-all routes. This catches ambiguous definitions before they can make
// match results depend on insertion order.
//
// Parameters are collected after the winning route is selected, using the
// canonical route entry's capture names. Params stores up to four captures
// inline and grows to a slice only when needed. MatchInto and MatchPrefixInto
// reset and reuse a caller-provided Params value, which avoids heap allocation
// for common hot-path routing loops.
//
// Callers that insert routes while other goroutines use the router must
// synchronize access.
package match
