package bad

import "errors"

// S1002: comparing a boolean against a boolean constant says nothing extra.
func Simple(ready bool) int {
	if ready == true {
		return 1
	}
	return 0
}

// ST1005: an error string is a fragment, not a sentence.
func Capitalized() error {
	return errors.New("Cannot open the file")
}

// QF1011: the declared type repeats what the initializer already says.
func Quickfix() int {
	var count int = 1
	return count
}
