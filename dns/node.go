package dns

import (
	"errors"
	"fmt"
	match "github.com/ryanfowler/match"

	"github.com/ryanfowler/match/internal/conflictindex"
)

const (
	maxHostnameLen = 253
	maxLabelLen    = 63

	// inlineParamCapacity mirrors match.Params inline storage capacity so DNS
	// can pre-grow only when a pattern exceeds the inline capture buffer.
	inlineParamCapacity = 4

	// staticChildMapThreshold is the static-label fanout where a trie node
	// builds exact and folded lookup maps instead of relying only on slices.
	staticChildMapThreshold = 9
)

var (
	// ErrInvalidHostname reports malformed dot-separated hostname structure.
	ErrInvalidHostname = errors.New("hostnames must contain non-empty labels")

	// ErrInvalidParamLabel reports a pattern label that contains more than one
	// parameter.
	ErrInvalidParamLabel = errors.New("only one parameter is allowed per hostname label")

	// ErrInvalidParam reports malformed parameter syntax or an invalid
	// parameter name.
	ErrInvalidParam = match.ErrInvalidParam

	// ErrInvalidCatchAll reports a catch-all parameter outside the leftmost
	// pattern label.
	ErrInvalidCatchAll = errors.New("catch-all parameters are only allowed at the start of a hostname pattern")
)

// ConflictError reports a pattern that cannot be inserted because it overlaps
// an already registered pattern.
type ConflictError struct {
	// Pattern is the pattern that failed to insert.
	Pattern string

	// With is the previously registered pattern that conflicts with Pattern.
	With string
}

// Error returns a human-readable description of the pattern conflict.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("insertion failed due to conflict with previously registered pattern: %s", e.With)
}

type routeEntry[T any] struct {
	pattern             string
	labels              []labelPattern
	captures            []captureMeta
	labelCount          int
	order               int
	firstStaticLabel    string
	hasFirstStaticLabel bool
	hasCatchAll         bool
	value               T
}

type captureMeta struct {
	index uint32
	name  string
}

type node[T any] struct {
	routes                []*routeEntry[T]
	exactStatic           map[string]*routeEntry[T]
	maxExactStaticHostLen int
	hasDynamic            bool
	normalized            map[string]string
	conflictIndex         routeConflictIndex[T]
	root                  labelNode[T]
}

type routeConflictIndex[T any] struct {
	index conflictindex.Index[*routeEntry[T]]
}

type labelNode[T any] struct {
	static          []staticEdge[T]
	staticIndex     map[string]*labelNode[T]
	staticFoldIndex map[foldedLabelKey]*labelNode[T]
	params          []paramEdge[T]
	catchAll        []catchAllEdge[T]
	value           *routeEntry[T]
}

type staticEdge[T any] struct {
	label string
	child *labelNode[T]
}

type paramEdge[T any] struct {
	pattern labelPattern
	child   *labelNode[T]
}

type catchAllEdge[T any] struct {
	pattern labelPattern
	route   *routeEntry[T]
}

type foldedLabelKey struct {
	n     uint8
	bytes [maxLabelLen]byte
}

type labelPattern struct {
	raw      string
	literal  bool
	catchAll bool
	prefix   string
	suffix   string
	param    bool
}
