package bad

import "fmt"

// testableexamples: without an Output comment, go test compiles the example
// but never runs it, so it cannot fail.
func ExampleBadCond() {
	fmt.Println(BadCond(7))
}
