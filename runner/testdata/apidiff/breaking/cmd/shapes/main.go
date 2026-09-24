// Command shapes prints an area. Removing its exported Run breaks no importer.
package main

import (
	"fmt"

	"example.com/apidiff/internal/calc"
)

func main() {
	fmt.Println(calc.Twice(2))
}
