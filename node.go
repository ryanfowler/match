package match

type node[T any] struct {
	priority int
}

func (n *node[T]) insert(route string, value T) error {
	return nil
}

func (n *node[T]) match(route string) (T, Params, bool) {
	var val T
	return val, Params{}, false
}
