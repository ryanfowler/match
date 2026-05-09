# match

`match` is a small generic path router for Go.

```go
var router match.Router[string]

router.Insert("/posts/{year}/{slug}", "post")
router.Insert("/static/{*path}", "asset")

value, params, ok := router.Match("/posts/2026/route-grammar")
// value == "post"
// params.Get("year") == "2026"
// params.Get("slug") == "route-grammar"
// ok == true
```

## Route Grammar

Routes are slash-separated paths made from literal text, named parameters, and
catch-all parameters.

| Syntax | Meaning |
| --- | --- |
| `/about` | Literal text matches itself. |
| `/{page}` | A named parameter captures one non-empty path segment. |
| `/files/{name}.json` | Parameters may have literal prefixes or suffixes in the same segment. |
| `/static/{*path}` | A catch-all parameter captures the non-empty remainder of the path, including slashes. |
| `/{{/x/}}` | `{{` matches literal `{` and `}}` matches literal `}`. |

Parameter names must be non-empty. Names cannot contain `/`, and `*` is only
valid as the first character of a catch-all parameter. Escaped braces may appear
inside parameter names, so `{ba{{r}` registers the parameter name `ba{r`.

Each path segment may contain at most one parameter:

```go
router.TryInsert("/{first}-{second}", value) // ErrInvalidParamSegment
```

Catch-all parameters use `{*name}`. They must appear at the end of the route and
capture at least one byte:

```go
router.Insert("/static/{*path}", value)

_, params, ok := router.Match("/static/css/site.css")
// params.Get("path") == "css/site.css"
// ok == true
```

## Matching

Literal segments are matched before parameter segments, and catch-all routes are
considered after more specific segment matches:

```go
router.Insert("/posts/{year}/{slug}", "post")
router.Insert("/posts/{year}/index", "index")

value, _, _ := router.Match("/posts/2026/index")
// value == "index"
```

Parameters are returned in route order. `Match` allocates parameter storage as
needed. `MatchInto` reuses a caller-provided `Params` buffer.

## Conflicts

`TryInsert` rejects invalid, duplicate, and ambiguous routes. `Insert` panics on
the same errors.

Duplicate routes conflict even when parameter names differ:

```go
var router match.Router[string]

router.Insert("/x/{id}/bar", "id")
err := router.TryInsert("/x/{name}/bar", "name")

var conflict *match.ConflictError
// errors.As(err, &conflict) == true
```

Ambiguous dynamic routes also conflict when the same path could select either
route, such as `/user_{name}` and `/user_{id}`.
