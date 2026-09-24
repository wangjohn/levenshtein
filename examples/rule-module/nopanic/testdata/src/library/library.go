package library

import "errors"

func Parse(input string) int {
	if input == "" {
		panic("empty input") // want `Parse panics; return an error so callers can handle the failure`
	}
	return len(input)
}

func ParseChecked(input string) (int, error) {
	if input == "" {
		return 0, errors.New("empty input")
	}
	return len(input), nil
}

func MustParse(input string) int {
	count, err := ParseChecked(input)
	if err != nil {
		panic(err)
	}
	return count
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func Mustard(input string) int {
	if input == "" {
		panic("not a Must function") // want `Mustard panics; return an error so callers can handle the failure`
	}
	return len(input)
}

type Parser struct {
	strict bool
}

func (p Parser) init() {
	if p.strict {
		panic("a method named init is not package initialization") // want `init panics; return an error so callers can handle the failure`
	}
}

func (p Parser) Each(inputs []string) {
	for _, input := range inputs {
		func() {
			if input == "" {
				panic("inside a closure") // want `Each panics; return an error so callers can handle the failure`
			}
		}()
	}
}

var fallback = func() int {
	panic("at package level") // want `package-level code panics; return an error so callers can handle the failure`
}

func init() {
	if MustParse("ready") == 0 {
		panic("unreachable")
	}
}
