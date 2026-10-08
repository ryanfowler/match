package match

import (
	"maps"
	"slices"
	"strings"
)

// Matcher is an immutable snapshot of a Router's registered routes, optimized
// for matching. The zero value matches no routes. A Matcher may be shared by
// multiple goroutines; each Into call must use its own parameter storage.
type Matcher[T any] struct {
	static  map[string]*routeEntry[T]
	maxPath int
	plan    matchPlan[T]
	prefix  prefixTrie[T]
}

// Compile returns an immutable Matcher containing the routes currently
// registered in r. Later inserts into r do not affect the returned Matcher.
// The snapshot shares immutable route entries with r. Stored values retain the
// same assignment semantics as Clone.
//
// Compilation selects lookup plans for the route table and keeps only the
// state needed for matching. Callers must synchronize Compile with insertion.
func (r *Router[T]) Compile() *Matcher[T] {
	n := &r.root
	var dynamic []*routeEntry[T]
	for _, entry := range n.routes {
		if len(entry.captures) != 0 {
			dynamic = append(dynamic, entry)
		}
	}
	m := &Matcher[T]{
		static:  maps.Clone(n.exactStatic),
		maxPath: n.maxExactStaticPathLen,
		plan:    compileMatchPlan(dynamic, true),
	}
	// Prefix matching needs the entire tree, including static routes. Copy its
	// nodes so future registrations cannot mutate the snapshot.
	nodes := make(map[*segmentNode[T]]*segmentNode[T])
	cloneSegmentNodeInto(&n.root, &m.prefix.root, nil, nodes)
	m.prefix.absoluteRoot = nodes[n.absoluteRoot]
	m.prefix.rootPrefix = n.rootPrefix
	m.prefix.hasDynamic = n.hasDynamic
	m.prefix.exactStatic = m.static
	m.prefix.maxExactStaticPathLen = m.maxPath
	return m
}

// Match returns the value and parameters for path, with the same semantics as
// Router.Match.
func (m *Matcher[T]) Match(path string) (T, Params, bool) {
	if entry := m.staticEntry(path); entry != nil {
		return entry.value, Params{}, true
	}
	if m.plan.complex != nil && strings.HasPrefix(path, m.plan.complexPrefix) {
		var params Params
		if entry := m.plan.matchComplex(path, &params); entry != nil {
			return entry.value, params, true
		}
		var zero T
		return zero, Params{}, false
	}
	if m.plan.terminal != nil {
		if entry, start := m.plan.terminalEntry(path); entry != nil {
			return entry.value, Params{
				len: 1, inline: [inlineParamCapacity]Param{{Key: entry.captures[0].name, Val: path[start:]}},
			}, true
		}
		if m.plan.kind == planTerminalParam {
			var zero T
			return zero, Params{}, false
		}
	}
	var params Params
	if m.plan.radix != nil {
		if entry, ok := m.plan.radix.match(path, 0, &params); ok {
			applySimpleParamNames(entry, &params)
			return entry.value, params, true
		}
		if m.plan.kind == planRadix {
			var zero T
			return zero, Params{}, false
		}
	}
	if entry := m.plan.match(path, &params); entry != nil {
		return entry.value, params, true
	}
	var zero T
	return zero, Params{}, false
}

// MatchInto is like Match, but uses params as reusable parameter storage, with
// the same semantics as Router.MatchInto. Params is reset before matching and
// must be non-nil.
func (m *Matcher[T]) MatchInto(path string, params *Params) (T, bool) {
	params.Reset()
	if entry := m.staticEntry(path); entry != nil {
		return entry.value, true
	}
	if m.plan.complex != nil && strings.HasPrefix(path, m.plan.complexPrefix) {
		if entry := m.plan.matchComplex(path, params); entry != nil {
			return entry.value, true
		}
		var zero T
		return zero, false
	}
	if m.plan.terminal != nil {
		if entry, start := m.plan.terminalEntry(path); entry != nil {
			params.Append(entry.captures[0].name, path[start:])
			return entry.value, true
		}
		if m.plan.kind == planTerminalParam {
			var zero T
			return zero, false
		}
	}
	if m.plan.radix != nil {
		if entry, ok := m.plan.radix.match(path, 0, params); ok {
			applySimpleParamNames(entry, params)
			return entry.value, true
		}
		if m.plan.kind == planRadix {
			var zero T
			return zero, false
		}
	}
	if entry := m.plan.match(path, params); entry != nil {
		return entry.value, true
	}
	var zero T
	return zero, false
}

// MatchPrefix returns the best whole-segment prefix, with the same semantics
// as Router.MatchPrefix.
func (m *Matcher[T]) MatchPrefix(path string) (PrefixMatch[T], bool) {
	return m.prefix.matchPrefix(path)
}

// MatchPrefixInto is like MatchPrefix, but uses params as reusable parameter
// storage, with the same semantics as Router.MatchPrefixInto. Params is reset
// before matching and must be non-nil.
func (m *Matcher[T]) MatchPrefixInto(path string, params *Params) (PrefixMatch[T], bool) {
	return m.prefix.matchPrefixInto(path, params)
}

func (m *Matcher[T]) staticEntry(path string) *routeEntry[T] {
	if len(m.static) == 0 || len(path) > m.maxPath {
		return nil
	}
	return m.static[path]
}

type matchPlanKind uint8

const (
	planEmpty matchPlanKind = iota
	planTerminalParam
	planRadix
	planBranches
	planSegments
)

type matchPlan[T any] struct {
	kind          matchPlanKind
	terminal      map[string]*routeEntry[T]
	radix         *simpleRadixNode[T]
	absolute      map[string]*matchPlan[T]
	relative      map[string]*matchPlan[T]
	wildcard      *matchPlan[T]
	segments      *prefixTrie[T]
	complexPrefix string
	complex       *matchPlan[T]
}

func compileMatchPlan[T any](routes []*routeEntry[T], partition bool) matchPlan[T] {
	if len(routes) == 0 {
		return matchPlan[T]{}
	}
	terminal, simple := true, true
	for _, entry := range routes {
		terminal = terminal && terminalParamRoute(entry)
		simple = simple && simpleRoute(entry)
	}
	if terminal {
		plan := matchPlan[T]{kind: planTerminalParam, terminal: make(map[string]*routeEntry[T], len(routes))}
		for _, entry := range routes {
			plan.terminal[entry.route[:entry.firstCaptureOffset]] = entry
		}
		return plan
	}
	if simple {
		plan := matchPlan[T]{kind: planRadix, radix: new(simpleRadixNode[T])}
		for _, entry := range routes {
			plan.radix.insert(entry)
		}
		return plan
	}
	if partition {
		absolute := make(map[string][]*routeEntry[T])
		relative := make(map[string][]*routeEntry[T])
		var wildcard []*routeEntry[T]
		for _, entry := range routes {
			if !entry.hasFirstStaticSegment {
				wildcard = append(wildcard, entry)
			} else if entry.patterns[0].raw == "" {
				absolute[entry.firstStaticSegment] = append(absolute[entry.firstStaticSegment], entry)
			} else {
				relative[entry.firstStaticSegment] = append(relative[entry.firstStaticSegment], entry)
			}
		}
		if len(absolute)+len(relative) != 0 {
			fallback := compileMatchPlan(wildcard, false)
			plan := matchPlan[T]{kind: planBranches, wildcard: &fallback}
			plan.absolute = compileBranches(absolute, &plan)
			plan.relative = compileBranches(relative, &plan)
			// One complex branch can be selected with a prefix comparison,
			// avoiding failed simple lookups and a segment-map dispatch.
			if len(plan.absolute)+len(plan.relative) == 1 {
				for segment, branch := range plan.absolute {
					plan.complexPrefix, plan.complex = "/"+segment+"/", branch
				}
				for segment, branch := range plan.relative {
					plan.complexPrefix, plan.complex = segment+"/", branch
				}
			}
			return plan
		}
	}
	plan := matchPlan[T]{kind: planSegments, segments: new(prefixTrie[T])}
	for _, entry := range routes {
		plan.segments.insertTree(entry)
	}
	return plan
}

func compileBranches[T any](groups map[string][]*routeEntry[T], root *matchPlan[T]) map[string]*matchPlan[T] {
	if len(groups) == 0 {
		return nil
	}
	var branches map[string]*matchPlan[T]
	for _, segment := range slices.Sorted(maps.Keys(groups)) {
		entries := groups[segment]
		terminal, simple := true, true
		for _, entry := range entries {
			terminal = terminal && terminalParamRoute(entry)
			simple = simple && simpleRoute(entry)
		}
		switch {
		case terminal:
			if root.terminal == nil {
				root.terminal = make(map[string]*routeEntry[T])
			}
			for _, entry := range entries {
				root.terminal[entry.route[:entry.firstCaptureOffset]] = entry
			}
		case simple:
			if root.radix == nil {
				root.radix = new(simpleRadixNode[T])
			}
			for _, entry := range entries {
				root.radix.insert(entry)
			}
		default:
			if branches == nil {
				branches = make(map[string]*matchPlan[T])
			}
			plan := compileMatchPlan(entries, false)
			branches[segment] = &plan
		}
	}
	return branches
}

func terminalParamRoute[T any](entry *routeEntry[T]) bool {
	if len(entry.captures) != 1 {
		return false
	}
	i := int(entry.captures[0].index)
	pattern := entry.patterns[i]
	return i == len(entry.patterns)-1 && pattern.param && pattern.prefix == "" && pattern.suffix == ""
}

func (p *matchPlan[T]) terminalEntry(path string) (*routeEntry[T], int) {
	start := strings.LastIndexByte(path, '/') + 1
	if start == len(path) {
		return nil, 0
	}
	return p.terminal[path[:start]], start
}

func (p *matchPlan[T]) matchComplex(path string, params *Params) *routeEntry[T] {
	if entry := p.complex.match(path, params); entry != nil {
		return entry
	}
	return p.wildcard.match(path, params)
}

func (p *matchPlan[T]) match(path string, params *Params) *routeEntry[T] {
	switch p.kind {
	case planTerminalParam:
		if entry, start := p.terminalEntry(path); entry != nil {
			params.Append(entry.captures[0].name, path[start:])
			return entry
		}
	case planRadix:
		if entry, ok := p.radix.match(path, 0, params); ok {
			applySimpleParamNames(entry, params)
			return entry
		}
	case planBranches:
		// Match and MatchInto already tried the terminal and radix plans.
		// Those plans contain entire static branches and therefore outrank
		// wildcard branches. Only branches with complex patterns remain here.
		groups, index := p.relative, 0
		if len(path) != 0 && path[0] == '/' {
			groups, index = p.absolute, 1
		}
		segment, _ := nextPathSegment(path, index)
		if branch := groups[segment]; branch != nil {
			if entry := branch.match(path, params); entry != nil {
				return entry
			}
		}
		return p.wildcard.match(path, params)
	case planSegments:
		root, index, _ := p.segments.matchRoot(path)
		if entry, ok := root.matchPath(path, index); ok {
			collectParams(entry, path, params)
			return entry
		}
	}
	return nil
}
