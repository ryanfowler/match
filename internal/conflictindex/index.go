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
