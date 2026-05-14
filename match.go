package match

// Router maps path patterns to caller-provided values.
//
// The zero value is ready to use. After routes are registered, a Router may be
// used by multiple goroutines for matching. Callers that insert routes while
// other goroutines use the router must synchronize access.
type Router[T any] struct {
	root node[T]
}

// PrefixMatch contains the result of a successful prefix match.
//
// Rest is the remaining path after the matched prefix. It is always "/" when
// the match consumes the full path.
type PrefixMatch[T any] struct {
	Value  T
	Params Params
	Rest   string
}

// Insert registers route with value.
//
// It panics with the same errors returned by TryInsert when route is invalid or
// conflicts with an existing route.
func (r *Router[T]) Insert(route string, value T) {
	if err := r.TryInsert(route, value); err != nil {
		panic(err)
	}
}

// TryInsert registers route with value.
//
// It returns an error when route has invalid parameter syntax or when it would
// conflict with an existing route. Duplicate and ambiguous routes return a
// *ConflictError.
func (r *Router[T]) TryInsert(route string, value T) error {
	return r.root.insert(route, value)
}

// Match returns the value and parameters for path.
//
// The boolean result is false when no registered route matches; in that case
// the value is the zero value of T and the returned Params is empty.
func (r *Router[T]) Match(path string) (T, Params, bool) {
	return r.root.match(path)
}

// MatchInto returns the value and parameters for path using params as storage.
//
// The input Params value is reset before matching. Use NewParams to create a
// reusable Params buffer large enough for the expected number of captures.
func (r *Router[T]) MatchInto(path string, params Params) (T, Params, bool) {
	return r.root.matchInto(path, params)
}

// MatchPrefix returns the value, parameters, and remaining path for the best
// registered route that matches the front of path.
//
// The boolean result is false when no registered route matches a whole-segment
// prefix of path. When multiple routes match, the route that consumes the most
// path wins. A route registered as "/" matches the root prefix of any absolute
// path.
func (r *Router[T]) MatchPrefix(path string) (PrefixMatch[T], bool) {
	return r.root.matchPrefix(path)
}

// MatchPrefixInto is like MatchPrefix, but uses params as parameter storage.
//
// The input Params value is reset before matching. Use NewParams to create a
// reusable Params buffer large enough for the expected number of captures.
func (r *Router[T]) MatchPrefixInto(path string, params Params) (PrefixMatch[T], bool) {
	return r.root.matchPrefixInto(path, params)
}
