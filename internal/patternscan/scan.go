package patternscan

import "strings"

type TokenKind uint8

const (
	TokenLiteral TokenKind = iota
	TokenParam
	TokenCatchAll
)

type Token struct {
	Kind TokenKind
	Text string
}

type CatchAllRule uint8

const (
	CatchAllAtEnd CatchAllRule = iota
	CatchAllInFirstPart
)

type Options struct {
	Separator           byte
	CatchAllRule        CatchAllRule
	ErrInvalidParam     error
	ErrInvalidParamPart error
	ErrInvalidCatchAll  error
}

func Scan(pattern string, opts *Options) ([]Token, error) {
	tokens := make([]Token, 0, countTokens(pattern))
	var literal strings.Builder
	literal.Grow(len(pattern))
	paramsInPart := 0
	partIndex := 0

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		tokens = append(tokens, Token{Kind: TokenLiteral, Text: literal.String()})
		literal.Reset()
	}

	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case opts.Separator:
			literal.WriteByte(opts.Separator)
			paramsInPart = 0
			partIndex++
			i++
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flushLiteral()
			end, err := findParamEnd(pattern, i+1, opts.Separator, opts.ErrInvalidParam)
			if err != nil {
				return nil, err
			}
			name := UnescapeBraces(pattern[i+1 : end])
			if name == "" {
				return nil, opts.ErrInvalidParam
			}
			paramsInPart++
			if paramsInPart > 1 {
				return nil, opts.ErrInvalidParamPart
			}
			if name[0] == '*' {
				name = name[1:]
				if name == "" {
					return nil, opts.ErrInvalidParam
				}
				if !validCatchAll(pattern, end, partIndex, opts) {
					return nil, opts.ErrInvalidCatchAll
				}
				tokens = append(tokens, Token{Kind: TokenCatchAll, Text: name})
			} else {
				tokens = append(tokens, Token{Kind: TokenParam, Text: name})
			}
			i = end + 1
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return nil, opts.ErrInvalidParam
		default:
			literal.WriteByte(pattern[i])
			i++
		}
	}
	flushLiteral()

	return tokens, nil
}

func countTokens(pattern string) int {
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

func findParamEnd(pattern string, start int, separator byte, errInvalidParam error) (int, error) {
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if i+1 < len(pattern) && pattern[i+1] == '{' {
				i++
				continue
			}
			return 0, errInvalidParam
		case '}':
			if i+1 < len(pattern) && pattern[i+1] == '}' {
				i++
				continue
			}
			if i == start || pattern[i-1] == '*' {
				return 0, errInvalidParam
			}
			return i, nil
		case separator:
			return 0, errInvalidParam
		case '*':
			if i != start {
				return 0, errInvalidParam
			}
			if i+1 == len(pattern) || pattern[i+1] == '}' {
				return 0, errInvalidParam
			}
			continue
		}
	}
	return 0, errInvalidParam
}

func validCatchAll(pattern string, end int, partIndex int, opts *Options) bool {
	switch opts.CatchAllRule {
	case CatchAllAtEnd:
		return end+1 == len(pattern)
	case CatchAllInFirstPart:
		return partIndex == 0 && (end+1 == len(pattern) || pattern[end+1] == opts.Separator)
	default:
		return false
	}
}

func UnescapeBraces(s string) string {
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
