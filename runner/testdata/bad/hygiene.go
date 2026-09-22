package bad

import (
	"errors"
	"fmt"
	"net/http"
)

// errname: a sentinel error has to be named after the error it reports.
var notFound = errors.New("not found")

// Missing reports the sentinel so the declaration is not dead code.
func Missing() error {
	return notFound
}

// intrange: a counting loop reads better as a range over an integer.
func Intrange() int {
	total := 0
	for i := 0; i < 10; i++ {
		total += i
	}
	return total
}

// usestdlibvars: the method name has a standard-library constant.
func Usestdlibvars(request *http.Request) bool {
	return request.Method == "GET"
}

// perfsprint: formatting one integer is slower than converting it.
func Perfsprint(count int) string {
	return fmt.Sprintf("%d", count)
}

// predeclared: shadowing a builtin hides it for the whole function.
func Predeclared(len int) int {
	return len
}
