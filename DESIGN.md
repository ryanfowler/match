# Technical Architecture

This document describes the implementation architecture of `github.com/ryanfowler/match`.
It focuses on how the library stores routes, detects invalid or ambiguous route
tables, performs exact and prefix matching, and manages captured parameters.

## Purpose

`match` is a small generic path router. It maps slash-separated route patterns
to caller-provided Go values and later resolves path-like strings back to those
values plus any captured parameters.

The package is deliberately narrower than an HTTP framework. It does not know
about HTTP methods, redirects, middleware, request objects, URL decoding, or path
cleaning. The core abstraction is only:

```go
route pattern + value -> match path -> value + Params + ok
```

That narrow scope drives most of the architecture:

- Route parsing and validation happen once during insertion.
- Matching uses a compact segment trie.
- Ambiguous routes are rejected before they can make runtime matching depend on
  insertion order.
- Captured parameters are collected only after the winning route is known.
- Parameter storage avoids heap allocation for the common case.

The package has no third-party dependencies. It requires Go 1.23, primarily
because `Params.Seq` exposes an `iter.Seq2`.

## Source Layout

| File | Responsibility |
| --- | --- |
| `doc.go` | Package-level documentation and public behavior summary. |
| `match.go` | Public `Router[T]` and `PrefixMatch[T]` API surface. |
| `node.go` | Route parser, route entries, conflict detection, trie construction, exact matching, prefix matching, and low-level path helpers. |
| `params.go` | Public `Param` and `Params` types plus allocation-conscious parameter storage helpers. |
| `match_test.go` | Behavioral tests for route grammar, matching, prefix matching, conflicts, and `Params`. |
| `match_bench_test.go` | Benchmarks for insertion, exact matching, prefix matching, misses, and reusable parameter buffers. |

## High-Level Components

At runtime the router is a thin public wrapper around an internal `node[T]`:

```text
Router[T]
  root node[T]

node[T]
  routes          []*routeEntry[T]
  normalized      map[string]string
  conflictIndex   routeConflictIndex[T]
  root            segmentNode[T]
  absoluteRoot    *segmentNode[T]
  rootPrefix      *routeEntry[T]
```

The main subsystems are:

- Public API: `Router[T]` exposes insertion, exact matching, and prefix matching.
- Route model: `routeEntry[T]` stores the parsed canonical representation of a
  registered route.
- Parser: route strings are tokenized, split into path segments, converted to
  segment patterns, and normalized for duplicate detection.
- Conflict index: dynamic routes are indexed so ambiguous definitions can be
  rejected at insertion time without comparing every route in common cases.
- Segment trie: the matcher walks static, parameter, and catch-all edges in a
  deterministic specificity order.
- Parameter storage: `Params` stores up to four captures inline and optionally
  grows to a heap slice.

The normal lifecycle is:

```text
TryInsert(route, value)
  -> parse route if it contains braces
  -> build routeEntry
  -> reject duplicate normalized shape
  -> reject ambiguous conflicts
  -> insert routeEntry into trie
  -> add routeEntry to conflict index

Match(path)
  -> choose root search node
  -> walk trie by path segment
  -> select winning routeEntry
  -> collect captures from path using routeEntry metadata
  -> return value, Params, true
```

## Public API Layer

`Router[T]` in `match.go` is intentionally small:

```go
type Router[T any] struct {
	root node[T]
}
```

The zero value is usable because `node[T]` lazily allocates only the structures
it needs during insertion. The methods delegate directly to the internal root:

- `Insert(route, value)` calls `TryInsert` and panics on error.
- `TryInsert(route, value)` registers a route or returns validation/conflict
  errors.
- `Match(path)` returns an exact match.
- `MatchInto(path, params)` returns an exact match while reusing caller-provided
  parameter storage.
- `MatchPrefix(path)` returns the best whole-segment route prefix plus the
  remaining path.
- `MatchPrefixInto(path, params)` combines prefix matching with reusable
  parameter storage.

`PrefixMatch[T]` is the public result type for prefix matching:

```go
type PrefixMatch[T any] struct {
	Value  T
	Params Params
	Rest   string
}
```

`Rest` is normalized by the matcher to `/` when the route consumes the full
input path. It is otherwise the remaining path beginning at a segment boundary.

## Route Semantics

Routes and paths are plain strings. The library treats `/` as the segment
separator but does not normalize anything before matching.

Important consequences:

- Absolute and relative paths are distinct.
- Empty segments are significant.
- Trailing slashes are significant.
- URL escapes are not decoded.
- `.` and `..` are not cleaned.
- The empty route `""` is a valid exact route for the empty path.

The route grammar supports:

- Literal text.
- Named parameters: `{name}`.
- Affixed named parameters: `{name}.json`, `user_{id}`.
- Catch-all parameters: `{*name}`.
- Affixed catch-all parameters with a literal prefix: `prefix-{*path}`.
- Escaped literal braces: `{{` and `}}`.

Parameters and catch-all parameters always capture non-empty text. Each path
segment may contain at most one parameter. A catch-all parameter must be the
final token in the route, although literal text may appear before it in the same
final segment.

## Parsed Route Representation

Every registered route becomes a `routeEntry[T]`:

```go
type routeEntry[T any] struct {
	route                 string
	patterns              []segmentPattern
	captureNames          []string
	captureCount          int
	segmentCount          int
	order                 int
	firstStaticSegment    string
	singleCaptureSegment  uint32
	hasFirstStaticSegment bool
	hasCatchAll           bool
	value                 T
}
```

The fields serve distinct parts of the system:

- `route` is the canonical route string used in conflict reporting. Dynamic
  routes have escaped braces unescaped before storage.
- `patterns` contains one `segmentPattern` per path segment.
- `captureNames` is indexed by segment number. This works because the grammar
  permits at most one capture per segment.
- `captureCount` is used to skip parameter collection work and decide whether
  the conflict index needs the route.
- `segmentCount` lets the conflict index compare same-length routes quickly.
- `order` preserves registration order for deterministic conflict reporting.
- `firstStaticSegment` and `hasFirstStaticSegment` are used as conflict-index
  discriminators.
- `singleCaptureSegment` is a fast path for collecting the common single-param
  route.
- `hasCatchAll` controls catch-all conflict-index behavior.
- `value` is the caller-provided value returned on match.

Each route segment is represented by `segmentPattern`:

```go
type segmentPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}
```

For a literal segment, `raw` is the entire segment and `literal` is true. For a
parameter segment, `prefix` and `suffix` store the literal text around the
single capture. For example:

| Route segment | Pattern |
| --- | --- |
| `users` | literal, `raw="users"` |
| `{id}` | param, empty prefix and suffix |
| `user_{id}` | param, `prefix="user_"` |
| `{name}.json` | param, `suffix=".json"` |
| `prefix-{*path}` | catch-all, `prefix="prefix-"` |

`raw` for dynamic segments is the literal text only, which means it is the
concatenation of `prefix` and `suffix`.

## Parsing and Normalization

Insertion has two paths:

- Routes without `{` or `}` use `insertLiteral`.
- Routes containing braces use `insertDynamic`, even if those braces are escaped
  and the resulting route has no captures.

Literal routes are split directly by `/` using `literalSegmentPatterns`. They do
not need grammar validation because there are no parameter markers.

Dynamic routes go through `parseRoute`:

1. Scan the route byte by byte.
2. Accumulate literal text in a `strings.Builder`.
3. Treat `{{` and `}}` as escaped literal braces.
4. Convert `{name}` to `tokenParam`.
5. Convert `{*name}` to `tokenCatchAll`.
6. Reject malformed syntax with sentinel errors.
7. Build a normalized route shape while scanning.

The parser returns:

```go
tokens []token
normalized string
err error
```

`token` has a kind and text:

```go
type token struct {
	kind tokenKind
	text string
}
```

The normalized shape is used to detect duplicates independent of parameter
names. Literal portions include their byte length, while parameters are encoded
by ordinal:

```text
L<length>:<literal>
P<ordinal>;
C<ordinal>;
```

Literal-only routes registered through the literal fast path are prefixed with
`S` by `normalizedStaticLiteral`. This prevents a static route string from
colliding with a dynamic route's normalized encoding.

After tokenization, `splitTokenSegments` splits tokens on literal `/` bytes. It
preserves empty segments, so absolute paths, trailing slashes, and repeated
slashes retain their exact shape. `makeSegmentPatterns` then converts each
segment's tokens into a `segmentPattern` and records capture names.

## Insertion Pipeline

Both insertion paths produce a `routeEntry[T]`, then follow the same broad
process:

```text
build entry
  -> initialize normalized map if needed
  -> reject duplicate normalized shape
  -> find ambiguous conflict if needed
  -> store normalized shape
  -> append to node.routes
  -> add to conflict index
  -> insert into segment trie
  -> refresh root-prefix cache
```

The duplicate map catches exact route-shape collisions. For example,
`/x/{id}/bar` and `/x/{name}/bar` normalize to the same shape, so the second
insert returns `*ConflictError`.

The conflict index catches ambiguous overlaps that are not identical normalized
shapes. For example, `/files/{name}.json` conflicts with `/files/report.{ext}`
because the path `/files/report.json` could satisfy both patterns.

Static routes and dynamic routes may intentionally overlap when specificity
makes the result deterministic. For example, `/path/foo` can coexist with
`/path/{*rest}` because exact matching tries the static route before the
catch-all route.

## Trie Structure

The matching trie is built from `segmentNode[T]` values:

```go
type segmentNode[T any] struct {
	static      []staticEdge[T]
	staticIndex map[string]*segmentNode[T]
	params      []paramEdge[T]
	catchAll    []catchAllEdge[T]
	value       *routeEntry[T]
}
```

Each node can have:

- Static edges for literal segment transitions.
- Parameter edges for one-segment captures, including affixed parameters.
- Catch-all edges that terminate matching by consuming the remaining path.
- An optional route value when a route ends at that node.

Static edges are stored as a small slice first. When a node reaches nine static
children, `addStaticChild` builds `staticIndex` for O(1)-style lookup while
keeping the compact slice representation for small route tables.

Parameter edges are stored as segment patterns and sorted by segment-level
specificity:

1. More literal affix text first.
2. If tied, longer prefix first.

This makes `/user-{id}` get tried before `/{id}` for a segment like `user-42`.

Catch-all edges are terminal. `insertTree` appends a catch-all edge and returns
because the parser already guarantees a catch-all can only appear at the end of
the route.

## Absolute Root Optimization

Absolute routes begin with an empty first segment. For example, `/api` is stored
as the segment sequence `["", "api"]`.

The trie keeps an `absoluteRoot` pointer to the child reached through the root
empty segment. During matching, `matchRoot` can skip the leading empty segment
for absolute input paths:

```text
"/api" with absoluteRoot -> start at absoluteRoot, index 1
```

This optimization is disabled when the root node has parameter or catch-all
edges, because relative root-level dynamic routes can legally match absolute
paths. For example, `{*path}` can match `/other` and capture `/other`.

## Exact Matching

`Router.Match` delegates to `node.match`:

```text
node.match(path)
  -> root, index := matchRoot(path)
  -> entry := root.matchPath(path, index)
  -> collect params if entry found
```

`segmentNode.matchPath` is recursive:

1. If there are no remaining segments, return the node's value if present.
2. Read the next path segment with `nextPathSegment`.
3. Try the matching static child first.
4. Try matching parameter edges.
5. Try matching catch-all edges.
6. Return miss if nothing succeeds.

The static-first order gives literal routes priority over dynamic routes.

Parameter matching enforces non-empty captures:

- Bare parameters reject empty segments.
- Affixed parameters require the segment to have the configured prefix and
  suffix, with non-empty captured text between them.

After a parameter edge produces a successful route, the matcher checks later
matching parameter edges at the same node and chooses the more specific route
with `moreSpecificRoute`. This handles cases where multiple parameter branches
can match the same segment but only one should win globally.

Catch-all matching checks the remaining path string starting at the current
segment index. A bare catch-all captures the entire non-empty remainder. An
affixed catch-all requires the remainder to start with the configured prefix and
captures the non-empty text after that prefix.

## Specificity Rules

Specificity is centralized in `moreSpecificRoute` and related edge sorting.

For full route comparisons:

1. Literal segments beat non-literal segments.
2. Non-catch-all dynamic segments beat catch-all segments.
3. For parameter segments, more literal affix text beats less literal text.
4. If affix length ties, longer prefix beats shorter prefix.
5. If all compared segments tie, the longer route is more specific.

Insertion-time conflict detection rejects route pairs that would be ambiguous
under these rules. Runtime matching can therefore make deterministic choices
without exposing insertion order as part of route precedence.

## Conflict Detection

The library rejects two broad classes of insertion problems:

- Invalid syntax, reported with sentinel errors.
- Duplicate or ambiguous routes, reported as `*ConflictError`.

Syntax errors include:

- `ErrInvalidParamSegment`: more than one parameter in a path segment.
- `ErrInvalidParam`: malformed parameter syntax or invalid parameter name.
- `ErrInvalidCatchAll`: catch-all parameter not placed at the end of the route.

Duplicate route shapes are caught by the `normalized` map before the conflict
index is consulted.

The conflict index exists for non-identical dynamic patterns that still overlap.
It is shaped like this:

```go
type routeConflictIndex[T any] struct {
	bySegmentCount map[int]*routeConflictBucket[T]
	catchAll       routeConflictBucket[T]
}

type routeConflictBucket[T any] struct {
	all      []*routeEntry[T]
	static   map[string][]*routeEntry[T]
	wildcard []*routeEntry[T]
}
```

Only routes with captures are added to the index. This keeps literal route
insertion cheap and allows static routes to overlap dynamic routes when
specificity makes matching deterministic.

The index first groups dynamic routes by segment count. Within each count it
uses the first definitely-static segment as an additional discriminator. For
absolute routes, the leading empty segment is skipped because it is common to
all absolute paths and does not narrow the search.

Routes without a useful first static segment go into the bucket's `wildcard`
list. A new route with a first static segment needs to compare only:

- Existing routes with the same first static segment.
- Existing wildcard routes.

A new route without a first static segment must compare against the whole
bucket.

Catch-all routes need special handling because they can overlap routes with
more or fewer segments. The index keeps a separate `catchAll` bucket so:

- New catch-all routes can be checked against all segment-count buckets.
- New non-catch-all routes can be checked against existing catch-all routes.

`conflictsEntries` combines two checks:

1. Catch-all prefix conflicts in either direction.
2. Same-length segment-pattern conflicts.

Same-length conflicts require every segment to be able to overlap and at least
one segment to be genuinely ambiguous. Literal-vs-dynamic overlap alone is not
ambiguous because literal matching has higher precedence.

Catch-all prefix conflicts are more subtle. A catch-all route conflicts with a
dynamic route when the catch-all can consume a path suffix that the other route
could also match. Static routes below a catch-all are allowed, and dynamic
routes that would require an empty catch-all capture are allowed because
catch-all captures must be non-empty.

When multiple existing routes conflict with a new route, `earlierConflict`
reports the earliest registered one. This keeps error reporting deterministic.

## Prefix Matching

Prefix matching uses the same trie and segment grammar as exact matching. The
difference is that it tracks the best route value encountered while walking the
path.

`node.matchPrefixRoute` does three things:

1. Calls `matchRoot` to choose the starting trie node and path index.
2. Calls `segmentNode.matchPrefixPath` to find the best trie prefix.
3. Also considers `rootPrefixMatch` for a registered `/` route.

`segmentNode.matchPrefixPath` tracks a `prefixMatch[T]`:

```go
type prefixMatch[T any] struct {
	entry     *routeEntry[T]
	restIndex int
	consumed  int
}
```

The best prefix is chosen by:

1. More consumed path wins.
2. If consumed length ties, `moreSpecificRoute` wins.

This means `/api/v1` beats `/api` for `/api/v1/users`, and a more specific
parameter route beats a less specific parameter route when both consume the same
prefix.

The route `/` is special for prefix matching. Exact matching treats `/` as a
normal route, but prefix matching should allow `/` to match the root prefix of
any absolute path. `refreshRootPrefix` caches that route entry, and
`rootPrefixMatch` considers it separately.

`remainingPrefixPath` converts the stored `restIndex` into the public `Rest`
string. It returns `/` when the match consumes the whole path. Otherwise it
returns a slash-prefixed remainder at a whole-segment boundary. It also handles
the root-prefix case for paths like `//users/42` so the rest remains normalized
as `/users/42`.

## Parameter Collection

The matcher does not build `Params` while walking the trie. Instead it first
selects the winning `routeEntry[T]` and then calls `collectParams`.

This design has three advantages:

- Backtracking across parameter branches cannot leave stale captures behind.
- The captured names come from the canonical route entry that actually won.
- Routes without captures skip all parameter work.

`collectParams` has separate paths for:

- Zero captures: return the input `Params` unchanged.
- One capture: jump directly to `singleCaptureSegment`.
- Multiple captures: scan the path and route patterns together.

For routes with more than four captures, `collectParams` calls
`Params.ensureCapacity` before appending. This lets `Match` allocate once for a
large capture set and lets `MatchInto` reuse caller-provided heap capacity.

Capture extraction uses the same helpers as matching:

- `matchAffixedParamPattern` for prefix/suffix parameters.
- `matchCatchAllPattern` for catch-all parameters.

## Params Architecture

`Params` is a public opaque value type:

```go
type Params struct {
	len    int
	inline [inlineParams]Param
	heap   []Param
}
```

`inlineParams` is four. Up to four captures are stored directly in the `Params`
value, avoiding heap allocation for common route shapes.

When more than four captures are needed, `Params` switches to `heap` storage.
The heap slice is kept inside the value so callers can pass it back to
`MatchInto` or `MatchPrefixInto` for reuse.

The main operations are:

- `NewParams(capacity)` creates an empty reusable buffer.
- `ParamsOf` constructs a `Params` from explicit values.
- `Merge` concatenates two parameter sets without deduplicating keys.
- `Len` and `At` provide indexed access.
- `Get` and `TryGet` perform linear name lookup.
- `AppendTo` appends captures to an existing slice.
- `All` returns a snapshot slice.
- `Seq` supports range-over-function iteration.

`Get` and `TryGet` are intentionally linear. Parameter counts are expected to be
small, and avoiding a map keeps the hot path compact.

`Params.reset` clears the logical length while preserving heap capacity. This is
why `MatchInto` and `MatchPrefixInto` can avoid allocation after the caller has
provided a sufficiently large buffer.

## Error Model

`TryInsert` is the non-panicking insertion API. It can return:

- `ErrInvalidParamSegment`
- `ErrInvalidParam`
- `ErrInvalidCatchAll`
- `*ConflictError`

`Insert` is a convenience wrapper for trusted route tables. It panics with the
same error that `TryInsert` would have returned.

`ConflictError` contains:

```go
type ConflictError struct {
	Route string
	With  string
}
```

`Route` is the route that failed to insert. `With` is the previously registered
route that caused the conflict.

## Concurrency Model

After registration, a `Router[T]` can be shared by multiple goroutines for
matching. Matching reads immutable route entries and trie structures.

Insertion mutates:

- `node.routes`
- `node.normalized`
- `node.conflictIndex`
- `node.root` and descendants
- `node.absoluteRoot`
- `node.rootPrefix`

Callers that insert while matching, or insert from multiple goroutines, must
synchronize access externally.

## Performance Characteristics

The implementation is tuned around common routing workloads:

- Literal-only routes use a parser fast path.
- Routes are parsed once during insertion.
- Matching walks by path segment instead of scanning all routes.
- Static edges stay slice-backed for small fanout and gain a map after the
  ninth static child.
- Parameter edges are ordered by specificity to find likely winners early.
- Captures are collected after route selection to keep branch exploration cheap.
- Up to four captures are inline in `Params`.
- `MatchInto` and `MatchPrefixInto` let hot loops reuse heap-backed parameter
  buffers.
- The conflict index narrows ambiguous-route checks by segment count and first
  useful static segment.

Expected matching cost is roughly proportional to the number of path segments
and the number of dynamic alternatives at each traversed node. Static-heavy
route tables are especially cheap because a segment usually selects one child.

Insertion is more expensive than matching because it performs validation,
normalization, conflict detection, and trie construction. That tradeoff is
intentional: route tables are normally built at startup, while matching happens
on the hot path.

## Testing Strategy

The tests cover the behavior that defines the architecture:

- Core exact matching across static, parameter, affixed parameter, catch-all,
  escaped brace, relative, empty, and trailing-slash routes.
- Miss behavior, including empty captures and exact route shape mismatches.
- Canonical route-entry pointer stability after route-table growth.
- Absolute-root optimization fallback when root dynamic edges exist.
- Static child indexing after the fanout threshold.
- More-specific parameter route selection.
- Insert-time syntax errors and panics.
- Duplicate and ambiguous conflict detection.
- Catch-all overlap rules.
- Large parameter counts and parameter allocation behavior.
- `MatchInto` and `MatchPrefixInto` buffer reuse.
- Prefix matching semantics, including root prefix matching and longest-prefix
  selection.
- `Params` accessors, snapshots, merging, and iteration.

Benchmarks exercise:

- Static, parameter, catch-all, mixed, and large-route exact matching.
- Match misses.
- Reusable-buffer matching.
- Prefix matching.
- Insertion for mixed, generated static, and generated dynamic route tables.

## Maintenance Notes

Several parts of the implementation are intentionally coupled. Changes in one
area usually require updates elsewhere.

When changing the route grammar, check:

- `parseRoute`
- `findParamEnd`
- `splitTokenSegments`
- `makeSegment`
- `segmentPattern`
- `matchAffixedParamPattern`
- `matchCatchAllPattern`
- `collectParams`
- Conflict helpers such as `segmentMayOverlap` and `catchAllOverlapsSuffix`
- README, package docs, tests, and this document

When changing precedence or specificity, check:

- `sortParamEdges`
- `paramEdgeLess`
- `moreSpecificRoute`
- `matchPath`
- `matchPrefixPath`
- Conflict detection, because accepted overlaps must remain deterministic

When changing `Params`, preserve these public expectations:

- `Params` remains an opaque value type.
- `Len`, `At`, `Get`, `TryGet`, `AppendTo`, `All`, and `Seq` keep their current
  semantics.
- `MatchInto` and `MatchPrefixInto` reset logical length and preserve reusable
  heap capacity.
- Small capture sets avoid allocation.

When changing insertion, preserve these invariants:

- Duplicate normalized shapes are rejected.
- Ambiguous dynamic overlaps are rejected.
- Static routes may coexist with broader dynamic routes when precedence is
  deterministic.
- Conflict reporting identifies the earliest previously registered conflicting
  route.
- Trie entries point to stable canonical `routeEntry[T]` objects.

## Non-Goals

The architecture intentionally does not include:

- HTTP method routing.
- Middleware chains.
- Request or response abstractions.
- Automatic redirects.
- Path cleaning.
- URL decoding.
- Case-insensitive matching.
- A parameter lookup map.
- Internal locking around insertion and matching.

These omissions keep the router small, deterministic, and reusable outside HTTP
servers.
