package match

import "errors"

var ErrNotFound = errors.New("matching route not found")

type MatchResult[T any] struct {
	Value  T
	Params Params
}

type Router[T any] struct {
	root node[T]
}

func (r *Router[T]) Insert(route string, value T) {
	if err := r.TryInsert(route, value); err != nil {
		panic(err)
	}
}

func (r *Router[T]) TryInsert(route string, value T) error {
	return r.root.insert(route, value)
}

func (r *Router[T]) Match(route string) (T, Params, bool) {
	return r.root.match(route)
}

func (r *Router[T]) MatchInto(route string, params Params) (T, Params, bool) {
	return r.root.matchInto(route, params)
}

func (r *Router[T]) At(route string) (MatchResult[T], error) {
	value, params, ok := r.Match(route)
	if !ok {
		var zero MatchResult[T]
		return zero, ErrNotFound
	}
	return MatchResult[T]{Value: value, Params: params}, nil
}
