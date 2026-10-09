package match

import "strings"

type simpleRadixNode[T any] struct {
	value    *routeEntry[T]
	static   []simpleRadixEdge[T]
	indices  string // First bytes of static edges, in slice order.
	param    *simpleRadixNode[T]
	catchAll *routeEntry[T]
}

type simpleRadixEdge[T any] struct {
	label string
	child *simpleRadixNode[T]
}

func (n *simpleRadixNode[T]) clone(entries map[*routeEntry[T]]*routeEntry[T]) simpleRadixNode[T] {
	var cloned simpleRadixNode[T]
	cloned.indices = n.indices
	if n.value != nil {
		cloned.value = entries[n.value]
	}
	if len(n.static) != 0 {
		cloned.static = make([]simpleRadixEdge[T], len(n.static))
		for i := range n.static {
			child := n.static[i].child.clone(entries)
			cloned.static[i] = simpleRadixEdge[T]{
				label: n.static[i].label,
				child: &child,
			}
		}
	}
	if n.param != nil {
		child := n.param.clone(entries)
		cloned.param = &child
	}
	if n.catchAll != nil {
		cloned.catchAll = entries[n.catchAll]
	}
	return cloned
}

func (n *simpleRadixNode[T]) insert(entry *routeEntry[T]) {
	current := n
	var static strings.Builder

	flushStatic := func() {
		if static.Len() == 0 {
			return
		}
		current = current.insertStatic(static.String())
		static.Reset()
	}

	for i, pattern := range entry.patterns {
		if i > 0 {
			static.WriteByte('/')
		}

		switch {
		case pattern.literal:
			static.WriteString(pattern.raw)
		case pattern.param:
			flushStatic()
			if current.param == nil {
				current.param = &simpleRadixNode[T]{}
			}
			current = current.param
		case pattern.catchAll:
			flushStatic()
			current.catchAll = entry
			return
		}
	}

	flushStatic()
	current.value = entry
}

func (n *simpleRadixNode[T]) insertStatic(path string) *simpleRadixNode[T] {
	current := n
	for path != "" {
		next, rest := current.insertStaticEdge(path)
		current = next
		path = rest
	}
	return current
}

func (n *simpleRadixNode[T]) insertStaticEdge(path string) (*simpleRadixNode[T], string) {
	for i := range n.static {
		edge := &n.static[i]
		common := commonPrefixLen(path, edge.label)
		if common == 0 {
			continue
		}

		if common == len(edge.label) {
			return edge.child, path[common:]
		}

		split := &simpleRadixNode[T]{}
		oldLabel := edge.label
		oldChild := edge.child
		edge.label = oldLabel[:common]
		edge.child = split
		split.addStaticEdge(oldLabel[common:], oldChild)

		if common == len(path) {
			return split, ""
		}
		child := &simpleRadixNode[T]{}
		split.addStaticEdge(path[common:], child)
		return child, ""
	}

	child := &simpleRadixNode[T]{}
	n.addStaticEdge(path, child)
	return child, ""
}

func (n *simpleRadixNode[T]) addStaticEdge(label string, child *simpleRadixNode[T]) {
	n.static = append(n.static, simpleRadixEdge[T]{label: label, child: child})
	n.indices += label[:1]
}

func (n *simpleRadixNode[T]) match(path string, index int, params *Params) (*routeEntry[T], bool) {
	startLen := params.len
	for {
		if index == len(path) {
			if n.value != nil {
				return n.value, true
			}
			params.Truncate(startLen)
			return nil, false
		}

		var child *simpleRadixNode[T]
		next := index
		edgeIndex := -1
		// Scan first bytes without loading each edge's label and child. Wider
		// nodes benefit from the optimized byte search in strings.IndexByte.
		if len(n.indices) > 4 {
			edgeIndex = strings.IndexByte(n.indices, path[index])
		} else {
			for i := 0; i < len(n.indices); i++ {
				if n.indices[i] == path[index] {
					edgeIndex = i
					break
				}
			}
		}
		if edgeIndex >= 0 {
			edge := &n.static[edgeIndex]
			if strings.HasPrefix(path[index:], edge.label) {
				child, next = edge.child, index+len(edge.label)
			}
		}
		if child != nil {
			// Only branching nodes need a stack frame for backtracking.
			if n.param == nil && n.catchAll == nil {
				n, index = child, next
				continue
			}
			if entry, ok := child.match(path, next, params); ok {
				return entry, true
			}
		}

		if n.param != nil {
			end := nextParamEnd(path, index)
			if end > index {
				paramLen := params.len
				params.Append("", path[index:end])
				if n.catchAll == nil {
					n, index = n.param, end
					continue
				}
				if entry, ok := n.param.match(path, end, params); ok {
					return entry, true
				}
				params.Truncate(paramLen)
			}
		}

		if n.catchAll != nil {
			params.Append("", path[index:])
			return n.catchAll, true
		}

		params.Truncate(startLen)
		return nil, false
	}
}

func nextParamEnd(path string, index int) int {
	if slash := strings.IndexByte(path[index:], '/'); slash >= 0 {
		return index + slash
	}
	return len(path)
}

func commonPrefixLen(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}

// applySimpleParamNames names the captures that a simple radix match appended
// to params. The radix tree appends one unnamed value per capture, so the
// captures of entry are the last len(entry.captures) parameters.
func applySimpleParamNames[T any](entry *routeEntry[T], params *Params) {
	base := params.len - len(entry.captures)
	for i, capture := range entry.captures {
		params.setKey(base+i, capture.name)
	}
}

func simpleRoute[T any](entry *routeEntry[T]) bool {
	for i, pattern := range entry.patterns {
		if pattern.param && (pattern.prefix != "" || pattern.suffix != "") {
			return false
		}
		if pattern.catchAll {
			return pattern.prefix == "" && i == len(entry.patterns)-1
		}
	}
	return true
}
