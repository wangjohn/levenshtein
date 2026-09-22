package spacing

// Before and After are physically separated by blank lines. The //line
// directive renumbers what follows it, so logical line numbers would put
// After directly under Before.
type Before struct {
	Value int
}

//line lined.go:8

type After struct {
	Value int
}
