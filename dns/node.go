package dns

import (
	"errors"
	"fmt"
	match "github.com/ryanfowler/match"
	"maps"
	"slices"
	"strconv"
	"strings"
)

const (
	maxHostnameLen      = 253
	maxLabelLen         = 63
	inlineParamCapacity = 4
)

var (
	// ErrInvalidHostname reports malformed dot-separated hostname structure.
	ErrInvalidHostname = errors.New("hostnames must contain non-empty labels")

	// ErrInvalidParamLabel reports a pattern label that contains more than one
	// parameter.
	ErrInvalidParamLabel = errors.New("only one parameter is allowed per hostname label")

	// ErrInvalidParam reports malformed parameter syntax or an invalid
	// parameter name.
	ErrInvalidParam = match.ErrInvalidParam

	// ErrInvalidCatchAll reports a catch-all parameter outside the leftmost
	// pattern label.
	ErrInvalidCatchAll = errors.New("catch-all parameters are only allowed at the start of a hostname pattern")
)

// ConflictError reports a pattern that cannot be inserted because it overlaps
// an already registered pattern.
type ConflictError struct {
	// Pattern is the pattern that failed to insert.
	Pattern string

	// With is the previously registered pattern that conflicts with Pattern.
	With string
}

// Error returns a human-readable description of the pattern conflict.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("insertion failed due to conflict with previously registered pattern: %s", e.With)
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
	pattern             string
	labels              []labelPattern
	captureNames        []string
	captureCount        int
	labelCount          int
	order               int
	firstStaticLabel    string
	singleCaptureLabel  uint32
	hasFirstStaticLabel bool
	hasCatchAll         bool
	value               T
}

type node[T any] struct {
	routes        []*routeEntry[T]
	normalized    map[string]string
	conflictIndex routeConflictIndex[T]
	root          labelNode[T]
}

type routeConflictIndex[T any] struct {
	byLabelCount map[int]*routeConflictBucket[T]
	catchAll     routeConflictBucket[T]
}

type routeConflictBucket[T any] struct {
	all      []*routeEntry[T]
	static   map[string][]*routeEntry[T]
	wildcard []*routeEntry[T]
}

type labelNode[T any] struct {
	static          []staticEdge[T]
	staticIndex     map[string]*labelNode[T]
	staticFoldIndex map[foldedLabelKey]*labelNode[T]
	params          []paramEdge[T]
	catchAll        []catchAllEdge[T]
	value           *routeEntry[T]
}

type staticEdge[T any] struct {
	label string
	child *labelNode[T]
}

type paramEdge[T any] struct {
	pattern labelPattern
	child   *labelNode[T]
}

type catchAllEdge[T any] struct {
	pattern labelPattern
	route   *routeEntry[T]
}

type foldedLabelKey struct {
	n     uint8
	bytes [maxLabelLen]byte
}

type labelPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}

func (n *node[T]) clone() node[T] {
	var cloned node[T]
	entries := make(map[*routeEntry[T]]*routeEntry[T], len(n.routes))
	cloned.routes = cloneRouteEntries(n.routes, entries)
	cloned.normalized = maps.Clone(n.normalized)

	for _, entry := range cloned.routes {
		cloned.conflictIndex.add(entry)
	}

	nodes := make(map[*labelNode[T]]*labelNode[T])
	cloneLabelNodeInto(&n.root, &cloned.root, entries, nodes)

	return cloned
}

func cloneRouteEntries[T any](routes []*routeEntry[T], entries map[*routeEntry[T]]*routeEntry[T]) []*routeEntry[T] {
	if len(routes) == 0 {
		return nil
	}

	clonedRoutes := make([]*routeEntry[T], len(routes))
	for i, entry := range routes {
		clonedEntry := new(routeEntry[T])
		*clonedEntry = *entry
		clonedEntry.labels = slices.Clone(entry.labels)
		clonedEntry.captureNames = slices.Clone(entry.captureNames)
		clonedRoutes[i] = clonedEntry
		entries[entry] = clonedEntry
	}
	return clonedRoutes
}

func cloneLabelNodeInto[T any](src, dst *labelNode[T], entries map[*routeEntry[T]]*routeEntry[T], nodes map[*labelNode[T]]*labelNode[T]) {
	nodes[src] = dst
	if src.value != nil {
		dst.value = entries[src.value]
	}

	if len(src.static) != 0 {
		dst.static = slices.Clone(src.static)
		for i := range src.static {
			child := new(labelNode[T])
			cloneLabelNodeInto(src.static[i].child, child, entries, nodes)
			dst.static[i].child = child
		}
	}
	if src.staticIndex != nil {
		dst.staticIndex = maps.Clone(src.staticIndex)
		for label, child := range dst.staticIndex {
			dst.staticIndex[label] = nodes[child]
		}
	}
	if src.staticFoldIndex != nil {
		dst.staticFoldIndex = maps.Clone(src.staticFoldIndex)
		for label, child := range dst.staticFoldIndex {
			dst.staticFoldIndex[label] = nodes[child]
		}
	}

	if len(src.params) != 0 {
		dst.params = slices.Clone(src.params)
		for i := range src.params {
			child := new(labelNode[T])
			cloneLabelNodeInto(src.params[i].child, child, entries, nodes)
			dst.params[i].child = child
		}
	}

	if len(src.catchAll) != 0 {
		dst.catchAll = slices.Clone(src.catchAll)
		for i := range src.catchAll {
			dst.catchAll[i].route = entries[src.catchAll[i].route]
		}
	}
}

func (n *node[T]) insert(pattern string, value T) error {
	entry, normalized, err := makeRouteEntry(pattern, value, len(n.routes))
	if err != nil {
		return err
	}

	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[normalized]; ok {
		return &ConflictError{Pattern: entry.pattern, With: existing}
	}

	if entry.captureCount != 0 {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Pattern: entry.pattern, With: existing.pattern}
		}
	}

	n.normalized[normalized] = entry.pattern
	n.routes = append(n.routes, entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	return nil
}

func makeRouteEntry[T any](pattern string, value T, order int) (*routeEntry[T], string, error) {
	labels, captureNames, singleCaptureLabel, captureCount, canonicalPattern, err := parsePattern(pattern)
	if err != nil {
		return nil, "", err
	}

	firstStaticLabel, hasFirstStaticLabel := firstDefinitelyStaticLabel(labels)
	entry := &routeEntry[T]{
		pattern:             canonicalPattern,
		labels:              labels,
		captureNames:        captureNames,
		singleCaptureLabel:  uint32(singleCaptureLabel),
		captureCount:        captureCount,
		labelCount:          len(labels),
		order:               order,
		firstStaticLabel:    firstStaticLabel,
		hasFirstStaticLabel: hasFirstStaticLabel,
		hasCatchAll:         hasCatchAll(labels),
		value:               value,
	}

	return entry, normalizedLabels(labels), nil
}

func (i *routeConflictIndex[T]) add(entry *routeEntry[T]) {
	if entry.captureCount == 0 {
		return
	}
	if i.byLabelCount == nil {
		i.byLabelCount = make(map[int]*routeConflictBucket[T])
	}
	bucket := i.byLabelCount[entry.labelCount]
	if bucket == nil {
		bucket = &routeConflictBucket[T]{}
		i.byLabelCount[entry.labelCount] = bucket
	}
	bucket.add(entry)
	if entry.hasCatchAll {
		i.catchAll.add(entry)
	}
}

func (b *routeConflictBucket[T]) add(entry *routeEntry[T]) {
	b.all = append(b.all, entry)
	if !entry.hasFirstStaticLabel {
		b.wildcard = append(b.wildcard, entry)
		return
	}
	if b.static == nil {
		b.static = make(map[string][]*routeEntry[T])
	}
	b.static[entry.firstStaticLabel] = append(b.static[entry.firstStaticLabel], entry)
}

func (i *routeConflictIndex[T]) findConflict(entry *routeEntry[T]) *routeEntry[T] {
	var best *routeEntry[T]
	if bucket := i.byLabelCount[entry.labelCount]; bucket != nil {
		best = earlierConflict(best, bucket.findConflict(entry, 0))
	}

	if entry.hasCatchAll {
		for labelCount, bucket := range i.byLabelCount {
			if labelCount == entry.labelCount {
				continue
			}
			best = earlierConflict(best, bucket.findConflict(entry, 0))
		}
		return best
	}

	return earlierConflict(best, i.catchAll.findConflict(entry, entry.labelCount))
}

func (b *routeConflictBucket[T]) findConflict(entry *routeEntry[T], skipLabelCount int) *routeEntry[T] {
	if b == nil {
		return nil
	}
	if entry.hasFirstStaticLabel {
		static := findConflictInRoutes(b.static[entry.firstStaticLabel], entry, skipLabelCount)
		wildcard := findConflictInRoutes(b.wildcard, entry, skipLabelCount)
		return earlierConflict(static, wildcard)
	}
	return findConflictInRoutes(b.all, entry, skipLabelCount)
}

func findConflictInRoutes[T any](routes []*routeEntry[T], entry *routeEntry[T], skipLabelCount int) *routeEntry[T] {
	for _, existing := range routes {
		if skipLabelCount != 0 && existing.labelCount == skipLabelCount {
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

func (n *node[T]) match(hostname string) (T, Params, bool) {
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		var val T
		return val, Params{}, false
	}

	entry, ok := n.root.matchHost(host, len(host))
	if !ok {
		var val T
		return val, Params{}, false
	}
	var params Params
	collectParams(entry, host, 0, &params)
	return entry.value, params, true
}

func (n *node[T]) matchInto(hostname string, params *Params) (T, bool) {
	params.Reset()
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		var val T
		return val, false
	}

	entry, ok := n.root.matchHost(host, len(host))
	if !ok {
		var val T
		return val, false
	}
	collectParams(entry, host, 0, params)
	return entry.value, true
}

func (n *node[T]) matchSuffix(hostname string) (SuffixMatch[T], bool) {
	host, ok := validHostname(hostname)
	if !ok {
		return SuffixMatch[T]{}, false
	}

	match, ok := n.root.matchSuffixHost(host, len(host), 0)
	if !ok {
		return SuffixMatch[T]{}, false
	}
	var params Params
	collectParams(match.entry, host, suffixStart(match.prefixEnd), &params)
	return match.suffix(host, params), true
}

func (n *node[T]) matchSuffixInto(hostname string, params *Params) (SuffixMatch[T], bool) {
	params.Reset()
	host, ok := validHostname(hostname)
	if !ok {
		return SuffixMatch[T]{Params: *params}, false
	}

	match, ok := n.root.matchSuffixHost(host, len(host), 0)
	if !ok {
		return SuffixMatch[T]{Params: *params}, false
	}
	collectParams(match.entry, host, suffixStart(match.prefixEnd), params)
	return match.suffix(host, *params), true
}

func (n *node[T]) insertTree(entry *routeEntry[T]) {
	current := &n.root
	for i := len(entry.labels) - 1; i >= 0; i-- {
		pattern := entry.labels[i]
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
				child = &labelNode[T]{}
				current.addStaticChild(pattern.raw, child)
			}
			current = child
		} else {
			var child *labelNode[T]
			for j := range current.params {
				if sameLabelPattern(current.params[j].pattern, pattern) {
					child = current.params[j].child
					break
				}
			}
			if child == nil {
				child = &labelNode[T]{}
				current.params = append(current.params, paramEdge[T]{
					pattern: pattern,
					child:   child,
				})
				sortParamEdges(current.params)
			}
			current = child
		}

		if i == 0 {
			current.value = entry
		}
	}
}

func (n *labelNode[T]) matchHost(host string, end int) (*routeEntry[T], bool) {
	if end < 0 {
		if n.value != nil {
			return n.value, true
		}
		return nil, false
	}

	label, next, ok := prevHostLabel(host, end)
	if !ok {
		return nil, false
	}

	if child := n.staticChild(label); child != nil {
		if entry, ok := child.matchHost(host, next); ok {
			return entry, true
		}
	}

	for i := range n.params {
		pattern := n.params[i].pattern
		if pattern.prefix == "" && pattern.suffix == "" {
			if label == "" {
				continue
			}
		} else if _, ok := matchAffixedParamPattern(pattern, label); !ok {
			continue
		}
		if entry, ok := n.params[i].child.matchHost(host, next); ok {
			bestEntry := entry
			for j := i + 1; j < len(n.params); j++ {
				pattern := n.params[j].pattern
				if pattern.prefix == "" && pattern.suffix == "" {
					if label == "" {
						continue
					}
				} else if _, ok := matchAffixedParamPattern(pattern, label); !ok {
					continue
				}
				entry, ok := n.params[j].child.matchHost(host, next)
				if ok && moreSpecificRoute(entry, bestEntry) {
					bestEntry = entry
				}
			}
			return bestEntry, true
		}
	}

	if len(n.catchAll) != 0 {
		remaining := host[:end]
		if validHostnameLabels(remaining) {
			for i := range n.catchAll {
				if _, ok := matchCatchAllPattern(n.catchAll[i].pattern, remaining); ok {
					return n.catchAll[i].route, true
				}
			}
		}
	}

	return nil, false
}

type suffixRouteMatch[T any] struct {
	entry     *routeEntry[T]
	prefixEnd int
	consumed  int
}

func (m suffixRouteMatch[T]) suffix(host string, params Params) SuffixMatch[T] {
	return SuffixMatch[T]{
		Value:  m.entry.value,
		Params: params,
		Prefix: hostnamePrefix(host, m.prefixEnd),
	}
}

func (n *labelNode[T]) matchSuffixHost(host string, end, consumed int) (suffixRouteMatch[T], bool) {
	var best suffixRouteMatch[T]
	if n.value != nil {
		best = suffixRouteMatch[T]{
			entry:     n.value,
			prefixEnd: end,
			consumed:  consumed,
		}
	}

	if end >= 0 {
		label, next, ok := prevHostLabel(host, end)
		if !ok {
			return best, best.entry != nil
		}

		if child := n.staticChild(label); child != nil {
			if candidate, ok := child.matchSuffixHost(host, next, consumed+1); ok {
				best = betterSuffixMatch(best, candidate)
			}
		}

		for i := range n.params {
			pattern := n.params[i].pattern
			if pattern.prefix == "" && pattern.suffix == "" {
				if label == "" {
					continue
				}
			} else if _, ok := matchAffixedParamPattern(pattern, label); !ok {
				continue
			}
			if candidate, ok := n.params[i].child.matchSuffixHost(host, next, consumed+1); ok {
				best = betterSuffixMatch(best, candidate)
			}
		}

		remaining := host[:end]
		for i := range n.catchAll {
			if _, ok := matchCatchAllPattern(n.catchAll[i].pattern, remaining); ok {
				candidate := suffixRouteMatch[T]{
					entry:     n.catchAll[i].route,
					prefixEnd: -1,
					consumed:  consumed + countHostnameLabels(remaining),
				}
				best = betterSuffixMatch(best, candidate)
			}
		}
	}

	return best, best.entry != nil
}

func betterSuffixMatch[T any](best, candidate suffixRouteMatch[T]) suffixRouteMatch[T] {
	if best.entry == nil || candidate.consumed > best.consumed {
		return candidate
	}
	if candidate.consumed == best.consumed && moreSpecificRoute(candidate.entry, best.entry) {
		return candidate
	}
	return best
}

func collectParams[T any](entry *routeEntry[T], host string, start int, params *Params) {
	if entry.captureCount == 0 {
		return
	}

	if entry.captureCount > inlineParamCapacity {
		params.Grow(entry.captureCount)
	}
	if entry.captureCount == 1 {
		labelIndex := int(entry.singleCaptureLabel)
		pattern := entry.labels[labelIndex]
		name := entry.captureNames[labelIndex]
		if pattern.catchAll {
			catchEnd := indexBeforeRightLabels(host, len(entry.labels)-1)
			if value, ok := matchCatchAllPattern(pattern, host[start:catchEnd]); ok {
				params.Append(name, value)
				return
			}
			return
		}

		labelStart := start
		for i := 0; i < labelIndex; i++ {
			_, next, ok := nextHostLabel(host, labelStart)
			if !ok || next < 0 {
				return
			}
			labelStart = next
		}
		label, _, ok := nextHostLabel(host, labelStart)
		if !ok {
			return
		}
		if value, ok := matchParamCapture(pattern, label); ok {
			params.Append(name, value)
			return
		}
		return
	}

	if entry.labels[0].catchAll {
		catchEnd := indexBeforeRightLabels(host, len(entry.labels)-1)
		pattern := entry.labels[0]
		value, ok := matchCatchAllPattern(pattern, host[start:catchEnd])
		if ok {
			params.Append(entry.captureNames[0], value)
		}

		if len(entry.labels) == 1 {
			return
		}
		start = catchEnd + 1
		for i := 1; i < len(entry.labels); i++ {
			label, next, ok := nextHostLabel(host, start)
			if !ok {
				return
			}
			if entry.labels[i].param {
				if value, ok := matchParamCapture(entry.labels[i], label); ok {
					params.Append(entry.captureNames[i], value)
				}
			}
			start = next
		}
		return
	}

	for i := range entry.labels {
		label, next, ok := nextHostLabel(host, start)
		if !ok {
			return
		}
		if entry.labels[i].param {
			if value, ok := matchParamCapture(entry.labels[i], label); ok {
				params.Append(entry.captureNames[i], value)
			}
		}
		start = next
	}
}

func matchParamCapture(pattern labelPattern, label string) (string, bool) {
	if pattern.prefix == "" && pattern.suffix == "" {
		return label, label != ""
	}
	return matchAffixedParamPattern(pattern, label)
}

func moreSpecificRoute[T any](a, b *routeEntry[T]) bool {
	for ai, bi := len(a.labels)-1, len(b.labels)-1; ai >= 0 && bi >= 0; ai, bi = ai-1, bi-1 {
		ap := a.labels[ai]
		bp := b.labels[bi]
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
	return len(a.labels) > len(b.labels)
}

func (n *labelNode[T]) staticChild(label string) *labelNode[T] {
	if n.staticIndex != nil {
		if child := n.staticIndex[label]; child != nil {
			return child
		}
		if asciiLower(label) {
			return nil
		}
		if n.staticFoldIndex != nil {
			key := foldedLabel(label)
			return n.staticFoldIndex[key]
		}
		return nil
	}
	for i := range n.static {
		if n.static[i].label == label {
			return n.static[i].child
		}
	}
	for i := range n.static {
		if asciiEqualFold(n.static[i].label, label) {
			return n.static[i].child
		}
	}
	return nil
}

func (n *labelNode[T]) addStaticChild(label string, child *labelNode[T]) {
	n.static = append(n.static, staticEdge[T]{label: label, child: child})
	if len(n.static) == 9 {
		n.staticIndex = make(map[string]*labelNode[T], len(n.static))
		n.staticFoldIndex = make(map[foldedLabelKey]*labelNode[T], len(n.static))
		for i := range n.static {
			n.staticIndex[n.static[i].label] = n.static[i].child
			n.staticFoldIndex[foldedLabel(n.static[i].label)] = n.static[i].child
		}
		return
	}
	if n.staticIndex != nil {
		n.staticIndex[label] = child
		n.staticFoldIndex[foldedLabel(label)] = child
	}
}

func foldedLabel(label string) foldedLabelKey {
	var key foldedLabelKey
	key.n = uint8(len(label))
	for i := 0; i < len(label); i++ {
		key.bytes[i] = lowerASCIIByte(label[i])
	}
	return key
}

func prevHostLabel(host string, end int) (string, int, bool) {
	if end <= 0 {
		return "", 0, false
	}
	if host[end-1] == '.' {
		return "", 0, false
	}
	for i := end - 1; i >= 0; i-- {
		if host[i] == '.' {
			if i == end-1 || end-i-1 > maxLabelLen {
				return "", 0, false
			}
			return host[i+1 : end], i, true
		}
	}
	if end > maxLabelLen {
		return "", 0, false
	}
	return host[:end], -1, true
}

func nextHostLabel(host string, start int) (string, int, bool) {
	if start < 0 || start >= len(host) || host[start] == '.' {
		return "", 0, false
	}
	if i := strings.IndexByte(host[start:], '.'); i >= 0 {
		return host[start : start+i], start + i + 1, true
	}
	return host[start:], -1, true
}

func validHostname(host string) (string, bool) {
	host, ok := hostnameWithinBounds(host)
	if !ok {
		return "", false
	}
	return host, validHostnameLabels(host)
}

func hostnameWithinBounds(host string) (string, bool) {
	host = trimRootDot(host)
	if host == "" || len(host) > maxHostnameLen {
		return "", false
	}
	return host, true
}

func validHostnameLabels(host string) bool {
	labelLen := 0
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			if labelLen == 0 || labelLen > maxLabelLen {
				return false
			}
			labelLen = 0
			continue
		}
		labelLen++
	}
	if labelLen == 0 || labelLen > maxLabelLen {
		return false
	}
	return true
}

func hostnamePrefix(host string, end int) string {
	if end <= 0 {
		return ""
	}
	return host[:end]
}

func suffixStart(prefixEnd int) int {
	if prefixEnd < 0 {
		return 0
	}
	return prefixEnd + 1
}

func countHostnameLabels(host string) int {
	if host == "" {
		return 0
	}
	count := 1
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			count++
		}
	}
	return count
}

func indexBeforeRightLabels(host string, labels int) int {
	end := len(host)
	for i := 0; i < labels; i++ {
		_, next, ok := prevHostLabel(host, end)
		if !ok {
			return -1
		}
		end = next
	}
	return end
}

func trimRootDot(host string) string {
	if host != "" && host[len(host)-1] == '.' {
		return host[:len(host)-1]
	}
	return host
}

func parsePattern(pattern string) ([]labelPattern, []string, int, int, string, error) {
	canonicalPattern := trimRootDot(pattern)
	if canonicalPattern == "" {
		return nil, nil, 0, 0, "", ErrInvalidHostname
	}

	tokens, err := parsePatternTokens(canonicalPattern)
	if err != nil {
		return nil, nil, 0, 0, "", err
	}

	labelTokens := splitTokenLabels(tokens)
	labels := make([]labelPattern, len(labelTokens))
	var captureNames []string
	singleCaptureLabel := -1
	captureCount := 0

	for i := range labelTokens {
		if len(labelTokens[i]) == 0 {
			return nil, nil, 0, 0, "", ErrInvalidHostname
		}

		var capture string
		labels[i], capture = makeLabel(labelTokens[i])
		if err := validateLabelPattern(labels[i]); err != nil {
			return nil, nil, 0, 0, "", err
		}
		if labels[i].catchAll && i != 0 {
			return nil, nil, 0, 0, "", ErrInvalidCatchAll
		}
		if capture != "" {
			if captureNames == nil {
				captureNames = make([]string, len(labelTokens))
			}
			captureNames[i] = capture
			if captureCount == 0 {
				singleCaptureLabel = i
			}
			captureCount++
		}
	}

	if minHostnameLength(labels) > maxHostnameLen {
		return nil, nil, 0, 0, "", ErrInvalidHostname
	}

	return labels, captureNames, singleCaptureLabel, captureCount, unescapeBraces(canonicalPattern), nil
}

func parsePatternTokens(pattern string) ([]token, error) {
	tokens := make([]token, 0, countPatternTokens(pattern))
	var literal strings.Builder
	literal.Grow(len(pattern))
	paramsInLabel := 0
	labelIndex := 0

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		tokens = append(tokens, token{kind: tokenLiteral, text: literal.String()})
		literal.Reset()
	}

	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '.':
			literal.WriteByte('.')
			paramsInLabel = 0
			labelIndex++
			i++
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flushLiteral()
			end, err := findParamEnd(pattern, i+1)
			if err != nil {
				return nil, err
			}
			name := unescapeBraces(pattern[i+1 : end])
			if name == "" {
				return nil, ErrInvalidParam
			}
			paramsInLabel++
			if paramsInLabel > 1 {
				return nil, ErrInvalidParamLabel
			}
			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return nil, ErrInvalidParam
				}
				if labelIndex != 0 || (end+1 < len(pattern) && pattern[end+1] != '.') {
					return nil, ErrInvalidCatchAll
				}
				tokens = append(tokens, token{kind: tokenCatchAll, text: name})
			} else {
				tokens = append(tokens, token{kind: tokenParam, text: name})
			}
			i = end + 1
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return nil, ErrInvalidParam
		default:
			literal.WriteByte(pattern[i])
			i++
		}
	}
	flushLiteral()

	return tokens, nil
}

func countPatternTokens(pattern string) int {
	count := 1
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
			count += 2
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
			}
		}
	}
	return count
}

func findParamEnd(pattern string, start int) (int, error) {
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
			return 0, ErrInvalidParam
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
				continue
			}
			if i == start || pattern[i-1] == '*' {
				return 0, ErrInvalidParam
			}
			return i, nil
		case '.':
			return 0, ErrInvalidParam
		case '*':
			if i != start {
				return 0, ErrInvalidParam
			}
			if i+1 == len(pattern) || pattern[i+1] == '}' {
				return 0, ErrInvalidParam
			}
			continue
		}
	}
	return 0, ErrInvalidParam
}

func splitTokenLabels(tokens []token) [][]token {
	labels := make([][]token, 0, countTokenLabels(tokens))
	var current []token

	flush := func() {
		labels = append(labels, current)
		current = nil
	}

	for _, t := range tokens {
		if t.kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.text); i++ {
			if t.text[i] != '.' {
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
	return labels
}

func countTokenLabels(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.kind == tokenLiteral {
			count += strings.Count(t.text, ".")
		}
	}
	return count
}

func makeLabel(tokens []token) (labelPattern, string) {
	var p labelPattern
	var b strings.Builder
	var capture string
	for _, t := range tokens {
		switch t.kind {
		case tokenLiteral:
			text := lowerASCII(t.text)
			b.WriteString(text)
			if !p.param && !p.catchAll {
				p.prefix += text
			} else {
				p.suffix += text
			}
		case tokenParam:
			p.param = true
			capture = t.text
		case tokenCatchAll:
			p.catchAll = true
			capture = t.text
		}
	}
	p.raw = b.String()
	p.literal = !p.param && !p.catchAll
	return p, capture
}

func validateLabelPattern(pattern labelPattern) error {
	switch {
	case pattern.literal:
		if pattern.raw == "" || len(pattern.raw) > maxLabelLen {
			return ErrInvalidHostname
		}
	case pattern.catchAll:
		if len(pattern.prefix) >= maxLabelLen || pattern.suffix != "" {
			return ErrInvalidHostname
		}
	case pattern.param:
		if len(pattern.prefix)+len(pattern.suffix) >= maxLabelLen {
			return ErrInvalidHostname
		}
	}
	return nil
}

func minHostnameLength(labels []labelPattern) int {
	if len(labels) == 0 {
		return 0
	}

	length := len(labels) - 1
	for i := range labels {
		p := labels[i]
		if p.literal {
			length += len(p.raw)
			continue
		}
		length += len(p.prefix) + len(p.suffix) + 1
	}
	return length
}

func normalizedLabels(labels []labelPattern) string {
	var b strings.Builder
	for i := range labels {
		b.WriteByte('.')
		p := labels[i]
		if p.literal {
			writeNormalizedPart(&b, 'L', p.raw)
			continue
		}
		if p.catchAll {
			writeNormalizedPart(&b, 'C', p.prefix)
			continue
		}
		writeNormalizedPart(&b, 'P', p.prefix)
		writeNormalizedPart(&b, 'S', p.suffix)
	}
	return b.String()
}

func writeNormalizedPart(b *strings.Builder, kind byte, text string) {
	b.WriteByte(kind)
	b.WriteString(strconv.Itoa(len(text)))
	b.WriteByte(':')
	b.WriteString(text)
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

func hasCatchAll(labels []labelPattern) bool {
	for i := range labels {
		if labels[i].catchAll {
			return true
		}
	}
	return false
}

func firstDefinitelyStaticLabel(labels []labelPattern) (string, bool) {
	if len(labels) == 0 || !labels[0].literal {
		return "", false
	}
	return labels[0].raw, true
}

func sameLabelPattern(a, b labelPattern) bool {
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

func matchAffixedParamPattern(pattern labelPattern, label string) (string, bool) {
	if !asciiHasPrefixFold(label, pattern.prefix) || !asciiHasSuffixFold(label, pattern.suffix) {
		return "", false
	}

	valueStart := len(pattern.prefix)
	valueEnd := len(label) - len(pattern.suffix)
	if valueEnd <= valueStart {
		return "", false
	}
	return label[valueStart:valueEnd], true
}

func matchCatchAllPattern(pattern labelPattern, remaining string) (string, bool) {
	if remaining == "" {
		return "", false
	}
	if pattern.prefix == "" {
		return remaining, true
	}
	if !asciiHasPrefixFold(remaining, pattern.prefix) {
		return "", false
	}
	value := remaining[len(pattern.prefix):]
	if value == "" || value[0] == '.' {
		return "", false
	}
	return value, true
}

func conflictsEntries[T any](a, b *routeEntry[T]) bool {
	if a.captureCount == 0 || b.captureCount == 0 {
		return false
	}
	if a.hasCatchAll || b.hasCatchAll {
		return catchAllConflicts(a, b)
	}
	return conflictsLabels(a.labels, b.labels)
}

func conflictsLabels(as, bs []labelPattern) bool {
	if len(as) != len(bs) {
		return false
	}
	ambiguous := false
	for i := range as {
		if !labelMayOverlap(as[i], bs[i]) {
			return false
		}
		if labelConflict(as[i], bs[i]) {
			ambiguous = true
		}
	}
	return ambiguous
}

func catchAllConflicts[T any](a, b *routeEntry[T]) bool {
	switch {
	case a.hasCatchAll && b.hasCatchAll:
		return catchAllEntriesMayOverlap(a, b)
	case a.hasCatchAll:
		return catchAllFiniteConflict(a, b)
	default:
		return catchAllFiniteConflict(b, a)
	}
}

func catchAllEntriesMayOverlap[T any](a, b *routeEntry[T]) bool {
	if !compatiblePrefixes(a.labels[0].prefix, b.labels[0].prefix) {
		return false
	}
	return suffixesMayOverlap(a.labels[1:], b.labels[1:])
}

func catchAllFiniteConflict[T any](catchAll, finite *routeEntry[T]) bool {
	suffix := catchAll.labels[1:]
	if len(finite.labels) <= len(suffix) {
		return false
	}

	start := len(finite.labels) - len(suffix)
	for i := range suffix {
		if !labelMayOverlap(suffix[i], finite.labels[start+i]) {
			return false
		}
	}
	return catchAllOverlapsLeading(catchAll.labels[0], finite.labels[:start])
}

func suffixesMayOverlap(as, bs []labelPattern) bool {
	limit := len(as)
	if len(bs) < limit {
		limit = len(bs)
	}
	for i := 0; i < limit; i++ {
		if !labelMayOverlap(as[len(as)-1-i], bs[len(bs)-1-i]) {
			return false
		}
	}
	return true
}

func catchAllOverlapsLeading(catchAll labelPattern, leading []labelPattern) bool {
	if len(leading) == 0 {
		return false
	}
	return labelCanStartLongerThan(leading[0], catchAll.prefix)
}

func labelConflict(a, b labelPattern) bool {
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
	return labelMayOverlap(a, b)
}

func labelMayOverlap(a, b labelPattern) bool {
	if a.literal && b.literal {
		return a.raw == b.raw
	}
	if a.literal {
		return literalMatchesLabel(a.raw, b)
	}
	if b.literal {
		return literalMatchesLabel(b.raw, a)
	}
	return compatiblePrefixes(a.prefix, b.prefix) && compatibleSuffixes(a.suffix, b.suffix)
}

func literalMatchesLabel(lit string, pattern labelPattern) bool {
	if pattern.catchAll {
		return strings.HasPrefix(lit, pattern.prefix) && len(lit) > len(pattern.prefix)
	}
	if !pattern.param {
		return lit == pattern.raw
	}
	if !strings.HasPrefix(lit, pattern.prefix) || !strings.HasSuffix(lit, pattern.suffix) {
		return false
	}
	return len(lit) > len(pattern.prefix)+len(pattern.suffix)
}

func compatiblePrefixes(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func compatibleSuffixes(a, b string) bool {
	return strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

func labelCanStartLongerThan(pattern labelPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix) && len(pattern.raw) > len(prefix)
	}
	return labelCanStartWith(pattern, prefix)
}

func labelCanStartWith(pattern labelPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix)
	}
	return compatiblePrefixes(pattern.prefix, prefix)
}

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			var b strings.Builder
			b.Grow(len(s))
			b.WriteString(s[:i])
			for ; i < len(s); i++ {
				c := s[i]
				if 'A' <= c && c <= 'Z' {
					c += 'a' - 'A'
				}
				b.WriteByte(c)
			}
			return b.String()
		}
	}
	return s
}

func asciiLower(s string) bool {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			return false
		}
	}
	return true
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCIIByte(a[i]) != lowerASCIIByte(b[i]) {
			return false
		}
	}
	return true
}

func asciiHasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && asciiEqualFold(s[:len(prefix)], prefix)
}

func asciiHasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && asciiEqualFold(s[len(s)-len(suffix):], suffix)
}

func lowerASCIIByte(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
