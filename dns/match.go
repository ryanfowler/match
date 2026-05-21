package dns

// Router maps DNS hostname patterns to caller-provided values.
//
// The zero value is ready to use. After patterns are registered, a Router may
// be used by multiple goroutines for matching. Callers that insert patterns
// while other goroutines use the router must synchronize access.
type Router[T any] struct {
	root node[T]
}

// Clone returns a Router containing a deep copy of r's matching state.
//
// Future inserts into the returned Router do not mutate r. Stored values are
// copied by assignment.
func (r *Router[T]) Clone() Router[T] {
	return Router[T]{root: r.root.clone()}
}

// SuffixMatch contains the result of a successful suffix match.
//
// Prefix is the unmatched labels to the left of the matched suffix. It is empty
// when the match consumes the full hostname.
type SuffixMatch[T any] struct {
	Value  T
	Params Params
	Prefix string
}

// Insert registers pattern with value.
//
// It panics with the same errors returned by TryInsert when pattern is invalid
// or conflicts with an existing pattern.
func (r *Router[T]) Insert(pattern string, value T) {
	if err := r.TryInsert(pattern, value); err != nil {
		panic(err)
	}
}

// TryInsert registers pattern with value.
//
// It returns an error when pattern has invalid parameter syntax, invalid
// hostname-label structure, or when it would conflict with an existing pattern.
// Duplicate and ambiguous patterns return a *ConflictError.
func (r *Router[T]) TryInsert(pattern string, value T) error {
	return r.root.insert(pattern, value)
}

// Match returns the value and parameters for hostname.
//
// Hostname matching is ASCII case-insensitive. A single trailing root dot is
// ignored, so example.com and example.com. are equivalent. The boolean result
// is false when no registered pattern matches or hostname is malformed.
func (r *Router[T]) Match(hostname string) (T, Params, bool) {
	return r.root.match(hostname)
}

// MatchInto returns the value and parameters for hostname using params as
// storage.
//
// The input Params value is reset before matching. Use NewParams to create a
// reusable Params buffer large enough for the expected number of captures.
func (r *Router[T]) MatchInto(hostname string, params Params) (T, Params, bool) {
	return r.root.matchInto(hostname, params)
}

// MatchSuffix returns the value, parameters, and unmatched prefix for the best
// registered pattern that matches the right-hand suffix of hostname.
//
// The boolean result is false when no registered pattern matches a whole-label
// suffix of hostname. When multiple patterns match, the pattern that consumes
// the most hostname labels wins.
func (r *Router[T]) MatchSuffix(hostname string) (SuffixMatch[T], bool) {
	return r.root.matchSuffix(hostname)
}

// MatchSuffixInto is like MatchSuffix, but uses params as parameter storage.
//
// The input Params value is reset before matching. Use NewParams to create a
// reusable Params buffer large enough for the expected number of captures.
func (r *Router[T]) MatchSuffixInto(hostname string, params Params) (SuffixMatch[T], bool) {
	return r.root.matchSuffixInto(hostname, params)
}
