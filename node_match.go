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
