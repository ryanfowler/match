package match

import (
	"strings"
)

func (i *routeConflictIndex[T]) add(entry *routeEntry[T]) {
	if entry.captureCount == 0 {
		return
	}
	if i.bySegmentCount == nil {
		i.bySegmentCount = make(map[int]*routeConflictBucket[T])
	}
	bucket := i.bySegmentCount[entry.segmentCount]
	if bucket == nil {
		bucket = &routeConflictBucket[T]{}
		i.bySegmentCount[entry.segmentCount] = bucket
	}
	bucket.add(entry)
	if entry.hasCatchAll {
		i.catchAll.add(entry)
	}
}

func (b *routeConflictBucket[T]) add(entry *routeEntry[T]) {
	b.all = append(b.all, entry)
	if !entry.hasFirstStaticSegment {
		b.wildcard = append(b.wildcard, entry)
		return
	}
	if b.static == nil {
		b.static = make(map[string][]*routeEntry[T])
	}
	b.static[entry.firstStaticSegment] = append(b.static[entry.firstStaticSegment], entry)
}

func (i *routeConflictIndex[T]) findConflict(entry *routeEntry[T]) *routeEntry[T] {
	var best *routeEntry[T]
	if bucket := i.bySegmentCount[entry.segmentCount]; bucket != nil {
		best = earlierConflict(best, bucket.findConflict(entry, 0))
	}

	if entry.hasCatchAll {
		for segmentCount, bucket := range i.bySegmentCount {
			if segmentCount == entry.segmentCount {
				continue
			}
			best = earlierConflict(best, bucket.findConflict(entry, 0))
		}
		return best
	}

	return earlierConflict(best, i.catchAll.findConflict(entry, entry.segmentCount))
}

func (b *routeConflictBucket[T]) findConflict(entry *routeEntry[T], skipSegmentCount int) *routeEntry[T] {
	if b == nil {
		return nil
	}
	if entry.hasFirstStaticSegment {
		static := findConflictInRoutes(b.static[entry.firstStaticSegment], entry, skipSegmentCount)
		wildcard := findConflictInRoutes(b.wildcard, entry, skipSegmentCount)
		return earlierConflict(static, wildcard)
	}
	return findConflictInRoutes(b.all, entry, skipSegmentCount)
}

func findConflictInRoutes[T any](routes []*routeEntry[T], entry *routeEntry[T], skipSegmentCount int) *routeEntry[T] {
	for _, existing := range routes {
		if skipSegmentCount != 0 && existing.segmentCount == skipSegmentCount {
			continue
		}
		if entry.captureCount == 0 && existing.captureCount == 0 {
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
	if hasCatchAllPrefixConflict(a, b) || hasCatchAllPrefixConflict(b, a) {
		return true
	}
	return conflictsPatterns(a.patterns, b.patterns)
}

func conflictsPatterns(as, bs []segmentPattern) bool {
	if len(as) != len(bs) {
		return false
	}
	ambiguous := false
	for i := range as {
		if as[i].catchAll || bs[i].catchAll {
			if !segmentMayOverlap(as[i], bs[i]) {
				return false
			}
			if !as[i].literal && !bs[i].literal {
				ambiguous = true
			}
			continue
		}
		if !segmentMayOverlap(as[i], bs[i]) {
			return false
		}
		if segmentConflict(as[i], bs[i]) {
			ambiguous = true
		}
	}
	return ambiguous
}

func hasCatchAllPrefixConflict[T any](a, b *routeEntry[T]) bool {
	if b.captureCount == 0 {
		return false
	}
	for i, pattern := range a.patterns {
		if !pattern.catchAll {
			continue
		}
		if len(b.patterns) <= i {
			return false
		}
		for j := 0; j < i; j++ {
			if !segmentMayOverlap(a.patterns[j], b.patterns[j]) {
				return false
			}
		}
		return catchAllOverlapsSuffix(pattern, b.patterns[i:])
	}
	return false
}

func segmentConflict(a, b segmentPattern) bool {
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
	return segmentMayOverlap(a, b)
}

func segmentMayOverlap(a, b segmentPattern) bool {
	if a.literal && b.literal {
		return a.raw == b.raw
	}
	if a.literal {
		return literalMatchesSegment(a.raw, b)
	}
	if b.literal {
		return literalMatchesSegment(b.raw, a)
	}
	return compatibleAffixes(a.prefix, b.prefix) && compatibleSuffixes(a.suffix, b.suffix)
}

func literalMatchesSegment(lit string, p segmentPattern) bool {
	if p.catchAll {
		return strings.HasPrefix(lit, p.prefix) && len(lit) > len(p.prefix)
	}
	if !p.param {
		return lit == p.raw
	}
	if !strings.HasPrefix(lit, p.prefix) || !strings.HasSuffix(lit, p.suffix) {
		return false
	}
	return len(lit) > len(p.prefix)+len(p.suffix)
}

func compatibleAffixes(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func compatibleSuffixes(a, b string) bool {
	return strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

func catchAllOverlapsSuffix(catchAll segmentPattern, suffix []segmentPattern) bool {
	if len(suffix) == 0 {
		return false
	}
	first := suffix[0]
	if segmentCanStartLongerThan(first, catchAll.prefix) {
		return true
	}
	return len(suffix) > 1 && segmentCanEqual(first, catchAll.prefix)
}

func segmentCanStartLongerThan(pattern segmentPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix) && len(pattern.raw) > len(prefix)
	}
	return segmentCanStartWith(pattern, prefix)
}

func segmentCanStartWith(pattern segmentPattern, prefix string) bool {
	if pattern.literal {
		return strings.HasPrefix(pattern.raw, prefix)
	}
	return compatibleAffixes(pattern.prefix, prefix)
}

func segmentCanEqual(pattern segmentPattern, value string) bool {
	if pattern.literal {
		return pattern.raw == value
	}
	if pattern.catchAll {
		return strings.HasPrefix(value, pattern.prefix) && len(value) > len(pattern.prefix)
	}
	return literalMatchesSegment(value, pattern)
}
