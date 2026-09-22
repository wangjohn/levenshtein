package spacing

import "fmt"

const Limit = 3

type First struct {
	Value int
}
type Second struct { // want "separate top-level declarations with a blank line"
	Value int
}

// Third keeps its doc comment and the blank line that precedes it.
type Third struct {
	Value int
}

// Grouped declarations are one declaration, so their members need no spacing.
const (
	Low  = 1
	High = 2
)

var one, two = 1, 2

func use() {
	fmt.Println(Limit, First{}, Second{}, Third{}, Low, High, one, two)
}
func alsoUse() { // want "separate top-level declarations with a blank line"
	use()
}
