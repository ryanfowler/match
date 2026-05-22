package match

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

		if len(n.affixedParams) == 0 {
			if n.plainParam != nil && segment != "" {
				if candidate, ok := n.plainParam.child.matchPrefixPath(path, next, false); ok {
					best = betterPrefixMatch(best, candidate)
				}
			}
		} else {
			for i := range n.affixedParams {
				pattern := n.affixedParams[i].pattern
				if _, ok := matchAffixedParamPattern(pattern, segment); !ok {
					continue
				}
				if candidate, ok := n.affixedParams[i].child.matchPrefixPath(path, next, false); ok {
					best = betterPrefixMatch(best, candidate)
				}
			}

			if n.plainParam != nil && segment != "" {
				if candidate, ok := n.plainParam.child.matchPrefixPath(path, next, false); ok {
					best = betterPrefixMatch(best, candidate)
				}
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

func collectParams[T any](entry *routeEntry[T], path string, params *Params) {
	if entry.captureCount == 0 {
		return
	}

	if entry.captureCount > inlineParams {
		params.Grow(entry.captureCount)
	}

	if entry.captureCount == 1 {
		index := 0
		captureSegment := int(entry.singleCaptureSegment)
		for segmentIndex := 0; segmentIndex < captureSegment; segmentIndex++ {
			_, index = nextPathSegment(path, index)
			if index < 0 {
				return
			}
		}

		pattern := entry.patterns[captureSegment]
		name := entry.captureNames[captureSegment]
		if pattern.catchAll {
			rest := path[index:]
			if pattern.prefix == "" {
				if rest != "" {
					params.Append(name, rest)
					return
				}
			} else if value, ok := matchCatchAllPattern(pattern, rest); ok {
				params.Append(name, value)
				return
			}
			return
		}

		pathSegment, _ := nextPathSegment(path, index)
		if pattern.prefix == "" && pattern.suffix == "" {
			if pathSegment != "" {
				params.Append(name, pathSegment)
				return
			}
		} else if value, ok := matchAffixedParamPattern(pattern, pathSegment); ok {
			params.Append(name, value)
			return
		}
		return
	}

	index := 0
	for i := range entry.patterns {
		pattern := entry.patterns[i]
		if pattern.catchAll {
			rest := path[index:]
			if pattern.prefix == "" {
				if rest != "" {
					params.Append(entry.captureNames[i], rest)
				}
			} else if value, ok := matchCatchAllPattern(pattern, rest); ok {
				params.Append(entry.captureNames[i], value)
			}
			return
		}

		pathSegment, next := nextPathSegment(path, index)
		if pattern.param {
			if pattern.prefix == "" && pattern.suffix == "" {
				if pathSegment != "" {
					params.Append(entry.captureNames[i], pathSegment)
				}
			} else if value, ok := matchAffixedParamPattern(pattern, pathSegment); ok {
				params.Append(entry.captureNames[i], value)
			}
		}
		index = next
		if index < 0 {
			return
		}
	}
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
