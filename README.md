# match

`match` is a small generic path router for Go. It maps route patterns to values
you provide, then returns the matched value plus any captured path parameters.

It is useful when you want routing behavior without pulling in a full HTTP
framework: command dispatch, API route lookup, asset path handling, or any other
place where slash-separated paths need to resolve to typed application data.

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

The router's zero value is ready to use. Store whatever value type is useful for
your application:

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

Use `Match` to look up a path:

```go
value, params, ok := router.Match("/users/42")
```

Use `MatchInto` when matching in a hot path and you want to reuse parameter
storage:

```go
buf := match.NewParams(4)

value, params, ok := router.MatchInto("/users/42", buf)
_, _, _ = value, params, ok
```

`Router` is not safe for concurrent mutation. If routes are inserted while other
goroutines are matching, synchronize access around the router.

## Route Grammar

Routes are slash-separated paths made from literal text, named parameters, and
catch-all parameters.

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

Catch-all parameters can include a literal prefix in the final segment:

```go
router.Insert("/static/prefix-{*path}", value)

_, params, ok := router.Match("/static/prefix-css/site.css")
// params.Get("path") == "css/site.css"
// ok == true
```

## Matching Behavior

Literal segments are preferred over parameter segments, and catch-all routes are
considered after more specific segment matches.

```go
router.Insert("/posts/{year}/{slug}", "post")
router.Insert("/posts/{year}/index", "index")

value, _, _ := router.Match("/posts/2026/index")
// value == "index"
```

Parameters are returned in route order:

```go
router.Insert("/teams/{team}/members/{member}", "member")

_, params, _ := router.Match("/teams/core/members/ana")

params.At(0) // match.Param{Key: "team", Val: "core"}
params.At(1) // match.Param{Key: "member", Val: "ana"}
```

## Working With Params

`Params` is an opaque value type. Use its methods instead of depending on its
internal representation.

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

snapshot := params.All()
_ = snapshot
```

`Match` keeps the common case small with inline parameter storage and allocates
only when more storage is needed. `MatchInto` lets callers reuse a `Params`
buffer across matches:

```go
params := match.NewParams(8)

for _, path := range paths {
	value, matchedParams, ok := router.MatchInto(path, params)
	_, _, _ = value, matchedParams, ok
}
```

## Insert Errors and Conflicts

`TryInsert` returns an error when a route is invalid, duplicated, or ambiguous.
`Insert` panics on those same errors.

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

Ambiguous dynamic routes also conflict when the same path could select either
route, such as `/user_{name}` and `/user_{id}`.

## Development

Run the test suite with:

```sh
go test ./...
```

Run benchmarks with:

```sh
go test -bench=. -benchmem ./...
```
