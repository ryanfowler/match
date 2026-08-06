package dns

import (
	"strings"
)

func prevHostLabel(host string, end int) (string, int, bool) {
	if end <= 0 || host[end-1] == '.' {
		return "", 0, false
	}
	i := strings.LastIndexByte(host[:end], '.')
	if end-i-1 > maxLabelLen {
		return "", 0, false
	}
	return host[i+1 : end], i, true
}

func nextHostLabel(host string, start int) (string, int, bool) {
	if start < 0 || start >= len(host) || host[start] == '.' {
		return "", 0, false
	}
	if i := strings.IndexByte(host[start:], '.'); i >= 0 {
		return host[start : start+i], start + i + 1, true
	}
	return host[start:], -1, true
}

func hostnameWithinBounds(host string) (string, bool) {
	host = trimRootDot(host)
	if host == "" || len(host) > maxHostnameLen {
		return "", false
	}
	return host, true
}

func validHostnameLabels(host string) bool {
	labelLen := 0
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			if labelLen == 0 || labelLen > maxLabelLen {
				return false
			}
			labelLen = 0
			continue
		}
		labelLen++
	}
	if labelLen == 0 || labelLen > maxLabelLen {
		return false
	}
	return true
}

func hostnamePrefix(host string, end int) string {
	if end <= 0 {
		return ""
	}
	return host[:end]
}

func suffixStart(prefixEnd int) int {
	if prefixEnd < 0 {
		return 0
	}
	return prefixEnd + 1
}

func countHostnameLabels(host string) int {
	if host == "" {
		return 0
	}
	count := 1
	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			count++
		}
	}
	return count
}

func indexBeforeRightLabels(host string, labels int) int {
	end := len(host)
	for i := 0; i < labels; i++ {
		_, next, ok := prevHostLabel(host, end)
		if !ok {
			return -1
		}
		end = next
	}
	return end
}

func trimRootDot(host string) string {
	if host != "" && host[len(host)-1] == '.' {
		return host[:len(host)-1]
	}
	return host
}

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			var b strings.Builder
			b.Grow(len(s))
			b.WriteString(s[:i])
			for ; i < len(s); i++ {
				c := s[i]
				if 'A' <= c && c <= 'Z' {
					c += 'a' - 'A'
				}
				b.WriteByte(c)
			}
			return b.String()
		}
	}
	return s
}

func asciiLower(s string) bool {
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			return false
		}
	}
	return true
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCIIByte(a[i]) != lowerASCIIByte(b[i]) {
			return false
		}
	}
	return true
}

func asciiHasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && asciiEqualFold(s[:len(prefix)], prefix)
}

func asciiHasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && asciiEqualFold(s[len(s)-len(suffix):], suffix)
}

func lowerASCIIByte(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
