# match

[![Go Reference](https://pkg.go.dev/badge/github.com/ryanfowler/match.svg)](https://pkg.go.dev/github.com/ryanfowler/match)

`match` is a minimal, high-performance generic path router for Go. It maps
slash-separated route patterns to values you provide, then returns the matched
value and any captured parameters.

It is intentionally narrower than an HTTP framework. It does not know about
methods, middleware, redirects, request objects, URL decoding, or path cleaning.
That makes it useful anywhere a path-like string needs to resolve to typed
application data: HTTP handler lookup, command dispatch, API route tables, asset
paths, virtual filesystems, or nested routers.

Key properties:

- The zero value of `Router[T]` is ready to use.
- Routes can store any Go value: handlers, metadata, enum-like strings, or your
  own structs.
- Matching is deterministic. Literal routes beat dynamic routes, more-specific
  dynamic segments beat less-specific ones, and catch-all routes are considered
  last.
- Invalid, duplicate, and ambiguous route definitions are rejected at insertion
  time.
- Parameter storage is allocation-conscious: up to four captures are stored
  inline, and `MatchInto` / `MatchPrefixInto` let hot paths reuse storage.
- `Clone` can copy a route table before extending it independently.
- After registration, a router can be shared by multiple goroutines for
  matching.

## Install

```sh
go get github.com/ryanfowler/match
```

`match` requires Go 1.23 or newer.

## Quick Start

```go
package main

import (
	"fmt"

	"github.com/ryanfowler/match"
)

func main() {
	var router match.Router[string]

	router.Insert("/posts/{year}/{slug}", "post")
	router.Insert("/static/{*path}", "asset")

	value, params, ok := router.Match("/posts/2026/route-grammar")
	if !ok {
		return
	}

	fmt.Println(value)              // "post"
	fmt.Println(params.Get("year")) // "2026"
	fmt.Println(params.Get("slug")) // "route-grammar"
}
```

The router's zero value is ready to use. The value can be any Go type:

```go
type Handler struct {
	Name string
}

var router match.Router[Handler]
router.Insert("/health", Handler{Name: "healthcheck"})
```

## API Overview

Use `Insert` when route definitions are trusted and should fail fast:

```go
router.Insert("/users/{id}", "user")
```

Use `TryInsert` when routes come from configuration, plugins, or other input
that should produce a regular error:

```go
if err := router.TryInsert("/users/{id}", "user"); err != nil {
	return err
}
```

Use `Match` to look up a path. The path is matched exactly as provided; `match`
does not clean paths, decode escapes, or add a leading slash:

```go
value, params, ok := router.Match("/users/42")
```

Use `MatchInto` when matching in a hot path and you want to reuse parameter
storage:

```go
buf := match.NewParams(4)

value, ok := router.MatchInto("/users/42", &buf)
_, _, _ = value, buf, ok
```

Use `MatchPrefix` when a route should match the front of a path and return the
remaining path for nested dispatch or mounting:

```go
router.Insert("/api/{version}", "api")

got, ok := router.MatchPrefix("/api/v1/users/42")
// got.Value == "api"
// got.Params.Get("version") == "v1"
// got.Rest == "/users/42"
// ok == true
```

Use `MatchPrefixInto` to reuse parameter storage for prefix matches.

After routes are registered, a router may be used by multiple goroutines for
matching. If routes are inserted while other goroutines are using the router,
synchronize access around the router.

## Path Semantics

`match` treats routes and paths as plain strings with `/` as the segment
separator. It does not normalize either side before matching:

- Absolute and relative paths are distinct: `/users/{id}` does not match
  `users/42`.
- Empty segments are significant: `/a//b` is different from `/a/b`.
- Trailing slashes are significant: `/a/` is different from `/a`.
- Escaped URL bytes are not decoded, and `.` / `..` segments are not cleaned.

This keeps the package independent from any specific transport. If you are
matching `net/http` requests, apply whatever URL or path normalization your
application wants before calling `Match`.

## Route Grammar

Routes are slash-separated patterns made from literal text, named parameters,
and catch-all parameters. A route does not have to start with `/`, but most HTTP
path-style routes do.

| Syntax | Meaning |
| --- | --- |
| `/about` | Matches the literal path `/about`. |
| `/{page}` | Captures one non-empty path segment as `page`. |
| `/files/{name}.json` | Captures a parameter with a literal suffix in the same segment. |
| `/user_{id}` | Captures a parameter with a literal prefix in the same segment. |
| `/static/{*path}` | Captures the non-empty remainder of the path, including slashes. |
| `/{{/x/}}` | Matches literal braces: `{{` is `{` and `}}` is `}`. |

Rules to keep in mind:

- Parameter names must be non-empty.
- Parameter names cannot contain `/`.
- Each path segment may contain at most one parameter.
- `*` is only valid at the start of a catch-all parameter, as in `{*path}`.
- Catch-all parameters must be the final token in the route.
- Parameters and catch-all parameters capture non-empty text.

This is invalid because both parameters are in the same path segment:

```go
err := router.TryInsert("/{first}-{second}", value)
// errors.Is(err, match.ErrInvalidParamSegment) == true
```

Catch-all parameters can include a literal prefix in the final segment. The
captured value starts after that prefix:

```go
router.Insert("/static/prefix-{*path}", value)

_, params, ok := router.Match("/static/prefix-css/site.css")
// params.Get("path") == "css/site.css"
// ok == true
```

## Matching Behavior

When more than one route could match, `match` chooses the most specific route:
literal segments beat parameter segments, parameter segments with more literal
text are tried first, and catch-all routes are considered last.

```go
router.Insert("/posts/{year}/{slug}", "post")
router.Insert("/posts/{year}/index", "index")

value, _, _ := router.Match("/posts/2026/index")
// value == "index"
```

Prefix matching uses the same route grammar but chooses the route that consumes
the most path. A route registered as `/` matches the root prefix of any absolute
path, and `Rest` is `/` when the match consumes the full path:

```go
router.Insert("/api", "api")
router.Insert("/api/v1", "v1")

got, _ := router.MatchPrefix("/api/v1/users")
// got.Value == "v1"
// got.Rest == "/users"
```

Parameters are returned in the order they appear in the matched route:

```go
router.Insert("/teams/{team}/members/{member}", "member")

_, params, _ := router.Match("/teams/core/members/ana")

params.At(0) // match.Param{Key: "team", Val: "core"}
params.At(1) // match.Param{Key: "member", Val: "ana"}
```

## Working With Params

`Params` is an opaque value type. Use its methods instead of depending on its
internal representation:

```go
value, params, ok := router.Match("/posts/2026/route-grammar")
_, _ = value, ok

year := params.Get("year")

slug, found := params.TryGet("slug")
_, _ = year, slug
_, _ = found

for i := 0; i < params.Len(); i++ {
	param := params.At(i)
	_, _ = param.Key, param.Val
}

for key, val := range params.Seq() {
	_, _ = key, val
}

merged := match.Merge(params, match.ParamsOf(match.Param{Key: "source", Val: "cache"}))
_ = merged

snapshot := params.All()
_ = snapshot
```

`Match` stores up to four parameters inline and allocates only when more storage
is needed. `MatchInto` lets callers reuse a `Params` buffer across matches:

```go
params := match.NewParams(8)

for _, path := range paths {
	value, ok := router.MatchInto(path, &params)
	_, _, _ = value, params, ok
}
```

## Insert Errors and Conflicts

`TryInsert` returns an error when a route is invalid, duplicated, or ambiguous.
`Insert` panics on those same errors, which is convenient for hard-coded route
tables that should fail during startup.

Invalid route syntax is reported with sentinel errors:

```go
err := router.TryInsert("/src/{*filepath}x", value)
// errors.Is(err, match.ErrInvalidCatchAll) == true
```

Duplicate and ambiguous routes return `*match.ConflictError`. Parameter names do
not make otherwise identical routes distinct:

```go
var router match.Router[string]

router.Insert("/x/{id}/bar", "id")
err := router.TryInsert("/x/{name}/bar", "name")

var conflict *match.ConflictError
if errors.As(err, &conflict) {
	fmt.Println(conflict.Route) // "/x/{name}/bar"
	fmt.Println(conflict.With)  // "/x/{id}/bar"
}
```

Dynamic routes also conflict when the same path could select either route, such
as `/user_{name}` and `/user_{id}`.

## DNS Hostname Matching

The module also includes `github.com/ryanfowler/match/dns`, a sub-package for
DNS-style hostname matching with the same generic value storage and reusable
parameter capture model:

```go
package main

import (
	"fmt"

	"github.com/ryanfowler/match/dns"
)

func main() {
	var router dns.Router[string]

	router.Insert("example.com", "apex")
	router.Insert("{tenant}.example.com", "tenant")

	value, params, ok := router.Match("api.example.com.")
	if !ok {
		return
	}

	fmt.Println(value)                // "tenant"
	fmt.Println(params.Get("tenant")) // "api"
}
```

DNS patterns are dot-separated labels matched right-to-left. Literal labels are
ASCII case-insensitive, and a single trailing root dot is ignored, so
`Example.COM` and `example.com.` are equivalent. The package does not parse
`host:port` strings, perform IDNA conversion, or normalize Unicode.

The DNS grammar mirrors the path router where it fits DNS labels:

| Syntax | Meaning |
| --- | --- |
| `example.com` | Matches the literal hostname. |
| `{tenant}.example.com` | Captures one non-empty label as `tenant`. |
| `api-{region}.example.com` | Captures part of one label. |
| `{*subdomain}.example.com` | Captures one or more leading labels, such as `a.b`. |
| `{{literal}}.example.com` | Matches literal braces. |

Use `MatchSuffix` for zone-style dispatch:

```go
router.Insert("example.com", "zone")

got, ok := router.MatchSuffix("api.us.example.com")
// got.Value == "zone"
// got.Prefix == "api.us"
// ok == true
```

## Internals

For a deeper implementation-level architecture overview, see
[DESIGN.md](DESIGN.md).

Routes are parsed once during insertion. The parser turns a route string into
tokens, splits those tokens into segment patterns, records capture names in
route order, and builds a normalized route shape used to detect duplicates even
when parameter names differ.

The matcher is a segment trie. Each node can have static edges, parameter edges,
catch-all edges, and an optional route value. Static edges are tried first.
Parameter edges are sorted by specificity, with more literal text before less
literal text, so `/user-{id}` is preferred over `/{id}` for `"/user-42"`.
Catch-all edges are checked after static and parameter edges. Nodes with many
static children add a small lookup map while still preserving compact storage
for small route tables.

`TryInsert` also maintains a conflict index. Dynamic routes are grouped by
segment count and first definitely-static segment, with separate tracking for
catch-all routes. This catches ambiguous definitions before they can make match
results depend on insertion order. For example, `/x/{id}/bar` conflicts with
`/x/{name}/bar`, while `/files/{name}.json/a` and `/files/report.{ext}/b` can
coexist because later segments disambiguate them.

Parameters are collected after the winning route is selected, using the
canonical route entry's capture names. `Params` stores up to four captures
inline and grows to a slice only when needed. `MatchInto` and
`MatchPrefixInto` reset and reuse a caller-provided `*Params`, which avoids
heap allocation for common hot-path routing loops.

Prefix matching uses the same trie and route grammar as exact matching. It
tracks the best whole-segment prefix while walking the tree, chooses the route
that consumes the most path, and returns the remaining path as `Rest`. When a
prefix consumes the full path, `Rest` is `/`.

## Development

Run the test suite with:

```sh
go test ./...
```

Run benchmarks with:

```sh
go test -bench=. -benchmem ./...
```

## License

This project is licensed under the Apache License, Version 2.0. See
[LICENSE](LICENSE).
