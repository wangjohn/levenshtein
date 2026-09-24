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

func init() {
	if MustParse("ready") == 0 {
		panic("unreachable")
	}
}
