package match

import (
	"strconv"
	"strings"

	"github.com/ryanfowler/match/internal/patternscan"
)

var routeScanOptions = patternscan.Options{
	Separator:           '/',
	CatchAllRule:        patternscan.CatchAllAtEnd,
	ErrInvalidParam:     ErrInvalidParam,
	ErrInvalidParamPart: ErrInvalidParamSegment,
	ErrInvalidCatchAll:  ErrInvalidCatchAll,
}

func literalSegmentPatterns(route string) []segmentPattern {
	patterns := make([]segmentPattern, 0, strings.Count(route, "/")+1)
	start := 0
	for i := 0; i < len(route); i++ {
		if route[i] != '/' {
			continue
		}
		patterns = append(patterns, segmentPattern{raw: route[start:i], literal: true})
		start = i + 1
	}
	return append(patterns, segmentPattern{raw: route[start:], literal: true})
}

func splitTokenSegments(tokens []token) [][]token {
	segments := make([][]token, 0, countTokenSegments(tokens))
	var current []token

	flush := func() {
		segments = append(segments, current)
		current = nil
	}

	for _, t := range tokens {
		if t.Kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.Text); i++ {
			if t.Text[i] != '/' {
				continue
			}
			if i > start {
				current = append(current, token{Kind: tokenLiteral, Text: t.Text[start:i]})
			}
			flush()
			start = i + 1
		}
		if start < len(t.Text) {
			current = append(current, token{Kind: tokenLiteral, Text: t.Text[start:]})
		}
	}

	flush()
	return segments
}

func countTokenSegments(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.Kind != tokenLiteral {
			continue
		}
		count += strings.Count(t.Text, "/")
	}
	return count
}

func makeSegmentPatterns(segments [][]token) ([]segmentPattern, []captureMeta) {
	patterns := make([]segmentPattern, len(segments))
	var captures []captureMeta
	for i := range segments {
		var capture string
		patterns[i], capture = makeSegment(segments[i])
		if capture != "" {
			captures = append(captures, captureMeta{index: uint32(i), name: capture})
		}
	}
	return patterns, captures
}

func firstDefinitelyStaticSegment(patterns []segmentPattern) (string, bool) {
	if len(patterns) == 0 {
		return "", false
	}

	// Absolute routes all start with the same empty segment, so the next segment
	// is the first useful discriminator.
	index := 0
	if len(patterns) > 1 && patterns[0].literal && patterns[0].raw == "" {
		index = 1
	}

	pattern := patterns[index]
	if !pattern.literal || pattern.raw == "" {
		return "", false
	}
	return pattern.raw, true
}

func hasCatchAll(patterns []segmentPattern) bool {
	for i := range patterns {
		if patterns[i].catchAll {
			return true
		}
	}
	return false
}

func sameSegmentPattern(a, b segmentPattern) bool {
	return a.raw == b.raw &&
		a.literal == b.literal &&
		a.catchAll == b.catchAll &&
		a.prefix == b.prefix &&
		a.suffix == b.suffix &&
		a.param == b.param
}

func parseRoute(route string) ([]token, string, error) {
	tokens, err := patternscan.Scan(route, &routeScanOptions)
	if err != nil {
		return nil, "", err
	}
	return tokens, normalizedRoute(tokens, len(route)), nil
}

func normalizedRoute(tokens []token, routeLen int) string {
	var normalized strings.Builder
	normalized.Grow(routeLen + 8)
	paramOrdinal := 0
	for _, t := range tokens {
		switch t.Kind {
		case tokenLiteral:
			normalized.WriteByte('L')
			normalized.WriteString(strconv.Itoa(len(t.Text)))
			normalized.WriteByte(':')
			normalized.WriteString(t.Text)
		case tokenParam:
			normalized.WriteByte('P')
			normalized.WriteString(strconv.Itoa(paramOrdinal))
			normalized.WriteByte(';')
			paramOrdinal++
		case tokenCatchAll:
			normalized.WriteByte('C')
			normalized.WriteString(strconv.Itoa(paramOrdinal))
			normalized.WriteByte(';')
		}
	}
	return normalized.String()
}

func unescapeBraces(s string) string {
	return patternscan.UnescapeBraces(s)
}

type segmentPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}

func makeSegment(tokens []token) (segmentPattern, string) {
	var s segmentPattern
	var b strings.Builder
	var capture string
	for _, t := range tokens {
		switch t.Kind {
		case tokenLiteral:
			b.WriteString(t.Text)
			if !s.param && !s.catchAll {
				s.prefix += t.Text
			} else {
				s.suffix += t.Text
			}
		case tokenParam:
			s.param = true
			capture = t.Text
		case tokenCatchAll:
			s.catchAll = true
			capture = t.Text
		}
	}
	s.raw = b.String()
	s.literal = !s.param && !s.catchAll
	return s, capture
}
