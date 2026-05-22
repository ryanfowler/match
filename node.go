package match

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidParamSegment reports a route segment that contains more than
	// one parameter.
	ErrInvalidParamSegment = errors.New("only one parameter is allowed per path segment")

	// ErrInvalidParam reports malformed parameter syntax or an invalid
	// parameter name.
	ErrInvalidParam = errors.New("parameters must be registered with a valid name")

	// ErrInvalidCatchAll reports a catch-all parameter that is not the final
	// token in its route.
	ErrInvalidCatchAll = errors.New("catch-all parameters are only allowed at the end of a route")
)

// ConflictError reports a route that cannot be inserted because it overlaps an
// already registered route.
type ConflictError struct {
	// Route is the route that failed to insert.
	Route string

	// With is the previously registered route that conflicts with Route.
	With string
}

// Error returns a human-readable description of the route conflict.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("insertion failed due to conflict with previously registered route: %s", e.With)
}

type tokenKind uint8

const (
	tokenLiteral tokenKind = iota
	tokenParam
	tokenCatchAll
)

type token struct {
	kind tokenKind
	text string
}

type routeEntry[T any] struct {
	route                 string
	patterns              []segmentPattern
	captures              []captureMeta
	segmentCount          int
	order                 int
	firstStaticSegment    string
	hasFirstStaticSegment bool
	hasCatchAll           bool
	value                 T
}

type captureMeta struct {
	index uint32
	name  string
}

type node[T any] struct {
	routes                []*routeEntry[T]
	exactStatic           map[string]*routeEntry[T]
	maxExactStaticPathLen int
	fastRoot              simpleRadixNode[T]
	hasComplexParams      bool
	hasSimpleDynamic      bool
	maxSimpleCaptureCount int
	normalized            map[string]string
	conflictIndex         routeConflictIndex[T]
	root                  segmentNode[T]
	absoluteRoot          *segmentNode[T]
	rootPrefix            *routeEntry[T]
}

type routeConflictIndex[T any] struct {
	bySegmentCount map[int]*routeConflictBucket[T]
	catchAll       routeConflictBucket[T]
}

type routeConflictBucket[T any] struct {
	all      []*routeEntry[T]
	static   map[string][]*routeEntry[T]
	wildcard []*routeEntry[T]
}

type segmentNode[T any] struct {
	static        []staticEdge[T]
	staticIndex   map[string]*segmentNode[T]
	plainParam    *paramEdge[T]
	affixedParams []paramEdge[T]
	catchAll      []catchAllEdge[T]
	value         *routeEntry[T]
}

type staticEdge[T any] struct {
	segment string
	child   *segmentNode[T]
}

type paramEdge[T any] struct {
	pattern segmentPattern
	child   *segmentNode[T]
}

type catchAllEdge[T any] struct {
	pattern segmentPattern
	route   *routeEntry[T]
}
