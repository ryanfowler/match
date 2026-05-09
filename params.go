package match

import "iter"

// Param is one captured route parameter.
type Param struct {
	// Key is the parameter name from the matched route.
	Key string

	// Val is the substring captured from the matched path.
	Val string
}

const inlineParams = 4

// Params stores captured route parameters in route order.
//
// Params is an opaque value type. Use Len and At to inspect captures without
// allocation, Get or TryGet to look up a named capture, and AppendTo or All
// when a []Param snapshot is needed.
type Params struct {
	len    int
	inline [inlineParams]Param
	heap   []Param
}

// NewParams returns an empty Params value with room for capacity parameters.
//
// It is most useful with Router.MatchInto when callers want to reuse storage
// across matches.
func NewParams(capacity int) Params {
	if capacity <= inlineParams {
		return Params{}
	}
	return Params{heap: make([]Param, 0, capacity)}
}

// ParamsOf returns a Params value containing params in the same order.
func ParamsOf(params ...Param) Params {
	p := NewParams(len(params))
	for i := range params {
		p = p.append(params[i].Key, params[i].Val)
	}
	return p
}

// Len returns the number of captured parameters.
func (p Params) Len() int {
	return p.len
}

// At returns the parameter at index i.
//
// It panics if i is outside the range [0, Len()).
func (p Params) At(i int) Param {
	if i < 0 || i >= p.len {
		panic("match: parameter index out of range")
	}
	return p.at(i)
}

func (p Params) at(i int) Param {
	if p.heap != nil {
		return p.heap[i]
	}
	return p.inline[i]
}

// AppendTo appends the captured parameters to dst and returns the extended slice.
func (p Params) AppendTo(dst []Param) []Param {
	if p.heap != nil {
		return append(dst, p.heap...)
	}
	return append(dst, p.inline[:p.len]...)
}

// All returns a new slice containing the captured parameters.
func (p Params) All() []Param {
	return p.AppendTo(make([]Param, 0, p.len))
}

// Seq returns an iterator over captured parameter keys and values in route order.
func (p Params) Seq() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for i := 0; i < p.len; i++ {
			param := p.at(i)
			if !yield(param.Key, param.Val) {
				return
			}
		}
	}
}

// Get returns the value for key, or an empty string when key was not captured.
func (p Params) Get(key string) string {
	val, _ := p.TryGet(key)
	return val
}

// TryGet returns the value for key and whether key was captured.
func (p Params) TryGet(key string) (string, bool) {
	for i := 0; i < p.len; i++ {
		param := p.at(i)
		if param.Key == key {
			return param.Val, true
		}
	}
	return "", false
}

func (p Params) reset() Params {
	p.len = 0
	if p.heap != nil {
		p.heap = p.heap[:0]
	}
	return p
}

func (p Params) append(key, val string) Params {
	if p.heap != nil {
		p.heap = append(p.heap, Param{Key: key, Val: val})
		p.len = len(p.heap)
		return p
	}

	if p.len < len(p.inline) {
		p.inline[p.len] = Param{Key: key, Val: val}
		p.len++
		return p
	}

	heap := make([]Param, p.len, p.len*2)
	copy(heap, p.inline[:p.len])
	heap = append(heap, Param{Key: key, Val: val})
	p.heap = heap
	p.len = len(heap)
	return p
}
