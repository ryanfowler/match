package match

import (
	"strconv"
	"strings"
)

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
		if t.kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.text); i++ {
			if t.text[i] != '/' {
				continue
			}
			if i > start {
				current = append(current, token{kind: tokenLiteral, text: t.text[start:i]})
			}
			flush()
			start = i + 1
		}
		if start < len(t.text) {
			current = append(current, token{kind: tokenLiteral, text: t.text[start:]})
		}
	}

	flush()
	return segments
}

func countTokenSegments(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.kind != tokenLiteral {
			continue
		}
		count += strings.Count(t.text, "/")
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
	tokens := make([]token, 0, countRouteTokens(route))
	var normalized strings.Builder
	var literal strings.Builder
	normalized.Grow(len(route) + 8)
	literal.Grow(len(route))
	paramsInSegment := 0
	paramOrdinal := 0

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		text := literal.String()
		tokens = append(tokens, token{kind: tokenLiteral, text: text})
		normalized.WriteByte('L')
		normalized.WriteString(strconv.Itoa(len(text)))
		normalized.WriteByte(':')
		normalized.WriteString(text)
		literal.Reset()
	}

	for i := 0; i < len(route); {
		switch route[i] {
		case '/':
			literal.WriteByte('/')
			paramsInSegment = 0
			i++
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flushLiteral()
			end, err := findParamEnd(route, i+1)
			if err != nil {
				return nil, "", err
			}
			name := unescapeBraces(route[i+1 : end])
			if name == "" {
				return nil, "", ErrInvalidParam
			}
			paramsInSegment++
			if paramsInSegment > 1 {
				return nil, "", ErrInvalidParamSegment
			}
			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return nil, "", ErrInvalidParam
				}
				if end+1 != len(route) {
					return nil, "", ErrInvalidCatchAll
				}
				tokens = append(tokens, token{kind: tokenCatchAll, text: name})
				normalized.WriteByte('C')
				normalized.WriteString(strconv.Itoa(paramOrdinal))
				normalized.WriteByte(';')
			} else {
				tokens = append(tokens, token{kind: tokenParam, text: name})
				normalized.WriteByte('P')
				normalized.WriteString(strconv.Itoa(paramOrdinal))
				normalized.WriteByte(';')
				paramOrdinal++
			}
			i = end + 1
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return nil, "", ErrInvalidParam
		default:
			literal.WriteByte(route[i])
			i++
		}
	}
	flushLiteral()

	return tokens, normalized.String(), nil
}

func countRouteTokens(route string) int {
	count := 1
	for i := 0; i < len(route); i++ {
		switch route[i] {
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				i++
				continue
			}
			count += 2
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				i++
			}
		}
	}
	return count
}

func findParamEnd(route string, start int) (int, error) {
	for i := start; i < len(route); i++ {
		switch route[i] {
		case '{':
			if i+1 < len(route) && route[i+1] == '{' {
				i++
				continue
			}
			return 0, ErrInvalidParam
		case '}':
			if i+1 < len(route) && route[i+1] == '}' {
				i++
				continue
			}
			if i == start || route[i-1] == '*' {
				return 0, ErrInvalidParam
			}
			return i, nil
		case '/':
			return 0, ErrInvalidParam
		case '*':
			if i != start {
				return 0, ErrInvalidParam
			}
			if i+1 == len(route) || route[i+1] == '}' {
				return 0, ErrInvalidParam
			}
			continue
		}
	}
	return 0, ErrInvalidParam
}

func unescapeBraces(s string) string {
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
			var b strings.Builder
			b.Grow(len(s) - 1)
			b.WriteString(s[:i])
			for ; i < len(s); i++ {
				if i+1 < len(s) && ((s[i] == '{' && s[i+1] == '{') || (s[i] == '}' && s[i+1] == '}')) {
					b.WriteByte(s[i])
					i++
					continue
				}
				b.WriteByte(s[i])
			}
			return b.String()
		}
	}
	return s
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
		switch t.kind {
		case tokenLiteral:
			b.WriteString(t.text)
			if !s.param && !s.catchAll {
				s.prefix += t.text
			} else {
				s.suffix += t.text
			}
		case tokenParam:
			s.param = true
			capture = t.text
		case tokenCatchAll:
			s.catchAll = true
			capture = t.text
		}
	}
	s.raw = b.String()
	s.literal = !s.param && !s.catchAll
	return s, capture
}
