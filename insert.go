package match

import (
	"strings"
)

func (n *node[T]) insert(route string, value T) error {
	if !strings.ContainsAny(route, "{}") {
		return n.insertLiteral(route, value)
	}
	return n.insertDynamic(route, value)
}

func (n *node[T]) insertLiteral(route string, value T) error {
	patterns := literalSegmentPatterns(route)
	firstStaticSegment, hasFirstStaticSegment := firstDefinitelyStaticSegment(patterns)
	entry := &routeEntry[T]{
		route:                 route,
		patterns:              patterns,
		segmentCount:          len(patterns),
		order:                 len(n.routes),
		firstStaticSegment:    firstStaticSegment,
		hasFirstStaticSegment: hasFirstStaticSegment,
		value:                 value,
	}
	normalized := normalizedStaticLiteral(route)
	catchAllStaticConflict := n.root.conflictsWithCatchAllStaticPatterns(patterns, 0)
	needsConflictCheck := len(entry.captures) != 0 || catchAllStaticConflict

	return n.finishInsert(entry, normalized, needsConflictCheck)
}

func (n *node[T]) insertDynamic(route string, value T) error {
	tokens, normalized, err := parseRoute(route)
	if err != nil {
		return err
	}

	segments := splitTokenSegments(tokens)
	patterns, captures := makeSegmentPatterns(segments)
	firstStaticSegment, hasFirstStaticSegment := firstDefinitelyStaticSegment(patterns)
	entry := &routeEntry[T]{
		route:                 unescapeBraces(route),
		patterns:              patterns,
		captures:              captures,
		segmentCount:          len(patterns),
		order:                 len(n.routes),
		firstStaticSegment:    firstStaticSegment,
		hasFirstStaticSegment: hasFirstStaticSegment,
		hasCatchAll:           hasCatchAll(patterns),
		value:                 value,
	}
	needsConflictCheck := len(entry.captures) != 0 || n.root.conflictsWithCatchAllStatic(segments, 0)

	return n.finishInsert(entry, normalized, needsConflictCheck)
}

func (n *node[T]) finishInsert(entry *routeEntry[T], normalized string, needsConflictCheck bool) error {
	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[normalized]; ok {
		return &ConflictError{Route: entry.route, With: existing}
	}

	if needsConflictCheck {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Route: entry.route, With: existing.route}
		}
	}

	n.normalized[normalized] = entry.route
	n.routes = append(n.routes, entry)
	n.addExactStatic(entry)
	n.addFastRoute(entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	n.refreshRootPrefix(entry)
	return nil
}

func (n *node[T]) addExactStatic(entry *routeEntry[T]) {
	if len(entry.captures) != 0 || entry.hasCatchAll {
		return
	}
	if n.exactStatic == nil {
		n.exactStatic = make(map[string]*routeEntry[T])
	}
	n.exactStatic[entry.route] = entry
	if len(entry.route) > n.maxExactStaticPathLen {
		n.maxExactStaticPathLen = len(entry.route)
	}
}

func (n *node[T]) addFastRoute(entry *routeEntry[T]) {
	if !simpleRoute(entry) {
		n.hasComplexParams = true
		return
	}
	capturesLen := len(entry.captures)
	if capturesLen == 0 {
		return
	}
	n.hasSimpleDynamic = true
	n.fastRoot.insert(entry)
	if capturesLen > n.maxSimpleCaptureCount {
		n.maxSimpleCaptureCount = capturesLen
	}
}

func normalizedStaticLiteral(route string) string {
	return "S" + route
}

func (n *node[T]) refreshRootPrefix(entry *routeEntry[T]) {
	if len(entry.patterns) == 2 &&
		entry.patterns[0].literal &&
		entry.patterns[0].raw == "" &&
		entry.patterns[1].literal &&
		entry.patterns[1].raw == "" {
		n.rootPrefix = entry
	}
}
