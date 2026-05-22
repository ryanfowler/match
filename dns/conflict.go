package dns

import (
	"strings"
)

func (i *routeConflictIndex[T]) add(entry *routeEntry[T]) {
	if len(entry.captures) == 0 {
		return
	}
	if i.byLabelCount == nil {
		i.byLabelCount = make(map[int]*routeConflictBucket[T])
	}
	bucket := i.byLabelCount[entry.labelCount]
	if bucket == nil {
		bucket = &routeConflictBucket[T]{}
		i.byLabelCount[entry.labelCount] = bucket
	}
	bucket.add(entry)
	if entry.hasCatchAll {
		i.catchAll.add(entry)
	}
}

func (b *routeConflictBucket[T]) add(entry *routeEntry[T]) {
	b.all = append(b.all, entry)
	if !entry.hasFirstStaticLabel {
		b.wildcard = append(b.wildcard, entry)
		return
	}
	if b.static == nil {
		b.static = make(map[string][]*routeEntry[T])
	}
	b.static[entry.firstStaticLabel] = append(b.static[entry.firstStaticLabel], entry)
}

func (i *routeConflictIndex[T]) findConflict(entry *routeEntry[T]) *routeEntry[T] {
	var best *routeEntry[T]
	if bucket := i.byLabelCount[entry.labelCount]; bucket != nil {
		best = earlierConflict(best, bucket.findConflict(entry, 0))
	}

	if entry.hasCatchAll {
		for labelCount, bucket := range i.byLabelCount {
			if labelCount == entry.labelCount {
				continue
			}
			best = earlierConflict(best, bucket.findConflict(entry, 0))
		}
		return best
	}

	return earlierConflict(best, i.catchAll.findConflict(entry, entry.labelCount))
}

func (b *routeConflictBucket[T]) findConflict(entry *routeEntry[T], skipLabelCount int) *routeEntry[T] {
	if b == nil {
		return nil
	}
	if entry.hasFirstStaticLabel {
		static := findConflictInRoutes(b.static[entry.firstStaticLabel], entry, skipLabelCount)
		wildcard := findConflictInRoutes(b.wildcard, entry, skipLabelCount)
		return earlierConflict(static, wildcard)
	}
	return findConflictInRoutes(b.all, entry, skipLabelCount)
}

func findConflictInRoutes[T any](routes []*routeEntry[T], entry *routeEntry[T], skipLabelCount int) *routeEntry[T] {
	for _, existing := range routes {
		if skipLabelCount != 0 && existing.labelCount == skipLabelCount {
			continue
		}
		if conflictsEntries(existing, entry) {
			return existing
		}
	}
	return nil
}

func earlierConflict[T any](a, b *routeEntry[T]) *routeEntry[T] {
	if a == nil || (b != nil && b.order < a.order) {
		return b
	}
	return a
}

func conflictsEntries[T any](a, b *routeEntry[T]) bool {
	if len(a.captures) == 0 || len(b.captures) == 0 {
		return false
	}
	if a.hasCatchAll || b.hasCatchAll {
		return catchAllConflicts(a, b)
	}
	return conflictsLabels(a.labels, b.labels)
}

func conflictsLabels(as, bs []labelPattern) bool {
	if len(as) != len(bs) {
		return false
	}
	ambiguous := false
	for i := range as {
		if !labelMayOverlap(as[i], bs[i]) {
			return false
		}
		if labelConflict(as[i], bs[i]) {
			ambiguous = true
		}
	}
	return ambiguous
}

func catchAllConflicts[T any](a, b *routeEntry[T]) bool {
	switch {
	case a.hasCatchAll && b.hasCatchAll:
		return catchAllEntriesMayOverlap(a, b)
	case a.hasCatchAll:
		return catchAllFiniteConflict(a, b)
	default:
		return catchAllFiniteConflict(b, a)
	}
}

func catchAllEntriesMayOverlap[T any](a, b *routeEntry[T]) bool {
	if !compatiblePrefixes(a.labels[0].prefix, b.labels[0].prefix) {
		return false
	}
	return suffixesMayOverlap(a.labels[1:], b.labels[1:])
}

func catchAllFiniteConflict[T any](catchAll, finite *routeEntry[T]) bool {
	suffix := catchAll.labels[1:]
	if len(finite.labels) <= len(suffix) {
		return false
	}

	start := len(finite.labels) - len(suffix)
	for i := range suffix {
		if !labelMayOverlap(suffix[i], finite.labels[start+i]) {
			return false
		}
	}
	return catchAllOverlapsLeading(catchAll.labels[0], finite.labels[:start])
}

func suffixesMayOverlap(as, bs []labelPattern) bool {
	limit := len(as)
	if len(bs) < limit {
		limit = len(bs)
	}
	for i := 0; i < limit; i++ {
		if !labelMayOverlap(as[len(as)-1-i], bs[len(bs)-1-i]) {
			return false
		}
	}
	return true
}

func catchAllOverlapsLeading(catchAll labelPattern, leading []labelPattern) bool {
	if len(leading) == 0 {
		return false
	}
	return labelCanStartLongerThan(leading[0], catchAll.prefix)
}

func labelConflict(a, b labelPattern) bool {
	if a.literal || b.literal {
		return false
	}
	if !a.param || !b.param {
		return false
	}
	if a.prefix == "" && a.suffix == "" {
		return false
	}
	if b.prefix == "" && b.suffix == "" {
		return false
	}
	return labelMayOverlap(a, b)
}

func labelMayOverlap(a, b labelPattern) bool {
	if a.literal && b.literal {
		return a.raw == b.raw
	}
	if a.literal {
		return literalMatchesLabel(a.raw, b)
	}
	if b.literal {
		return literalMatchesLabel(b.raw, a)
	}
	return compatiblePrefixes(a.prefix, b.prefix) && compatibleSuffixes(a.suffix, b.suffix)
}

func literalMatchesLabel(lit string, pattern labelPattern) bool {
	if pattern.catchAll {
		return strings.HasPrefix(lit, pattern.prefix) && len(lit) > len(pattern.prefix)
	}
	if !pattern.param {
		return lit == pattern.raw
	}
	if !strings.HasPrefix(lit, pattern.prefix) || !strings.HasSuffix(lit, pattern.suffix) {
		return false
	}
	return len(lit) > len(pattern.prefix)+len(pattern.suffix)
}

func compatiblePrefixes(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func compatibleSuffixes(a, b string) bool {
	return strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

func labelCanStartLongerThan(pattern labelPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix) && len(pattern.raw) > len(prefix)
	}
	return labelCanStartWith(pattern, prefix)
}

func labelCanStartWith(pattern labelPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix)
	}
	return compatiblePrefixes(pattern.prefix, prefix)
}
