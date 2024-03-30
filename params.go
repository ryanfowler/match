package match

type Param struct {
	Key string
	Val string
}

type Params []Param

func (p Params) Get(key string) string {
	val, _ := p.TryGet(key)
	return val
}

func (p Params) TryGet(key string) (string, bool) {
	for _, param := range p {
		if param.Key == key {
			return param.Val, true
		}
	}
	return "", false
}
