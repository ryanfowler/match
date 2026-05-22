package conflictindex

// Index groups conflict candidates by route shape so callers only compare
// entries that can plausibly overlap.
type Index[E any] struct {
	ByCount  map[int]*Bucket[E]
	CatchAll Bucket[E]
}

// Bucket stores entries with the same count.
type Bucket[E any] struct {
	All      []E
	Static   map[string][]E
	Wildcard []E
}

// Clone returns a copy of i with each stored entry replaced by translate(entry).
func (i Index[E]) Clone(translate func(E) E) Index[E] {
	var cloned Index[E]
	if len(i.ByCount) != 0 {
		cloned.ByCount = make(map[int]*Bucket[E], len(i.ByCount))
		for count, bucket := range i.ByCount {
			clonedBucket := bucket.Clone(translate)
			cloned.ByCount[count] = &clonedBucket
		}
	}
	cloned.CatchAll = i.CatchAll.Clone(translate)
	return cloned
}

// Clone returns a copy of b with each stored entry replaced by translate(entry).
func (b Bucket[E]) Clone(translate func(E) E) Bucket[E] {
	cloned := Bucket[E]{
		All:      cloneEntries(b.All, translate),
		Wildcard: cloneEntries(b.Wildcard, translate),
	}
	if len(b.Static) != 0 {
		cloned.Static = make(map[string][]E, len(b.Static))
		for key, entries := range b.Static {
			cloned.Static[key] = cloneEntries(entries, translate)
		}
	}
	return cloned
}

func cloneEntries[E any](entries []E, translate func(E) E) []E {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]E, len(entries))
	for i := range entries {
		cloned[i] = translate(entries[i])
	}
	return cloned
}

// Add stores entry in the count bucket and, when hasCatchAll is true, in the
// catch-all bucket.
func (i *Index[E]) Add(count int, staticKey string, hasStaticKey, hasCatchAll bool, entry E) {
	if i.ByCount == nil {
		i.ByCount = make(map[int]*Bucket[E])
	}
	bucket := i.ByCount[count]
	if bucket == nil {
		bucket = &Bucket[E]{}
		i.ByCount[count] = bucket
	}
	bucket.Add(staticKey, hasStaticKey, entry)
	if hasCatchAll {
		i.CatchAll.Add(staticKey, hasStaticKey, entry)
	}
}

// Add stores entry in b under staticKey or in the wildcard list.
func (b *Bucket[E]) Add(staticKey string, hasStaticKey bool, entry E) {
	b.All = append(b.All, entry)
	if !hasStaticKey {
		b.Wildcard = append(b.Wildcard, entry)
		return
	}
	if b.Static == nil {
		b.Static = make(map[string][]E)
	}
	b.Static[staticKey] = append(b.Static[staticKey], entry)
}
