package dns

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
	captures := entry.captures
	if len(captures) == 0 {
		return
	}

	if len(captures) > inlineParamCapacity {
		params.Grow(len(captures))
	}

	if len(captures) == 1 {
		capture := captures[0]
		labelIndex := int(capture.index)
		pattern := entry.labels[labelIndex]
		if pattern.catchAll {
			catchEnd := indexBeforeRightLabels(host, len(entry.labels)-1)
			if catchEnd < start {
				return
			}
			if value, ok := matchCatchAllPattern(pattern, host[start:catchEnd]); ok {
				params.Append(capture.name, value)
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
			params.Append(capture.name, value)
			return
		}
		return
	}

	labelStart := start
	labelIndex := 0
	for _, capture := range captures {
		captureLabel := int(capture.index)
		pattern := entry.labels[captureLabel]
		if pattern.catchAll {
			catchEnd := indexBeforeRightLabels(host, len(entry.labels)-1)
			if catchEnd < start {
				return
			}
			if value, ok := matchCatchAllPattern(pattern, host[start:catchEnd]); ok {
				params.Append(capture.name, value)
			}
			labelStart = catchEnd + 1
			labelIndex = captureLabel + 1
			continue
		}

		for labelIndex < captureLabel {
			_, next, ok := nextHostLabel(host, labelStart)
			if !ok || next < 0 {
				return
			}
			labelStart = next
			labelIndex++
		}

		label, next, ok := nextHostLabel(host, labelStart)
		if !ok {
			return
		}
		if value, ok := matchParamCapture(pattern, label); ok {
			params.Append(capture.name, value)
		}
		labelStart = next
		labelIndex = captureLabel + 1
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
