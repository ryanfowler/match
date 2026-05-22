package match

import "strings"

type simpleRadixNode[T any] struct {
	value    *routeEntry[T]
	static   []simpleRadixEdge[T]
	param    *simpleRadixNode[T]
	catchAll *routeEntry[T]
}

type simpleRadixEdge[T any] struct {
	label string
	child *simpleRadixNode[T]
}

func (n *simpleRadixNode[T]) clone(entries map[*routeEntry[T]]*routeEntry[T]) simpleRadixNode[T] {
	var cloned simpleRadixNode[T]
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
		split.static = append(split.static, simpleRadixEdge[T]{
			label: oldLabel[common:],
			child: oldChild,
		})

		if common == len(path) {
			return split, ""
		}
		child := &simpleRadixNode[T]{}
		split.static = append(split.static, simpleRadixEdge[T]{
			label: path[common:],
			child: child,
		})
		return child, ""
	}

	child := &simpleRadixNode[T]{}
	n.static = append(n.static, simpleRadixEdge[T]{
		label: path,
		child: child,
	})
	return child, ""
}

func (n *simpleRadixNode[T]) match(path string, index int, params *Params) (*routeEntry[T], bool) {
	if index == len(path) {
		if n.value != nil {
			return n.value, true
		}
		return nil, false
	}

	if child, next := n.staticChild(path, index); child != nil {
		if entry, ok := child.match(path, next, params); ok {
			return entry, true
		}
	}

	if n.param != nil {
		end := nextParamEnd(path, index)
		if end > index {
			paramLen := params.Len()
			params.Append("", path[index:end])
			if entry, ok := n.param.match(path, end, params); ok {
				return entry, true
			}
			params.truncate(paramLen)
		}
	}

	if n.catchAll != nil {
		params.Append("", path[index:])
		return n.catchAll, true
	}

	return nil, false
}

func (n *simpleRadixNode[T]) staticChild(path string, index int) (*simpleRadixNode[T], int) {
	if index >= len(path) {
		return nil, 0
	}
	for i := range n.static {
		edge := &n.static[i]
		if path[index] != edge.label[0] {
			continue
		}
		if len(edge.label) <= len(path)-index && strings.HasPrefix(path[index:], edge.label) {
			return edge.child, index + len(edge.label)
		}
	}
	return nil, 0
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

func applySimpleParamNames[T any](entry *routeEntry[T], params *Params) {
	if entry.captureCount == 0 {
		return
	}

	index := 0
	for _, name := range entry.captureNames {
		if name == "" {
			continue
		}
		params.setKey(index, name)
		index++
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
