package match

import (
	"maps"
	"slices"
)

func (n *node[T]) clone() node[T] {
	var cloned node[T]
	entries := make(map[*routeEntry[T]]*routeEntry[T], len(n.routes))
	cloned.routes = cloneRouteEntries(n.routes, entries)
	cloned.exactStatic = cloneExactStatic(n.exactStatic, cloned.routes)
	cloned.maxExactStaticPathLen = n.maxExactStaticPathLen
	cloned.fastRoot = n.fastRoot.clone(entries)
	cloned.hasComplexParams = n.hasComplexParams
	cloned.hasSimpleDynamic = n.hasSimpleDynamic
	cloned.hasDynamic = n.hasDynamic
	cloned.maxSimpleCaptureCount = n.maxSimpleCaptureCount
	cloned.normalized = maps.Clone(n.normalized)
	cloned.conflictIndex = n.conflictIndex.clone(cloned.routes)

	nodes := make(map[*segmentNode[T]]*segmentNode[T])
	cloneSegmentNodeInto(&n.root, &cloned.root, entries, nodes)
	if n.absoluteRoot != nil {
		cloned.absoluteRoot = nodes[n.absoluteRoot]
	}
	if n.rootPrefix != nil {
		cloned.rootPrefix = entries[n.rootPrefix]
	}

	return cloned
}

func cloneExactStatic[T any](exactStatic map[string]*routeEntry[T], routes []*routeEntry[T]) map[string]*routeEntry[T] {
	if len(exactStatic) == 0 {
		return nil
	}

	cloned := make(map[string]*routeEntry[T], len(exactStatic))
	for path, entry := range exactStatic {
		cloned[path] = routes[entry.order]
	}
	return cloned
}

func cloneRouteEntries[T any](routes []*routeEntry[T], entries map[*routeEntry[T]]*routeEntry[T]) []*routeEntry[T] {
	if len(routes) == 0 {
		return nil
	}

	clonedRoutes := make([]*routeEntry[T], len(routes))
	for i, entry := range routes {
		clonedEntry := new(routeEntry[T])
		*clonedEntry = *entry
		clonedEntry.patterns = slices.Clone(entry.patterns)
		clonedEntry.captures = slices.Clone(entry.captures)
		clonedRoutes[i] = clonedEntry
		entries[entry] = clonedEntry
	}
	return clonedRoutes
}

// A nil entries map preserves immutable route entries for compiled snapshots.
// Router clones pass a map to remap entries to independent copies.
func cloneSegmentNodeInto[T any](src, dst *segmentNode[T], entries map[*routeEntry[T]]*routeEntry[T], nodes map[*segmentNode[T]]*segmentNode[T]) {
	nodes[src] = dst
	dst.value = src.value
	if src.value != nil && entries != nil {
		dst.value = entries[src.value]
	}

	if len(src.static) != 0 {
		dst.static = slices.Clone(src.static)
		for i := range src.static {
			child := new(segmentNode[T])
			cloneSegmentNodeInto(src.static[i].child, child, entries, nodes)
			dst.static[i].child = child
		}
	}
	if src.staticIndex != nil {
		dst.staticIndex = maps.Clone(src.staticIndex)
		for segment, child := range dst.staticIndex {
			dst.staticIndex[segment] = nodes[child]
		}
	}

	if src.plainParam != nil {
		dst.plainParam = new(paramEdge[T])
		*dst.plainParam = *src.plainParam
		child := new(segmentNode[T])
		cloneSegmentNodeInto(src.plainParam.child, child, entries, nodes)
		dst.plainParam.child = child
	}

	if len(src.affixedParams) != 0 {
		dst.affixedParams = slices.Clone(src.affixedParams)
		for i := range src.affixedParams {
			child := new(segmentNode[T])
			cloneSegmentNodeInto(src.affixedParams[i].child, child, entries, nodes)
			dst.affixedParams[i].child = child
		}
	}

	if len(src.catchAll) != 0 {
		dst.catchAll = slices.Clone(src.catchAll)
		if entries != nil {
			for i := range src.catchAll {
				dst.catchAll[i].route = entries[src.catchAll[i].route]
			}
		}
	}
}
