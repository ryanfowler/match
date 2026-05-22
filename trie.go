package match

import (
	"strings"
)

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
			if pattern.prefix == "" && pattern.suffix == "" {
				if current.plainParam != nil && sameSegmentPattern(current.plainParam.pattern, pattern) {
					child = current.plainParam.child
				}
				if child == nil {
					child = &segmentNode[T]{}
					current.plainParam = &paramEdge[T]{
						pattern: pattern,
						child:   child,
					}
				}
			} else {
				for j := range current.affixedParams {
					if sameSegmentPattern(current.affixedParams[j].pattern, pattern) {
						child = current.affixedParams[j].child
						break
					}
				}
				if child == nil {
					child = &segmentNode[T]{}
					current.affixedParams = append(current.affixedParams, paramEdge[T]{
						pattern: pattern,
						child:   child,
					})
					sortParamEdges(current.affixedParams)
				}
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

	if len(n.affixedParams) == 0 {
		if n.plainParam != nil && segment != "" {
			if entry, ok := n.plainParam.child.matchPath(path, next); ok {
				return entry, true
			}
		}
	} else {
		for i := range n.affixedParams {
			pattern := n.affixedParams[i].pattern
			if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
				continue
			}
			if entry, ok := n.affixedParams[i].child.matchPath(path, next); ok {
				bestEntry := entry
				for j := i + 1; j < len(n.affixedParams); j++ {
					pattern := n.affixedParams[j].pattern
					if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
						continue
					}
					entry, ok := n.affixedParams[j].child.matchPath(path, next)
					if ok && moreSpecificRoute(entry, bestEntry) {
						bestEntry = entry
					}
				}
				if n.plainParam != nil && segment != "" {
					entry, ok := n.plainParam.child.matchPath(path, next)
					if ok && moreSpecificRoute(entry, bestEntry) {
						bestEntry = entry
					}
				}
				return bestEntry, true
			}
		}

		if n.plainParam != nil && segment != "" {
			if entry, ok := n.plainParam.child.matchPath(path, next); ok {
				return entry, true
			}
		}
	}

	for i := range n.catchAll {
		if _, ok := matchCatchAllPattern(n.catchAll[i].pattern, path[index:]); ok {
			return n.catchAll[i].route, true
		}
	}

	return nil, false
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
	if len(segment) == 1 && segment[0].Kind == tokenLiteral {
		return segment[0].Text, true
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
