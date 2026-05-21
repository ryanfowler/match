// Package dns provides a minimal, high-performance generic matcher for DNS
// hostnames.
//
// A Router maps dot-separated hostname patterns to caller-provided values, then
// returns the matched value and any captured parameters. Patterns are matched
// right-to-left using DNS label boundaries, so shared suffixes such as
// example.com are stored once.
//
// Hostname matching is ASCII case-insensitive. A single trailing root dot is
// ignored for both insertion and matching, so example.com and example.com. are
// equivalent. The package does not parse host:port strings, perform IDNA
// conversion, or normalize Unicode; callers should provide only the hostname
// they want to match.
//
// # Quick Start
//
//	var router dns.Router[string]
//	router.Insert("example.com", "apex")
//	router.Insert("{tenant}.example.com", "tenant")
//	router.Insert("{*subdomain}.example.com", "subdomain")
//
//	value, params, ok := router.Match("api.example.com")
//	_ = value                 // "tenant"
//	_ = params.Get("tenant")  // "api"
//	_ = ok                    // true
//
// # Pattern Grammar
//
// Patterns are dot-separated labels made from literal text, named parameters,
// and left-side catch-all parameters.
//
// Literal labels match themselves case-insensitively. A named parameter is
// written as {name} and captures one non-empty label. A parameter may have
// literal text before or after it in the same label, such as
// api-{region}.example.com or {service}-prod.example.com. Each label may
// contain at most one parameter.
//
// A catch-all parameter is written as {*name}. It must appear in the leftmost
// label of the pattern and captures the non-empty leading hostname text before
// the remaining suffix labels. For example, {*sub}.example.com captures
// "a.b" from a.b.example.com. A catch-all may have a literal prefix in its
// leftmost label, such as svc-{*sub}.example.com.
//
// Literal braces are escaped by doubling them: {{ matches a literal { and }}
// matches a literal }. Escaped braces may also appear inside parameter names.
//
// # Matching Behavior
//
// When more than one pattern could match, dns chooses deterministically while
// walking labels from right to left: literal labels beat parameter labels,
// parameter labels with more literal text are tried first, and catch-all
// patterns are considered last.
//
// Match looks up an exact hostname. MatchInto is the same operation using a
// caller-provided *Params value as reusable storage. MatchSuffix and
// MatchSuffixInto return the best whole-label hostname suffix plus the
// unmatched leading prefix, which is useful for zone-style dispatch. Clone
// returns an independent copy of a Router's matching state.
package dns
