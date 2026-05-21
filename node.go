package match

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrInvalidParamSegment reports a route segment that contains more than
	// one parameter.
	ErrInvalidParamSegment = errors.New("only one parameter is allowed per path segment")

	// ErrInvalidParam reports malformed parameter syntax or an invalid
	// parameter name.
	ErrInvalidParam = errors.New("parameters must be registered with a valid name")

	// ErrInvalidCatchAll reports a catch-all parameter that is not the final
	// token in its route.
	ErrInvalidCatchAll = errors.New("catch-all parameters are only allowed at the end of a route")
)

// ConflictError reports a route that cannot be inserted because it overlaps an
// already registered route.
type ConflictError struct {
	// Route is the route that failed to insert.
	Route string

	// With is the previously registered route that conflicts with Route.
	With string
}

// Error returns a human-readable description of the route conflict.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("insertion failed due to conflict with previously registered route: %s", e.With)
}

type tokenKind uint8

const (
	tokenLiteral tokenKind = iota
	tokenParam
	tokenCatchAll
)

type token struct {
	kind tokenKind
	text string
}

type routeEntry[T any] struct {
	route                 string
	patterns              []segmentPattern
	captureNames          []string
	captureCount          int
	segmentCount          int
	order                 int
	firstStaticSegment    string
	hasFirstStaticSegment bool
	hasCatchAll           bool
	value                 T
}

type node[T any] struct {
	routes        []*routeEntry[T]
	normalized    map[string]string
	conflictIndex routeConflictIndex[T]
	root          segmentNode[T]
	absoluteRoot  *segmentNode[T]
	rootPrefix    *routeEntry[T]
}

type routeConflictIndex[T any] struct {
	bySegmentCount map[int]*routeConflictBucket[T]
	catchAll       routeConflictBucket[T]
}

type routeConflictBucket[T any] struct {
	all      []*routeEntry[T]
	static   map[string][]*routeEntry[T]
	wildcard []*routeEntry[T]
}

type segmentNode[T any] struct {
	static      []staticEdge[T]
	staticIndex map[string]*segmentNode[T]
	params      []paramEdge[T]
	catchAll    []catchAllEdge[T]
	value       *routeEntry[T]
}

type staticEdge[T any] struct {
	segment string
	child   *segmentNode[T]
}

type paramEdge[T any] struct {
	pattern segmentPattern
	child   *segmentNode[T]
}

type catchAllEdge[T any] struct {
	pattern segmentPattern
	route   *routeEntry[T]
}

func (n *node[T]) insert(route string, value T) error {
	if !strings.ContainsAny(route, "{}") {
		return n.insertLiteral(route, value)
	}
	return n.insertDynamic(route, value)
}

func (n *node[T]) insertLiteral(route string, value T) error {
	patterns := literalSegmentPatterns(route)
	firstStaticSegment, hasFirstStaticSegment := firstDefinitelyStaticSegment(patterns)
	entry := &routeEntry[T]{
		route:                 route,
		patterns:              patterns,
		segmentCount:          len(patterns),
		order:                 len(n.routes),
		firstStaticSegment:    firstStaticSegment,
		hasFirstStaticSegment: hasFirstStaticSegment,
		value:                 value,
	}
	normalized := normalizedStaticLiteral(route)
	catchAllStaticConflict := n.root.conflictsWithCatchAllStaticPatterns(patterns, 0)

	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[normalized]; ok {
		return &ConflictError{Route: entry.route, With: existing}
	}

	if entry.captureCount != 0 || catchAllStaticConflict {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Route: entry.route, With: existing.route}
		}
	}

	n.normalized[normalized] = entry.route
	n.routes = append(n.routes, entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	n.refreshRootPrefix(entry)
	return nil
}

func (n *node[T]) insertDynamic(route string, value T) error {
	tokens, normalized, err := parseRoute(route)
	if err != nil {
		return err
	}

	segments := splitTokenSegments(tokens)
	patterns, captureNames, captureCount := makeSegmentPatterns(segments)
	firstStaticSegment, hasFirstStaticSegment := firstDefinitelyStaticSegment(patterns)
	entry := &routeEntry[T]{
		route:                 unescapeBraces(route),
		patterns:              patterns,
		captureNames:          captureNames,
		captureCount:          captureCount,
		segmentCount:          len(patterns),
		order:                 len(n.routes),
		firstStaticSegment:    firstStaticSegment,
		hasFirstStaticSegment: hasFirstStaticSegment,
		hasCatchAll:           hasCatchAll(patterns),
		value:                 value,
	}

	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[normalized]; ok {
		return &ConflictError{Route: entry.route, With: existing}
	}

	if entry.captureCount != 0 || n.root.conflictsWithCatchAllStatic(segments, 0) {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Route: entry.route, With: existing.route}
		}
	}

	n.normalized[normalized] = entry.route
	n.routes = append(n.routes, entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	n.refreshRootPrefix(entry)
	return nil
}

func normalizedStaticLiteral(route string) string {
	return "S" + route
}

func (n *node[T]) refreshRootPrefix(entry *routeEntry[T]) {
	if len(entry.patterns) == 2 &&
		entry.patterns[0].literal &&
		entry.patterns[0].raw == "" &&
		entry.patterns[1].literal &&
		entry.patterns[1].raw == "" {
		n.rootPrefix = entry
	}
}

func (i *routeConflictIndex[T]) add(entry *routeEntry[T]) {
	if entry.captureCount == 0 {
		return
	}
	if i.bySegmentCount == nil {
		i.bySegmentCount = make(map[int]*routeConflictBucket[T])
	}
	bucket := i.bySegmentCount[entry.segmentCount]
	if bucket == nil {
		bucket = &routeConflictBucket[T]{}
		i.bySegmentCount[entry.segmentCount] = bucket
	}
	bucket.add(entry)
	if entry.hasCatchAll {
		i.catchAll.add(entry)
	}
}

func (b *routeConflictBucket[T]) add(entry *routeEntry[T]) {
	b.all = append(b.all, entry)
	if !entry.hasFirstStaticSegment {
		b.wildcard = append(b.wildcard, entry)
		return
	}
	if b.static == nil {
		b.static = make(map[string][]*routeEntry[T])
	}
	b.static[entry.firstStaticSegment] = append(b.static[entry.firstStaticSegment], entry)
}

func (i *routeConflictIndex[T]) findConflict(entry *routeEntry[T]) *routeEntry[T] {
	var best *routeEntry[T]
	if bucket := i.bySegmentCount[entry.segmentCount]; bucket != nil {
		best = earlierConflict(best, bucket.findConflict(entry, 0))
	}

	if entry.hasCatchAll {
		for segmentCount, bucket := range i.bySegmentCount {
			if segmentCount == entry.segmentCount {
				continue
			}
			best = earlierConflict(best, bucket.findConflict(entry, 0))
		}
		return best
	}

	return earlierConflict(best, i.catchAll.findConflict(entry, entry.segmentCount))
}

func (b *routeConflictBucket[T]) findConflict(entry *routeEntry[T], skipSegmentCount int) *routeEntry[T] {
	if b == nil {
		return nil
	}
	if entry.hasFirstStaticSegment {
		static := findConflictInRoutes(b.static[entry.firstStaticSegment], entry, skipSegmentCount)
		wildcard := findConflictInRoutes(b.wildcard, entry, skipSegmentCount)
		return earlierConflict(static, wildcard)
	}
	return findConflictInRoutes(b.all, entry, skipSegmentCount)
}

func findConflictInRoutes[T any](routes []*routeEntry[T], entry *routeEntry[T], skipSegmentCount int) *routeEntry[T] {
	for _, existing := range routes {
		if skipSegmentCount != 0 && existing.segmentCount == skipSegmentCount {
			continue
		}
		if entry.captureCount == 0 && existing.captureCount == 0 {
			continue
		}
		if conflictsEntries(existing, entry) {
			return existing
		}
	}
	return nil
}

func earlierConflict[T any](a, b *routeEntry[T]) *routeEntry[T] {
	if a == nil || (b != nil && b.order < a.order) {
		return b
	}
	return a
}

func (n *node[T]) match(route string) (T, Params, bool) {
	root, index, _ := n.matchRoot(route)
	entry, ok := root.matchPath(route, index)
	if !ok {
		var val T
		return val, Params{}, false
	}
	return entry.value, collectParams(entry, route, Params{}), true
}

func (n *node[T]) matchInto(route string, params Params) (T, Params, bool) {
	root, index, _ := n.matchRoot(route)
	params = params.reset()
	entry, ok := root.matchPath(route, index)
	if !ok {
		var val T
		return val, params, false
	}
	return entry.value, collectParams(entry, route, params), true
}

func (n *node[T]) matchPrefix(path string) (PrefixMatch[T], bool) {
	match, ok := n.matchPrefixRoute(path)
	if !ok {
		return PrefixMatch[T]{}, false
	}
	return match.prefix(path, collectParams(match.entry, path, Params{})), true
}

func (n *node[T]) matchPrefixInto(path string, params Params) (PrefixMatch[T], bool) {
	params = params.reset()
	match, ok := n.matchPrefixRoute(path)
	if !ok {
		return PrefixMatch[T]{Params: params}, false
	}
	return match.prefix(path, collectParams(match.entry, path, params)), true
}

func (n *node[T]) matchPrefixRoute(path string) (prefixMatch[T], bool) {
	root, index, skippedRoot := n.matchRoot(path)
	best, ok := root.matchPrefixPath(path, index, skippedRoot)
	if rootMatch, rootOK := n.rootPrefixMatch(path); rootOK {
		best = betterPrefixMatch(best, rootMatch)
		ok = true
	}
	return best, ok
}

func (n *node[T]) rootPrefixMatch(path string) (prefixMatch[T], bool) {
	if path == "" || path[0] != '/' || n.rootPrefix == nil {
		return prefixMatch[T]{}, false
	}
	return prefixMatch[T]{
		entry:     n.rootPrefix,
		restIndex: 1,
		consumed:  1,
	}, true
}

func (n *node[T]) matchRoot(route string) (*segmentNode[T], int, bool) {
	if route == "" || route[0] != '/' || len(n.root.params) != 0 || len(n.root.catchAll) != 0 {
		return &n.root, 0, false
	}
	if n.absoluteRoot != nil {
		return n.absoluteRoot, 1, true
	}
	return &n.root, 0, false
}

func (n *node[T]) insertTree(entry *routeEntry[T]) {
	current := &n.root
	for i, pattern := range entry.patterns {
		if pattern.catchAll {
			current.catchAll = append(current.catchAll, catchAllEdge[T]{
				pattern: pattern,
				route:   entry,
			})
			return
		}

		if pattern.literal {
			child := current.staticChild(pattern.raw)
			if child == nil {
				child = &segmentNode[T]{}
				current.addStaticChild(pattern.raw, child)
			}
			if current == &n.root && pattern.raw == "" {
				n.absoluteRoot = child
			}
			current = child
		} else {
			var child *segmentNode[T]
			for j := range current.params {
				if sameSegmentPattern(current.params[j].pattern, pattern) {
					child = current.params[j].child
					break
				}
			}
			if child == nil {
				child = &segmentNode[T]{}
				current.params = append(current.params, paramEdge[T]{
					pattern: pattern,
					child:   child,
				})
				sortParamEdges(current.params)
			}
			current = child
		}

		if i == len(entry.patterns)-1 {
			current.value = entry
		}
	}
}

func (n *segmentNode[T]) matchPath(path string, index int) (*routeEntry[T], bool) {
	if index < 0 {
		if n.value != nil {
			return n.value, true
		}
		return nil, false
	}

	segment, next := nextPathSegment(path, index)
	if child := n.staticChild(segment); child != nil {
		if entry, ok := child.matchPath(path, next); ok {
			return entry, true
		}
	}

	for i := range n.params {
		pattern := n.params[i].pattern
		if pattern.prefix == "" && pattern.suffix == "" {
			if segment == "" {
				continue
			}
		} else if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
			continue
		}
		if entry, ok := n.params[i].child.matchPath(path, next); ok {
			bestEntry := entry
			for j := i + 1; j < len(n.params); j++ {
				pattern := n.params[j].pattern
				if pattern.prefix == "" && pattern.suffix == "" {
					if segment == "" {
						continue
					}
				} else if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
					continue
				}
				entry, ok := n.params[j].child.matchPath(path, next)
				if ok && moreSpecificRoute(entry, bestEntry) {
					bestEntry = entry
				}
			}
			return bestEntry, true
		}
	}

	for i := range n.catchAll {
		if _, ok := matchCatchAllPattern(n.catchAll[i].pattern, path[index:]); ok {
			return n.catchAll[i].route, true
		}
	}

	return nil, false
}

type prefixMatch[T any] struct {
	entry     *routeEntry[T]
	restIndex int
	consumed  int
}

func (m prefixMatch[T]) prefix(path string, params Params) PrefixMatch[T] {
	return PrefixMatch[T]{
		Value:  m.entry.value,
		Params: params,
		Rest:   remainingPrefixPath(path, m.restIndex),
	}
}

func (n *segmentNode[T]) matchPrefixPath(path string, index int, ignoreValue bool) (prefixMatch[T], bool) {
	var best prefixMatch[T]
	if !ignoreValue && n.value != nil {
		best = prefixMatch[T]{
			entry:     n.value,
			restIndex: index,
			consumed:  consumedPrefixPath(path, index),
		}
	}

	if index >= 0 {
		segment, next := nextPathSegment(path, index)
		if child := n.staticChild(segment); child != nil {
			ignoreChildValue := index == 0 && len(path) > 0 && path[0] == '/' && segment == ""
			if candidate, ok := child.matchPrefixPath(path, next, ignoreChildValue); ok {
				best = betterPrefixMatch(best, candidate)
			}
		}

		for i := range n.params {
			pattern := n.params[i].pattern
			if pattern.prefix == "" && pattern.suffix == "" {
				if segment == "" {
					continue
				}
			} else if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
				continue
			}
			if candidate, ok := n.params[i].child.matchPrefixPath(path, next, false); ok {
				best = betterPrefixMatch(best, candidate)
			}
		}

		for i := range n.catchAll {
			if _, ok := matchCatchAllPattern(n.catchAll[i].pattern, path[index:]); ok {
				candidate := prefixMatch[T]{
					entry:     n.catchAll[i].route,
					restIndex: -1,
					consumed:  len(path) + 1,
				}
				best = betterPrefixMatch(best, candidate)
			}
		}
	}

	return best, best.entry != nil
}

func collectParams[T any](entry *routeEntry[T], path string, params Params) Params {
	if entry.captureCount == 0 {
		return params
	}

	if entry.captureCount > inlineParams {
		params = params.ensureCapacity(entry.captureCount)
	}

	index := 0
	for i := range entry.patterns {
		pattern := entry.patterns[i]
		if pattern.catchAll {
			rest := path[index:]
			if pattern.prefix == "" {
				if rest != "" {
					params = params.append(entry.captureNames[i], rest)
				}
			} else if value, ok := matchCatchAllPattern(pattern, rest); ok {
				params = params.append(entry.captureNames[i], value)
			}
			return params
		}

		pathSegment, next := nextPathSegment(path, index)
		if pattern.param {
			if pattern.prefix == "" && pattern.suffix == "" {
				if pathSegment != "" {
					params = params.append(entry.captureNames[i], pathSegment)
				}
			} else if value, ok := matchAffixedParamPattern(pattern, pathSegment); ok {
				params = params.append(entry.captureNames[i], value)
			}
		}
		index = next
		if index < 0 {
			return params
		}
	}

	return params
}

func betterPrefixMatch[T any](best, candidate prefixMatch[T]) prefixMatch[T] {
	if best.entry == nil || candidate.consumed > best.consumed {
		return candidate
	}
	if candidate.consumed == best.consumed {
		return betterEqualPrefixMatch(best, candidate)
	}
	return best
}

// Keep the uncommon specificity tie-break out of betterPrefixMatch's hot path.
//
//go:noinline
func betterEqualPrefixMatch[T any](best, candidate prefixMatch[T]) prefixMatch[T] {
	if moreSpecificRoute(candidate.entry, best.entry) {
		return candidate
	}
	return best
}

func consumedPrefixPath(path string, index int) int {
	if index < 0 {
		return len(path) + 1
	}
	return index
}

func remainingPrefixPath(path string, index int) string {
	if index < 0 || index > len(path) || index == len(path) {
		return "/"
	}
	if path[index] == '/' {
		if index == 1 && len(path) > 1 && path[0] == '/' {
			return "/" + path[index+1:]
		}
		return path[index:]
	}
	if index == 0 {
		return path
	}
	return path[index-1:]
}

func moreSpecificRoute[T any](a, b *routeEntry[T]) bool {
	for i := 0; i < len(a.patterns) && i < len(b.patterns); i++ {
		ap := a.patterns[i]
		bp := b.patterns[i]
		if ap.literal != bp.literal {
			return ap.literal
		}
		if ap.catchAll != bp.catchAll {
			return bp.catchAll
		}
		if ap.literal || ap.catchAll {
			continue
		}
		aStatic := len(ap.prefix) + len(ap.suffix)
		bStatic := len(bp.prefix) + len(bp.suffix)
		if aStatic != bStatic {
			return aStatic > bStatic
		}
		if len(ap.prefix) != len(bp.prefix) {
			return len(ap.prefix) > len(bp.prefix)
		}
	}
	return len(a.patterns) > len(b.patterns)
}

func (n *segmentNode[T]) staticChild(segment string) *segmentNode[T] {
	if n.staticIndex != nil {
		return n.staticIndex[segment]
	}
	for i := range n.static {
		if n.static[i].segment == segment {
			return n.static[i].child
		}
	}
	return nil
}

func (n *segmentNode[T]) addStaticChild(segment string, child *segmentNode[T]) {
	n.static = append(n.static, staticEdge[T]{segment: segment, child: child})
	if len(n.static) == 9 {
		n.staticIndex = make(map[string]*segmentNode[T], len(n.static))
		for i := range n.static {
			n.staticIndex[n.static[i].segment] = n.static[i].child
		}
		return
	}
	if n.staticIndex != nil {
		n.staticIndex[segment] = child
	}
}

func (n *segmentNode[T]) conflictsWithCatchAllStatic(segments [][]token, index int) bool {
	if len(n.catchAll) > 0 {
		return true
	}
	if index == len(segments) {
		return false
	}

	segment, ok := staticSegmentRaw(segments[index])
	if !ok {
		return false
	}
	child := n.staticChild(segment)
	if child == nil {
		return false
	}
	return child.conflictsWithCatchAllStatic(segments, index+1)
}

func (n *segmentNode[T]) conflictsWithCatchAllStaticPatterns(patterns []segmentPattern, index int) bool {
	if len(n.catchAll) > 0 {
		return true
	}
	if index == len(patterns) {
		return false
	}

	pattern := patterns[index]
	if !pattern.literal {
		return false
	}
	child := n.staticChild(pattern.raw)
	if child == nil {
		return false
	}
	return child.conflictsWithCatchAllStaticPatterns(patterns, index+1)
}

func staticSegmentRaw(segment []token) (string, bool) {
	if len(segment) == 0 {
		return "", true
	}
	if len(segment) == 1 && segment[0].kind == tokenLiteral {
		return segment[0].text, true
	}
	return "", false
}

func nextPathSegment(path string, index int) (string, int) {
	if index == len(path) {
		return "", -1
	}
	end := index + 16
	if end > len(path) {
		end = len(path)
	}
	for i := index; i < end; i++ {
		if path[i] == '/' {
			return path[index:i], i + 1
		}
	}
	if end < len(path) {
		if i := strings.IndexByte(path[end:], '/'); i >= 0 {
			return path[index : end+i], end + i + 1
		}
	}
	return path[index:], -1
}

func literalSegmentPatterns(route string) []segmentPattern {
	patterns := make([]segmentPattern, 0, strings.Count(route, "/")+1)
	start := 0
	for i := 0; i < len(route); i++ {
		if route[i] != '/' {
			continue
		}
		patterns = append(patterns, segmentPattern{raw: route[start:i], literal: true})
		start = i + 1
	}
	return append(patterns, segmentPattern{raw: route[start:], literal: true})
}

func splitTokenSegments(tokens []token) [][]token {
	segments := make([][]token, 0, countTokenSegments(tokens))
	var current []token

	flush := func() {
		segments = append(segments, current)
		current = nil
	}

	for _, t := range tokens {
		if t.kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.text); i++ {
			if t.text[i] != '/' {
				continue
			}
			if i > start {
				current = append(current, token{kind: tokenLiteral, text: t.text[start:i]})
			}
			flush()
			start = i + 1
		}
		if start < len(t.text) {
			current = append(current, token{kind: tokenLiteral, text: t.text[start:]})
		}
	}

	flush()
	return segments
}

func countTokenSegments(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.kind != tokenLiteral {
			continue
		}
		count += strings.Count(t.text, "/")
	}
	return count
}

func makeSegmentPatterns(segments [][]token) ([]segmentPattern, []string, int) {
	patterns := make([]segmentPattern, len(segments))
	var captureNames []string
	captureCount := 0
	for i := range segments {
		var capture string
		patterns[i], capture = makeSegment(segments[i])
		if capture != "" {
			if captureNames == nil {
				captureNames = make([]string, len(segments))
			}
			captureNames[i] = capture
			captureCount++
		}
	}
	return patterns, captureNames, captureCount
}

func firstDefinitelyStaticSegment(patterns []segmentPattern) (string, bool) {
	if len(patterns) == 0 {
		return "", false
	}

	// Absolute routes all start with the same empty segment, so the next segment
	// is the first useful discriminator.
	index := 0
	if len(patterns) > 1 && patterns[0].literal && patterns[0].raw == "" {
		index = 1
	}

	pattern := patterns[index]
	if !pattern.literal || pattern.raw == "" {
		return "", false
	}
	return pattern.raw, true
}

func hasCatchAll(patterns []segmentPattern) bool {
	for i := range patterns {
		if patterns[i].catchAll {
			return true
		}
	}
	return false
}

func sameSegmentPattern(a, b segmentPattern) bool {
	return a.raw == b.raw &&
		a.literal == b.literal &&
		a.catchAll == b.catchAll &&
		a.prefix == b.prefix &&
		a.suffix == b.suffix &&
		a.param == b.param
}

func sortParamEdges[T any](edges []paramEdge[T]) {
	for i := 1; i < len(edges); i++ {
		for j := i; j > 0 && paramEdgeLess(edges[j], edges[j-1]); j-- {
			edges[j], edges[j-1] = edges[j-1], edges[j]
		}
	}
}

func paramEdgeLess[T any](a, b paramEdge[T]) bool {
	aStatic := len(a.pattern.prefix) + len(a.pattern.suffix)
	bStatic := len(b.pattern.prefix) + len(b.pattern.suffix)
	if aStatic != bStatic {
		return aStatic > bStatic
	}
	return len(a.pattern.prefix) > len(b.pattern.prefix)
}

func matchAffixedParamPattern(pattern segmentPattern, segment string) (string, bool) {
	if !strings.HasPrefix(segment, pattern.prefix) || !strings.HasSuffix(segment, pattern.suffix) {
		return "", false
	}

	valueStart := len(pattern.prefix)
	valueEnd := len(segment) - len(pattern.suffix)
	if valueEnd <= valueStart {
		return "", false
	}
	return segment[valueStart:valueEnd], true
}

func matchCatchAllPattern(pattern segmentPattern, rest string) (string, bool) {
	if pattern.prefix == "" {
		return rest, rest != ""
	}

	if !strings.HasPrefix(rest, pattern.prefix) {
		return "", false
	}

	value := rest[len(pattern.prefix):]
	if value == "" {
		return "", false
	}
	return value, true
}

func parseRoute(route string) ([]token, string, error) {
	tokens := make([]token, 0, countRouteTokens(route))
	var normalized strings.Builder
	var literal strings.Builder
	normalized.Grow(len(route) + 8)
	literal.Grow(len(route))
	paramsInSegment := 0
	paramOrdinal := 0

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		text := literal.String()
		tokens = append(tokens, token{kind: tokenLiteral, text: text})
		normalized.WriteByte('L')
		normalized.WriteString(strconv.Itoa(len(text)))
		normalized.WriteByte(':')
		normalized.WriteString(text)
		literal.Reset()
	}

	for i := 0; i < len(route); {
		switch route[i] {
		case '/':
			literal.WriteByte('/')
			paramsInSegment = 0
			i++
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flushLiteral()
			end, err := findParamEnd(route, i+1)
			if err != nil {
				return nil, "", err
			}
			name := unescapeBraces(route[i+1 : end])
			if name == "" {
				return nil, "", ErrInvalidParam
			}
			paramsInSegment++
			if paramsInSegment > 1 {
				return nil, "", ErrInvalidParamSegment
			}
			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return nil, "", ErrInvalidParam
				}
				if end+1 != len(route) {
					return nil, "", ErrInvalidCatchAll
				}
				tokens = append(tokens, token{kind: tokenCatchAll, text: name})
				normalized.WriteByte('C')
				normalized.WriteString(strconv.Itoa(paramOrdinal))
				normalized.WriteByte(';')
			} else {
				tokens = append(tokens, token{kind: tokenParam, text: name})
				normalized.WriteByte('P')
				normalized.WriteString(strconv.Itoa(paramOrdinal))
				normalized.WriteByte(';')
				paramOrdinal++
			}
			i = end + 1
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return nil, "", ErrInvalidParam
		default:
			literal.WriteByte(route[i])
			i++
		}
	}
	flushLiteral()

	return tokens, normalized.String(), nil
}

func countRouteTokens(route string) int {
	count := 1
	for i := 0; i < len(route); i++ {
		switch route[i] {
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				i++
				continue
			}
			count += 2
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				i++
			}
		}
	}
	return count
}

func findParamEnd(route string, start int) (int, error) {
	for i := start; i < len(route); i++ {
		switch route[i] {
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				i++
				continue
			}
			return 0, ErrInvalidParam
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				i++
				continue
			}
			if i == start || route[i-1] == '*' {
				return 0, ErrInvalidParam
			}
			return i, nil
		case '/':
			return 0, ErrInvalidParam
		case '*':
			if i != start {
				return 0, ErrInvalidParam
			}
			if i+1 == len(route) || route[i+1] == '}' {
				return 0, ErrInvalidParam
			}
			continue
		}
	}
	return 0, ErrInvalidParam
}

func unescapeBraces(s string) string {
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
			var b strings.Builder
			b.Grow(len(s) - 1)
			b.WriteString(s[:i])
			for ; i < len(s); i++ {
				if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
					b.WriteByte(s[i])
					i++
					continue
				}
				b.WriteByte(s[i])
			}
			return b.String()
		}
	}
	return s
}

func conflictsEntries[T any](a, b *routeEntry[T]) bool {
	if hasCatchAllPrefixConflict(a, b) || hasCatchAllPrefixConflict(b, a) {
		return true
	}
	return conflictsPatterns(a.patterns, b.patterns)
}

func conflictsPatterns(as, bs []segmentPattern) bool {
	if len(as) != len(bs) {
		return false
	}
	ambiguous := false
	for i := range as {
		if as[i].catchAll || bs[i].catchAll {
			if !segmentMayOverlap(as[i], bs[i]) {
				return false
			}
			if !as[i].literal && !bs[i].literal {
				ambiguous = true
			}
			continue
		}
		if !segmentMayOverlap(as[i], bs[i]) {
			return false
		}
		if segmentConflict(as[i], bs[i]) {
			ambiguous = true
		}
	}
	return ambiguous
}

func hasCatchAllPrefixConflict[T any](a, b *routeEntry[T]) bool {
	if b.captureCount == 0 {
		return false
	}
	for i, pattern := range a.patterns {
		if !pattern.catchAll {
			continue
		}
		if len(b.patterns) <= i {
			return false
		}
		for j := 0; j < i; j++ {
			if !segmentMayOverlap(a.patterns[j], b.patterns[j]) {
				return false
			}
		}
		return catchAllOverlapsSuffix(pattern, b.patterns[i:])
	}
	return false
}

type segmentPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}

func makeSegment(tokens []token) (segmentPattern, string) {
	var s segmentPattern
	var b strings.Builder
	var capture string
	for _, t := range tokens {
		switch t.kind {
		case tokenLiteral:
			b.WriteString(t.text)
			if !s.param && !s.catchAll {
				s.prefix += t.text
			} else {
				s.suffix += t.text
			}
		case tokenParam:
			s.param = true
			capture = t.text
		case tokenCatchAll:
			s.catchAll = true
			capture = t.text
		}
	}
	s.raw = b.String()
	s.literal = !s.param && !s.catchAll
	return s, capture
}

func segmentConflict(a, b segmentPattern) bool {
	if a.literal || b.literal {
		return false
	}
	if !a.param || !b.param {
		return false
	}
	if a.prefix == "" && a.suffix == "" {
		return false
	}
	if b.prefix == "" && b.suffix == "" {
		return false
	}
	return segmentMayOverlap(a, b)
}

func segmentMayOverlap(a, b segmentPattern) bool {
	if a.literal && b.literal {
		return a.raw == b.raw
	}
	if a.literal {
		return literalMatchesSegment(a.raw, b)
	}
	if b.literal {
		return literalMatchesSegment(b.raw, a)
	}
	return compatibleAffixes(a.prefix, b.prefix) && compatibleSuffixes(a.suffix, b.suffix)
}

func literalMatchesSegment(lit string, p segmentPattern) bool {
	if p.catchAll {
		return strings.HasPrefix(lit, p.prefix) && len(lit) > len(p.prefix)
	}
	if !p.param {
		return lit == p.raw
	}
	if !strings.HasPrefix(lit, p.prefix) || !strings.HasSuffix(lit, p.suffix) {
		return false
	}
	return len(lit) > len(p.prefix)+len(p.suffix)
}

func compatibleAffixes(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func compatibleSuffixes(a, b string) bool {
	return strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

func catchAllOverlapsSuffix(catchAll segmentPattern, suffix []segmentPattern) bool {
	if len(suffix) == 0 {
		return false
	}
	first := suffix[0]
	if segmentCanStartLongerThan(first, catchAll.prefix) {
		return true
	}
	return len(suffix) > 1 && segmentCanEqual(first, catchAll.prefix)
}

func segmentCanStartLongerThan(pattern segmentPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix) && len(pattern.raw) > len(prefix)
	}
	return segmentCanStartWith(pattern, prefix)
}

func segmentCanStartWith(pattern segmentPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix)
	}
	return compatibleAffixes(pattern.prefix, prefix)
}

func segmentCanEqual(pattern segmentPattern, value string) bool {
	if pattern.literal {
		return pattern.raw == value
	}
	if pattern.catchAll {
		return strings.HasPrefix(value, pattern.prefix) && len(value) > len(pattern.prefix)
	}
	return literalMatchesSegment(value, pattern)
}
