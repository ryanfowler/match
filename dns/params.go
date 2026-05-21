package dns

import match "github.com/ryanfowler/match"

// Param is one captured hostname parameter.
type Param = match.Param

// Params stores captured hostname parameters in pattern order.
type Params = match.Params

// NewParams returns an empty Params value with room for capacity parameters.
func NewParams(capacity int) Params {
	return match.NewParams(capacity)
}

// ParamsOf returns a Params value containing params in the same order.
func ParamsOf(params ...Param) Params {
	return match.ParamsOf(params...)
}

// Merge returns a Params value containing a followed by b.
func Merge(a, b Params) Params {
	return match.Merge(a, b)
}
