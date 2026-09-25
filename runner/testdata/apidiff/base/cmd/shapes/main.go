// Command shapes prints an area. Nothing can import a command.
package main

import (
	"fmt"

	"example.com/apidiff/internal/calc"
)

// Run prints the area of a square.
func Run() {
	fmt.Println(calc.Twice(2))
}

func main() {
	Run()
}
