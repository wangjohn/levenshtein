package good

import (
	"fmt"
	"strings"
)

// The Output comment makes go test run the example and compare what it prints,
// so testableexamples passes it.
func ExampleCountLines() {
	lines, err := CountLines(strings.NewReader("one\ntwo\n"))
	fmt.Println(lines, err)
	// Output: 2 <nil>
}
