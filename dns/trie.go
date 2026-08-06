package dns

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
			if pattern.prefix == "" && pattern.suffix == "" {
				if current.plainParam != nil {
					child = current.plainParam.child
				} else {
					child = &labelNode[T]{}
					current.plainParam = &paramEdge[T]{pattern: pattern, child: child}
				}
			} else {
				for j := range current.affixedParams {
					if sameLabelPattern(current.affixedParams[j].pattern, pattern) {
						child = current.affixedParams[j].child
						break
					}
				}
				if child == nil {
					child = &labelNode[T]{}
					current.affixedParams = append(current.affixedParams, paramEdge[T]{
						pattern: pattern,
						child:   child,
					})
					sortParamEdges(current.affixedParams)
				}
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

	if len(n.affixedParams) == 0 {
		if n.plainParam != nil {
			if entry, ok := n.plainParam.child.matchHost(host, next); ok {
				return entry, true
			}
		}
	} else {
		for i := range n.affixedParams {
			pattern := n.affixedParams[i].pattern
			if _, ok := matchAffixedParamPattern(pattern, label); !ok {
				continue
			}
			if entry, ok := n.affixedParams[i].child.matchHost(host, next); ok {
				bestEntry := entry
				for j := i + 1; j < len(n.affixedParams); j++ {
					pattern := n.affixedParams[j].pattern
					if _, ok := matchAffixedParamPattern(pattern, label); !ok {
						continue
					}
					entry, ok := n.affixedParams[j].child.matchHost(host, next)
					if ok && moreSpecificRoute(entry, bestEntry) {
						bestEntry = entry
					}
				}
				if n.plainParam != nil {
					entry, ok := n.plainParam.child.matchHost(host, next)
					if ok && moreSpecificRoute(entry, bestEntry) {
						bestEntry = entry
					}
				}
				return bestEntry, true
			}
		}

		if n.plainParam != nil {
			if entry, ok := n.plainParam.child.matchHost(host, next); ok {
				return entry, true
			}
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

func (n *labelNode[T]) matchStaticHostFolded(host string, end int) (*routeEntry[T], bool) {
	current := n
	for end >= 0 {
		label, next, ok := prevHostLabel(host, end)
		if !ok {
			return nil, false
		}
		current = current.staticChildFolded(label)
		if current == nil {
			return nil, false
		}
		end = next
	}
	return current.value, current.value != nil
}

func (n *labelNode[T]) staticChildFolded(label string) *labelNode[T] {
	if n.staticFoldIndex != nil {
		return n.staticFoldIndex[foldedLabel(label)]
	}
	for i := range n.static {
		if asciiEqualFold(n.static[i].label, label) {
			return n.static[i].child
		}
	}
	return nil
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
	if len(n.static) == staticChildMapThreshold {
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
