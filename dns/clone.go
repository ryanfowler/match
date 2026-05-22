package dns

import (
	"maps"
	"slices"
)

func (n *node[T]) clone() node[T] {
	var cloned node[T]
	entries := make(map[*routeEntry[T]]*routeEntry[T], len(n.routes))
	cloned.routes = cloneRouteEntries(n.routes, entries)
	cloned.exactStatic = cloneExactStatic(n.exactStatic, cloned.routes)
	cloned.maxExactStaticHostLen = n.maxExactStaticHostLen
	cloned.hasDynamic = n.hasDynamic
	cloned.normalized = maps.Clone(n.normalized)
	cloned.conflictIndex = n.conflictIndex.clone(cloned.routes)

	nodes := make(map[*labelNode[T]]*labelNode[T])
	cloneLabelNodeInto(&n.root, &cloned.root, entries, nodes)

	return cloned
}

func cloneExactStatic[T any](exactStatic map[string]*routeEntry[T], routes []*routeEntry[T]) map[string]*routeEntry[T] {
	if len(exactStatic) == 0 {
		return nil
	}

	cloned := make(map[string]*routeEntry[T], len(exactStatic))
	for host, entry := range exactStatic {
		cloned[host] = routes[entry.order]
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
		clonedEntry.labels = slices.Clone(entry.labels)
		clonedEntry.captures = slices.Clone(entry.captures)
		clonedRoutes[i] = clonedEntry
		entries[entry] = clonedEntry
	}
	return clonedRoutes
}

func cloneLabelNodeInto[T any](src, dst *labelNode[T], entries map[*routeEntry[T]]*routeEntry[T], nodes map[*labelNode[T]]*labelNode[T]) {
	nodes[src] = dst
	if src.value != nil {
		dst.value = entries[src.value]
	}

	if len(src.static) != 0 {
		dst.static = slices.Clone(src.static)
		for i := range src.static {
			child := new(labelNode[T])
			cloneLabelNodeInto(src.static[i].child, child, entries, nodes)
			dst.static[i].child = child
		}
	}
	if src.staticIndex != nil {
		dst.staticIndex = maps.Clone(src.staticIndex)
		for label, child := range dst.staticIndex {
			dst.staticIndex[label] = nodes[child]
		}
	}
	if src.staticFoldIndex != nil {
		dst.staticFoldIndex = maps.Clone(src.staticFoldIndex)
		for label, child := range dst.staticFoldIndex {
			dst.staticFoldIndex[label] = nodes[child]
		}
	}

	if len(src.params) != 0 {
		dst.params = slices.Clone(src.params)
		for i := range src.params {
			child := new(labelNode[T])
			cloneLabelNodeInto(src.params[i].child, child, entries, nodes)
			dst.params[i].child = child
		}
	}

	if len(src.catchAll) != 0 {
		dst.catchAll = slices.Clone(src.catchAll)
		for i := range src.catchAll {
			dst.catchAll[i].route = entries[src.catchAll[i].route]
		}
	}
}
