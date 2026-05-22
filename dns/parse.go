package dns

import (
	"strconv"
	"strings"
)

func parsePattern(pattern string) ([]labelPattern, []captureMeta, string, error) {
	canonicalPattern := trimRootDot(pattern)
	if canonicalPattern == "" {
		return nil, nil, "", ErrInvalidHostname
	}

	tokens, err := parsePatternTokens(canonicalPattern)
	if err != nil {
		return nil, nil, "", err
	}

	labelTokens := splitTokenLabels(tokens)
	labels := make([]labelPattern, len(labelTokens))
	var captures []captureMeta

	for i := range labelTokens {
		if len(labelTokens[i]) == 0 {
			return nil, nil, "", ErrInvalidHostname
		}

		var capture string
		labels[i], capture = makeLabel(labelTokens[i])
		if err := validateLabelPattern(labels[i]); err != nil {
			return nil, nil, "", err
		}
		if labels[i].catchAll && i != 0 {
			return nil, nil, "", ErrInvalidCatchAll
		}
		if capture != "" {
			captures = append(captures, captureMeta{index: uint32(i), name: capture})
		}
	}

	if minHostnameLength(labels) > maxHostnameLen {
		return nil, nil, "", ErrInvalidHostname
	}

	return labels, captures, unescapeBraces(canonicalPattern), nil
}

func parsePatternTokens(pattern string) ([]token, error) {
	tokens := make([]token, 0, countPatternTokens(pattern))
	var literal strings.Builder
	literal.Grow(len(pattern))
	paramsInLabel := 0
	labelIndex := 0

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		tokens = append(tokens, token{kind: tokenLiteral, text: literal.String()})
		literal.Reset()
	}

	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '.':
			literal.WriteByte('.')
			paramsInLabel = 0
			labelIndex++
			i++
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flushLiteral()
			end, err := findParamEnd(pattern, i+1)
			if err != nil {
				return nil, err
			}
			name := unescapeBraces(pattern[i+1 : end])
			if name == "" {
				return nil, ErrInvalidParam
			}
			paramsInLabel++
			if paramsInLabel > 1 {
				return nil, ErrInvalidParamLabel
			}
			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return nil, ErrInvalidParam
				}
				if labelIndex != 0 || (end+1 < len(pattern) && pattern[end+1] != '.') {
					return nil, ErrInvalidCatchAll
				}
				tokens = append(tokens, token{kind: tokenCatchAll, text: name})
			} else {
				tokens = append(tokens, token{kind: tokenParam, text: name})
			}
			i = end + 1
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return nil, ErrInvalidParam
		default:
			literal.WriteByte(pattern[i])
			i++
		}
	}
	flushLiteral()

	return tokens, nil
}

func countPatternTokens(pattern string) int {
	count := 1
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
			count += 2
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
			}
		}
	}
	return count
}

func findParamEnd(pattern string, start int) (int, error) {
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
			return 0, ErrInvalidParam
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
				continue
			}
			if i == start || pattern[i-1] == '*' {
				return 0, ErrInvalidParam
			}
			return i, nil
		case '.':
			return 0, ErrInvalidParam
		case '*':
			if i != start {
				return 0, ErrInvalidParam
			}
			if i+1 == len(pattern) || pattern[i+1] == '}' {
				return 0, ErrInvalidParam
			}
			continue
		}
	}
	return 0, ErrInvalidParam
}

func splitTokenLabels(tokens []token) [][]token {
	labels := make([][]token, 0, countTokenLabels(tokens))
	var current []token

	flush := func() {
		labels = append(labels, current)
		current = nil
	}

	for _, t := range tokens {
		if t.kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.text); i++ {
			if t.text[i] != '.' {
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
	return labels
}

func countTokenLabels(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.kind == tokenLiteral {
			count += strings.Count(t.text, ".")
		}
	}
	return count
}

func makeLabel(tokens []token) (labelPattern, string) {
	var p labelPattern
	var b strings.Builder
	var capture string
	for _, t := range tokens {
		switch t.kind {
		case tokenLiteral:
			text := lowerASCII(t.text)
			b.WriteString(text)
			if !p.param && !p.catchAll {
				p.prefix += text
			} else {
				p.suffix += text
			}
		case tokenParam:
			p.param = true
			capture = t.text
		case tokenCatchAll:
			p.catchAll = true
			capture = t.text
		}
	}
	p.raw = b.String()
	p.literal = !p.param && !p.catchAll
	return p, capture
}

func validateLabelPattern(pattern labelPattern) error {
	switch {
	case pattern.literal:
		if pattern.raw == "" || len(pattern.raw) > maxLabelLen {
			return ErrInvalidHostname
		}
	case pattern.catchAll:
		if len(pattern.prefix) >= maxLabelLen || pattern.suffix != "" {
			return ErrInvalidHostname
		}
	case pattern.param:
		if len(pattern.prefix)+len(pattern.suffix) >= maxLabelLen {
			return ErrInvalidHostname
		}
	}
	return nil
}

func minHostnameLength(labels []labelPattern) int {
	if len(labels) == 0 {
		return 0
	}

	length := len(labels) - 1
	for i := range labels {
		p := labels[i]
		if p.literal {
			length += len(p.raw)
			continue
		}
		length += len(p.prefix) + len(p.suffix) + 1
	}
	return length
}

func normalizedLabels(labels []labelPattern) string {
	var b strings.Builder
	for i := range labels {
		b.WriteByte('.')
		p := labels[i]
		if p.literal {
			writeNormalizedPart(&b, 'L', p.raw)
			continue
		}
		if p.catchAll {
			writeNormalizedPart(&b, 'C', p.prefix)
			continue
		}
		writeNormalizedPart(&b, 'P', p.prefix)
		writeNormalizedPart(&b, 'S', p.suffix)
	}
	return b.String()
}

func writeNormalizedPart(b *strings.Builder, kind byte, text string) {
	b.WriteByte(kind)
	b.WriteString(strconv.Itoa(len(text)))
	b.WriteByte(':')
	b.WriteString(text)
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

func hasCatchAll(labels []labelPattern) bool {
	for i := range labels {
		if labels[i].catchAll {
			return true
		}
	}
	return false
}

func firstDefinitelyStaticLabel(labels []labelPattern) (string, bool) {
	if len(labels) == 0 || !labels[0].literal {
		return "", false
	}
	return labels[0].raw, true
}

func sameLabelPattern(a, b labelPattern) bool {
	return a.raw == b.raw &&
		a.literal == b.literal &&
		a.catchAll == b.catchAll &&
		a.prefix == b.prefix &&
		a.suffix == b.suffix &&
		a.param == b.param
}
