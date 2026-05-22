package dns

func (n *node[T]) insert(pattern string, value T) error {
	entry, normalized, err := makeRouteEntry(pattern, value, len(n.routes))
	if err != nil {
		return err
	}

	if n.normalized == nil {
		n.normalized = make(map[string]string)
	}
	if existing, ok := n.normalized[normalized]; ok {
		return &ConflictError{Pattern: entry.pattern, With: existing}
	}

	if entry.captureCount != 0 {
		if existing := n.conflictIndex.findConflict(entry); existing != nil {
			return &ConflictError{Pattern: entry.pattern, With: existing.pattern}
		}
	}

	n.normalized[normalized] = entry.pattern
	n.routes = append(n.routes, entry)
	n.conflictIndex.add(entry)
	n.insertTree(entry)
	return nil
}

func makeRouteEntry[T any](pattern string, value T, order int) (*routeEntry[T], string, error) {
	labels, captureNames, singleCaptureLabel, captureCount, canonicalPattern, err := parsePattern(pattern)
	if err != nil {
		return nil, "", err
	}

	firstStaticLabel, hasFirstStaticLabel := firstDefinitelyStaticLabel(labels)
	entry := &routeEntry[T]{
		pattern:             canonicalPattern,
		labels:              labels,
		captureNames:        captureNames,
		singleCaptureLabel:  uint32(singleCaptureLabel),
		captureCount:        captureCount,
		labelCount:          len(labels),
		order:               order,
		firstStaticLabel:    firstStaticLabel,
		hasFirstStaticLabel: hasFirstStaticLabel,
		hasCatchAll:         hasCatchAll(labels),
		value:               value,
	}

	return entry, normalizedLabels(labels), nil
}
