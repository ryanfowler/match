package dns

import "strings"

func (n *node[T]) insert(pattern string, value T) error {
	entry, key, err := makeRouteEntry(pattern, value, len(n.routes))
	if err != nil {
		return err
	}

	exactStatic := len(entry.captures) == 0 && !entry.hasCatchAll
	if exactStatic {
		if existing := n.exactStatic[key]; existing != nil {
			return &ConflictError{Pattern: entry.pattern, With: existing.pattern}
		}
	} else {
		n.hasDynamic = true
		if n.normalized == nil {
			n.normalized = make(map[string]string)
		}
		if existing, ok := n.normalized[key]; ok {
			return &ConflictError{Pattern: entry.pattern, With: existing}
		}
	}

	if len(entry.captures) != 0 {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Pattern: entry.pattern, With: existing.pattern}
		}
	}

	if exactStatic {
		n.addExactStatic(key, entry)
	} else {
		n.normalized[key] = entry.pattern
	}
	n.routes = append(n.routes, entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	return nil
}

func (n *node[T]) addExactStatic(key string, entry *routeEntry[T]) {
	if n.exactStatic == nil {
		n.exactStatic = make(map[string]*routeEntry[T])
	}
	n.exactStatic[key] = entry
	if len(key) > n.maxExactStaticHostLen {
		n.maxExactStaticHostLen = len(key)
	}
}

func (n *node[T]) matchExactStatic(host string) (*routeEntry[T], bool) {
	if len(n.exactStatic) == 0 || len(host) > n.maxExactStaticHostLen {
		return nil, false
	}
	if host[0] < 'A' || host[0] > 'Z' {
		if entry := n.exactStatic[host]; entry != nil {
			return entry, true
		}
		if asciiLower(host) {
			return nil, false
		}
	}
	return n.matchExactStaticFolded(host)
}

// Normalize into stack storage. Converting the buffer for a map lookup does
// not allocate, and avoids walking and hashing each label separately.
func (n *node[T]) matchExactStaticFolded(host string) (*routeEntry[T], bool) {
	var folded [maxHostnameLen]byte
	for i := 0; i < len(host); i++ {
		folded[i] = lowerASCIIByte(host[i])
	}
	entry := n.exactStatic[string(folded[:len(host)])]
	return entry, entry != nil
}

func makeRouteEntry[T any](pattern string, value T, order int) (*routeEntry[T], string, error) {
	labels, captures, canonicalPattern, err := parsePattern(pattern)
	if err != nil {
		return nil, "", err
	}

	firstStaticLabel, hasFirstStaticLabel := firstDefinitelyStaticLabel(labels)
	hasCatchAll := hasCatchAll(labels)
	if len(captures) == 1 {
		captures[0].prefix = uint32(singleCapturePrefix(labels, captures))
	}
	entry := &routeEntry[T]{
		pattern:             canonicalPattern,
		labels:              labels,
		captures:            captures,
		labelCount:          len(labels),
		singleCaptureSuffix: singleCaptureSuffix(labels, captures),
		order:               order,
		firstStaticLabel:    firstStaticLabel,
		hasFirstStaticLabel: hasFirstStaticLabel,
		hasCatchAll:         hasCatchAll,
		value:               value,
	}

	if len(captures) == 0 && !hasCatchAll {
		return entry, exactStaticKey(canonicalPattern, labels), nil
	}
	return entry, normalizedLabels(labels), nil
}

// With one capture, every label before and after it has a fixed length.
func singleCapturePrefix(labels []labelPattern, captures []captureMeta) int {
	if len(captures) != 1 {
		return 0
	}
	prefix := 0
	for i := 0; i < int(captures[0].index); i++ {
		prefix += len(labels[i].raw) + 1
	}
	return prefix
}

func singleCaptureSuffix(labels []labelPattern, captures []captureMeta) int {
	if len(captures) != 1 {
		return 0
	}

	suffix := 0
	for i := int(captures[0].index) + 1; i < len(labels); i++ {
		suffix += len(labels[i].raw) + 1
	}
	return suffix
}

func exactStaticKey(pattern string, labels []labelPattern) string {
	if staticPatternMatchesLabels(pattern, labels) {
		return pattern
	}

	var b strings.Builder
	b.Grow(len(pattern))
	for i := range labels {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(labels[i].raw)
	}
	return b.String()
}

func staticPatternMatchesLabels(pattern string, labels []labelPattern) bool {
	pos := 0
	for i := range labels {
		if i > 0 {
			if pos >= len(pattern) || pattern[pos] != '.' {
				return false
			}
			pos++
		}
		label := labels[i].raw
		if len(pattern)-pos < len(label) || pattern[pos:pos+len(label)] != label {
			return false
		}
		pos += len(label)
	}
	return pos == len(pattern)
}
