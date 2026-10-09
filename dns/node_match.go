package dns

func (n *node[T]) match(hostname string) (T, Params, bool) {
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		var val T
		return val, Params{}, false
	}

	if !n.hasDynamic {
		if entry, ok := n.matchExactStatic(host); ok {
			return entry.value, Params{}, true
		}
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

func (n *node[T]) matchAppend(hostname string, params *Params) (T, bool) {
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		var val T
		return val, false
	}

	if !n.hasDynamic {
		if entry, ok := n.matchExactStatic(host); ok {
			return entry.value, true
		}
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
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		return SuffixMatch[T]{}, false
	}

	var match suffixRouteMatch[T]
	if n.hasDynamic {
		match, ok = n.root.matchSuffixHost(host, len(host), 0)
	} else {
		match, ok = n.root.matchStaticSuffixHost(host, len(host))
	}
	if !ok {
		return SuffixMatch[T]{}, false
	}
	if !validSuffixPrefix(host, match.prefixEnd) {
		return SuffixMatch[T]{}, false
	}
	var params Params
	collectParams(match.entry, host, suffixStart(match.prefixEnd), &params)
	return match.suffix(host, params), true
}

func (n *node[T]) matchSuffixInto(hostname string, params *Params) (SuffixMatch[T], bool) {
	params.Reset()
	host, ok := hostnameWithinBounds(hostname)
	if !ok {
		return SuffixMatch[T]{Params: *params}, false
	}

	var match suffixRouteMatch[T]
	if n.hasDynamic {
		match, ok = n.root.matchSuffixHost(host, len(host), 0)
	} else {
		match, ok = n.root.matchStaticSuffixHost(host, len(host))
	}
	if !ok {
		return SuffixMatch[T]{Params: *params}, false
	}
	if !validSuffixPrefix(host, match.prefixEnd) {
		return SuffixMatch[T]{Params: *params}, false
	}
	collectParams(match.entry, host, suffixStart(match.prefixEnd), params)
	return match.suffix(host, *params), true
}

func validSuffixPrefix(host string, prefixEnd int) bool {
	if prefixEnd < 0 {
		return true
	}
	return validHostnameLabels(host[:prefixEnd])
}
