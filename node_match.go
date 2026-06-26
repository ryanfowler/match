package match

func (n *node[T]) match(route string) (T, Params, bool) {
	if entry, ok := n.matchExactStatic(route); ok {
		return entry.value, Params{}, true
	}
	if !n.hasComplexParams {
		if !n.hasSimpleDynamic {
			var val T
			return val, Params{}, false
		}
		var params Params
		if entry, ok := n.fastRoot.match(route, 0, &params); ok {
			applySimpleParamNames(entry, &params)
			return entry.value, params, true
		}
		var val T
		return val, Params{}, false
	}
	root, index, _ := n.matchRoot(route)
	entry, ok := root.matchPath(route, index)
	if !ok {
		var val T
		return val, Params{}, false
	}
	var params Params
	collectParams(entry, route, &params)
	return entry.value, params, true
}

func (n *node[T]) matchInto(route string, params *Params) (T, bool) {
	params.Reset()
	if entry, ok := n.matchExactStatic(route); ok {
		return entry.value, true
	}
	if !n.hasComplexParams {
		if !n.hasSimpleDynamic {
			var val T
			return val, false
		}
		if entry, ok := n.fastRoot.match(route, 0, params); ok {
			applySimpleParamNames(entry, params)
			return entry.value, true
		}
		var val T
		return val, false
	}
	root, index, _ := n.matchRoot(route)
	entry, ok := root.matchPath(route, index)
	if !ok {
		var val T
		return val, false
	}
	collectParams(entry, route, params)
	return entry.value, true
}

func (n *node[T]) matchExactStatic(path string) (*routeEntry[T], bool) {
	if len(n.exactStatic) == 0 || len(path) > n.maxExactStaticPathLen {
		return nil, false
	}
	entry, ok := n.exactStatic[path]
	return entry, ok
}

func (n *node[T]) matchPrefix(path string) (PrefixMatch[T], bool) {
	if n.hasDynamic {
		match, ok := n.matchPrefixRoute(path)
		if !ok {
			return PrefixMatch[T]{}, false
		}
		var params Params
		collectParams(match.entry, path, &params)
		return match.prefix(path, params), true
	}

	if match, ok := n.matchStaticPrefixRoute(path); ok {
		return match.prefix(path, Params{}), true
	}
	return PrefixMatch[T]{}, false
}

func (n *node[T]) matchPrefixInto(path string, params *Params) (PrefixMatch[T], bool) {
	params.Reset()
	match, ok := n.matchPrefixRoute(path)
	if !ok {
		return PrefixMatch[T]{Params: *params}, false
	}
	collectParams(match.entry, path, params)
	return match.prefix(path, *params), true
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

func (n *node[T]) matchStaticPrefixRoute(path string) (prefixMatch[T], bool) {
	if len(n.exactStatic) == 0 {
		return prefixMatch[T]{}, false
	}

	if entry, ok := n.matchExactStatic(path); ok {
		return prefixMatch[T]{
			entry:     entry,
			restIndex: -1,
			consumed:  len(path) + 1,
		}, true
	}

	for i := len(path) - 1; i >= 0; i-- {
		if path[i] != '/' {
			continue
		}
		if i == 0 {
			if entry := n.exactStatic["/"]; entry != nil {
				return prefixMatch[T]{
					entry:     entry,
					restIndex: 1,
					consumed:  1,
				}, true
			}
			continue
		}
		if entry := n.exactStatic[path[:i]]; entry != nil {
			return prefixMatch[T]{
				entry:     entry,
				restIndex: i + 1,
				consumed:  i + 1,
			}, true
		}
	}

	return prefixMatch[T]{}, false
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
	if route == "" || route[0] != '/' || n.root.plainParam != nil || len(n.root.affixedParams) != 0 || len(n.root.catchAll) != 0 {
		return &n.root, 0, false
	}
	if n.absoluteRoot != nil {
		return n.absoluteRoot, 1, true
	}
	return &n.root, 0, false
}
