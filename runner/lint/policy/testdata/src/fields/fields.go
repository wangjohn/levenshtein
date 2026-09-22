package fields

type Pair struct {
	Left, Right string // want "declare each struct field on its own line"
}

type Separate struct {
	Left  string
	Right string
}

type Embedded struct {
	Pair
	Note string
}

type Tagged struct {
	First, Second int `json:"-"` // want "declare each struct field on its own line"
}

func anonymous() any {
	return struct {
		A, B int // want "declare each struct field on its own line"
	}{}
}

func signatures(a, b string) (first, second int) {
	_ = a
	_ = b
	return 0, 0
}
