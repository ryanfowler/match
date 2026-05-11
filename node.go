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
	route      string
	normalized string
	tokens     []token
	segments   [][]token
	patterns   []segmentPattern
	dynamic    bool
	value      T
}

type node[T any] struct {
	routes     []routeEntry[T]
	normalized map[string]string
	root       segmentNode[T]
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
	tokens  []token
	child   *segmentNode[T]
}

type catchAllEdge[T any] struct {
	pattern segmentPattern
	token   token
	route   *routeEntry[T]
}

func (n *node[T]) insert(route string, value T) error {
	tokens, normalized, err := parseRoute(route)
	if err != nil {
		return err
	}

	segments := splitTokenSegments(tokens)
	dynamic := hasParamToken(tokens)
	entry := routeEntry[T]{
		route:      unescapeBraces(route),
		normalized: normalized,
		tokens:     tokens,
		segments:   segments,
		dynamic:    dynamic,
		value:      value,
	}
	if dynamic {
		entry.patterns = makeSegmentPatterns(segments)
	}

	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[entry.normalized]; ok {
		return &ConflictError{Route: entry.route, With: existing}
	}

	if entry.dynamic || n.root.conflictsWithCatchAllStatic(entry.segments, 0) {
		for _, existing := range n.routes {
			if !entry.dynamic && !existing.dynamic {
				continue
			}
			if conflictsEntries(existing, entry) {
				return &ConflictError{Route: entry.route, With: existing.route}
			}
		}
	}

	n.normalized[entry.normalized] = entry.route
	n.routes = append(n.routes, entry)
	n.insertTree(&n.routes[len(n.routes)-1])
	return nil
}

func (n *node[T]) match(route string) (T, Params, bool) {
	entry, params, ok := n.root.matchPath(route, 0, Params{})
	if !ok {
		var val T
		return val, Params{}, false
	}
	return entry.value, params, true
}

func (n *node[T]) matchInto(route string, params Params) (T, Params, bool) {
	entry, params, ok := n.root.matchPath(route, 0, params.reset())
	if !ok {
		var val T
		return val, Params{}, false
	}
	return entry.value, params, true
}

func (n *node[T]) insertTree(entry *routeEntry[T]) {
	current := &n.root
	for i, segment := range entry.segments {
		pattern := makeSegment(segment)

		if pattern.catchAll {
			current.catchAll = append(current.catchAll, catchAllEdge[T]{
				pattern: pattern,
				token:   catchAllToken(segment),
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
					tokens:  segment,
					child:   child,
				})
				sortParamEdges(current.params)
			}
			current = child
		}

		if i == len(entry.segments)-1 {
			current.value = entry
		}
	}
}

func (n *segmentNode[T]) matchPath(path string, index int, params Params) (*routeEntry[T], Params, bool) {
	if index < 0 {
		if n.value != nil {
			return n.value, params, true
		}
		return nil, Params{}, false
	}

	segment, next := nextPathSegment(path, index)
	if child := n.staticChild(segment); child != nil {
		if entry, gotParams, ok := child.matchPath(path, next, params); ok {
			return entry, gotParams, true
		}
	}

	for i := range n.params {
		nextParams, ok := matchSegmentTokens(n.params[i].tokens, segment, params)
		if !ok {
			continue
		}
		if entry, gotParams, ok := n.params[i].child.matchPath(path, next, nextParams); ok {
			return entry, gotParams, true
		}
	}

	for i := range n.catchAll {
		if nextParams, ok := matchCatchAll(n.catchAll[i], path[index:], params); ok {
			return n.catchAll[i].route, nextParams, true
		}
	}

	return nil, Params{}, false
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
	if i := strings.IndexByte(path[index:], '/'); i >= 0 {
		return path[index : index+i], index + i + 1
	}
	return path[index:], -1
}

func splitTokenSegments(tokens []token) [][]token {
	var segments [][]token
	var current []token

	flush := func() {
		segment := make([]token, len(current))
		copy(segment, current)
		segments = append(segments, segment)
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

func makeSegmentPatterns(segments [][]token) []segmentPattern {
	patterns := make([]segmentPattern, len(segments))
	for i := range segments {
		patterns[i] = makeSegment(segments[i])
	}
	return patterns
}

func catchAllToken(tokens []token) token {
	for _, t := range tokens {
		if t.kind == tokenCatchAll {
			return t
		}
	}
	return token{}
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

func matchSegmentTokens(tokens []token, segment string, params Params) (Params, bool) {
	return matchSegmentFrom(tokens, 0, segment, 0, params)
}

func matchSegmentFrom(tokens []token, ti int, segment string, si int, params Params) (Params, bool) {
	if ti == len(tokens) {
		if si == len(segment) {
			return params, true
		}
		return Params{}, false
	}

	t := tokens[ti]
	switch t.kind {
	case tokenLiteral:
		if !strings.HasPrefix(segment[si:], t.text) {
			return Params{}, false
		}
		return matchSegmentFrom(tokens, ti+1, segment, si+len(t.text), params)
	case tokenParam:
		for end := len(segment); end > si; end-- {
			next := params.append(t.text, segment[si:end])
			if got, ok := matchSegmentFrom(tokens, ti+1, segment, end, next); ok {
				return got, true
			}
		}
		return Params{}, false
	default:
		return Params{}, false
	}
}

func matchCatchAll[T any](edge catchAllEdge[T], rest string, params Params) (Params, bool) {
	if !strings.HasPrefix(rest, edge.pattern.prefix) {
		return Params{}, false
	}

	value := rest[len(edge.pattern.prefix):]
	if value == "" {
		return Params{}, false
	}
	return params.append(edge.token.text, value), true
}

func parseRoute(route string) ([]token, string, error) {
	var tokens []token
	var normalized strings.Builder
	var literal strings.Builder
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

func findParamEnd(route string, start int) (int, error) {
	for i := start; i < len(route); i++ {
		switch route[i] {
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				i++
				continue
			}
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
		if route[i] == '*' && i != start {
			return 0, ErrInvalidParam
		}
	}
	return 0, ErrInvalidParam
}

func unescapeBraces(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func conflictsEntries[T any](a, b routeEntry[T]) bool {
	if hasCatchAllPrefixConflict(a.tokens, b.tokens) || hasCatchAllPrefixConflict(b.tokens, a.tokens) {
		return true
	}
	return conflictsPatterns(a.patterns, b.patterns)
}

func conflictsPatterns(as, bs []segmentPattern) bool {
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i].catchAll || bs[i].catchAll {
			if !as[i].literal && !bs[i].literal {
				return true
			}
			continue
		}
		if segmentConflict(as[i], bs[i]) {
			return true
		}
		if !segmentMayOverlap(as[i], bs[i]) {
			return false
		}
	}
	return false
}

func hasCatchAllPrefixConflict(a, b []token) bool {
	for i, t := range a {
		if t.kind != tokenCatchAll {
			continue
		}
		if !hasParamToken(b) {
			return false
		}
		prefix := literalPrefix(a[:i])
		return strings.HasPrefix(literalPrefix(b), prefix)
	}
	return false
}

func hasParamToken(tokens []token) bool {
	for _, t := range tokens {
		if t.kind == tokenParam || t.kind == tokenCatchAll {
			return true
		}
	}
	return false
}

func literalPrefix(tokens []token) string {
	var b strings.Builder
	for _, t := range tokens {
		if t.kind != tokenLiteral {
			break
		}
		b.WriteString(t.text)
	}
	return b.String()
}

type segmentPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}

func makeSegment(tokens []token) segmentPattern {
	var s segmentPattern
	var b strings.Builder
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
		case tokenCatchAll:
			s.catchAll = true
		}
	}
	s.raw = b.String()
	s.literal = !s.param && !s.catchAll
	return s
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
