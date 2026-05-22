package dns

import (
	"strconv"
	"strings"

	"github.com/ryanfowler/match/internal/patternscan"
)

var patternScanOptions = patternscan.Options{
	Separator:           '.',
	CatchAllRule:        patternscan.CatchAllInFirstPart,
	ErrInvalidParam:     ErrInvalidParam,
	ErrInvalidParamPart: ErrInvalidParamLabel,
	ErrInvalidCatchAll:  ErrInvalidCatchAll,
}

func parsePattern(pattern string) ([]labelPattern, []captureMeta, string, error) {
	canonicalPattern := trimRootDot(pattern)
	if canonicalPattern == "" {
		return nil, nil, "", ErrInvalidHostname
	}

	if !containsBrace(canonicalPattern) {
		if len(canonicalPattern) > maxHostnameLen {
			return nil, nil, "", ErrInvalidHostname
		}
		labels, err := literalLabelPatterns(canonicalPattern)
		if err != nil {
			return nil, nil, "", err
		}
		return labels, nil, canonicalPattern, nil
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

func containsBrace(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '}' {
			return true
		}
	}
	return false
}

func literalLabelPatterns(pattern string) ([]labelPattern, error) {
	labels := make([]labelPattern, 0, strings.Count(pattern, ".")+1)
	start := 0
	for i := 0; i <= len(pattern); i++ {
		if i < len(pattern) && pattern[i] != '.' {
			continue
		}
		if i == start || i-start > maxLabelLen {
			return nil, ErrInvalidHostname
		}
		label := lowerASCII(pattern[start:i])
		labels = append(labels, labelPattern{raw: label, literal: true})
		start = i + 1
	}
	return labels, nil
}

func parsePatternTokens(pattern string) ([]token, error) {
	return patternscan.Scan(pattern, &patternScanOptions)
}

func splitTokenLabels(tokens []token) [][]token {
	labels := make([][]token, 0, countTokenLabels(tokens))
	var current []token

	flush := func() {
		labels = append(labels, current)
		current = nil
	}

	for _, t := range tokens {
		if t.Kind != tokenLiteral {
			current = append(current, t)
			continue
		}

		start := 0
		for i := 0; i < len(t.Text); i++ {
			if t.Text[i] != '.' {
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
	return labels
}

func countTokenLabels(tokens []token) int {
	count := 1
	for _, t := range tokens {
		if t.Kind == tokenLiteral {
			count += strings.Count(t.Text, ".")
		}
	}
	return count
}

func makeLabel(tokens []token) (labelPattern, string) {
	var p labelPattern
	var b strings.Builder
	var capture string
	for _, t := range tokens {
		switch t.Kind {
		case tokenLiteral:
			text := lowerASCII(t.Text)
			b.WriteString(text)
			if !p.param && !p.catchAll {
				p.prefix += text
			} else {
				p.suffix += text
			}
		case tokenParam:
			p.param = true
			capture = t.Text
		case tokenCatchAll:
			p.catchAll = true
			capture = t.Text
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
	return patternscan.UnescapeBraces(s)
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
