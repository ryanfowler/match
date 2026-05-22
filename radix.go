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

type simpleRadixBacktrackKind uint8

const (
	simpleRadixParamBacktrack simpleRadixBacktrackKind = iota
	simpleRadixCatchAllBacktrack
)

type simpleRadixBacktrack[T any] struct {
	node     *simpleRadixNode[T]
	index    int
	paramLen int
	start    int
	end      int
	kind     simpleRadixBacktrackKind
}

type simpleRadixBacktrackStack[T any] struct {
	inline0 simpleRadixBacktrack[T]
	inline1 simpleRadixBacktrack[T]
	spill   []simpleRadixBacktrack[T]
	len     int
}

func (s *simpleRadixBacktrackStack[T]) push(frame simpleRadixBacktrack[T]) {
	switch s.len {
	case 0:
		s.inline0 = frame
	case 1:
		s.inline1 = frame
	case 2:
		s.spill = append(s.spill, s.inline0, s.inline1, frame)
	default:
		s.spill = append(s.spill, frame)
	}
	s.len++
}

func (s *simpleRadixBacktrackStack[T]) pop() (simpleRadixBacktrack[T], bool) {
	if s.len == 0 {
		return simpleRadixBacktrack[T]{}, false
	}

	s.len--
	if len(s.spill) == 0 {
		if s.len == 0 {
			return s.inline0, true
		}
		return s.inline1, true
	}

	i := len(s.spill) - 1
	frame := s.spill[i]
	s.spill = s.spill[:i]
	return frame, true
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
	current := n
	var backtrack simpleRadixBacktrackStack[T]

	for {
		if index == len(path) {
			if current.value != nil {
				return current.value, true
			}
		} else {
			if child, next := current.staticChild(path, index); child != nil {
				if current.catchAll != nil || current.param != nil {
					paramLen := params.Len()
					if current.catchAll != nil {
						backtrack.push(simpleRadixBacktrack[T]{
							node:     current,
							index:    index,
							paramLen: paramLen,
							kind:     simpleRadixCatchAllBacktrack,
						})
					}
					if current.param != nil {
						if end := nextParamEnd(path, index); end > index {
							backtrack.push(simpleRadixBacktrack[T]{
								node:     current.param,
								index:    end,
								paramLen: paramLen,
								start:    index,
								end:      end,
								kind:     simpleRadixParamBacktrack,
							})
						}
					}
				}

				current = child
				index = next
				continue
			}

			if current.param != nil {
				if end := nextParamEnd(path, index); end > index {
					if current.catchAll != nil {
						backtrack.push(simpleRadixBacktrack[T]{
							node:     current,
							index:    index,
							paramLen: params.Len(),
							kind:     simpleRadixCatchAllBacktrack,
						})
					}
					params.Append("", path[index:end])
					current = current.param
					index = end
					continue
				}
			}

			if current.catchAll != nil {
				params.Append("", path[index:])
				return current.catchAll, true
			}
		}

		for {
			frame, ok := backtrack.pop()
			if !ok {
				return nil, false
			}
			params.truncate(frame.paramLen)

			switch frame.kind {
			case simpleRadixParamBacktrack:
				params.Append("", path[frame.start:frame.end])
				current = frame.node
				index = frame.index
				goto next
			case simpleRadixCatchAllBacktrack:
				params.Append("", path[frame.index:])
				return frame.node.catchAll, true
			}
		}

	next:
	}
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
