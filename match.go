package match

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
