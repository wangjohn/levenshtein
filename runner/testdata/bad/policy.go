package bad

type untyped struct {
	Status string
}

//levenshtein:record
type record struct{ Value int }

func scattered() record {
	r := record{}
	r.Value = 1
	return r
}

// Deliberately omit a declared enum member.
type choice int

const (
	first choice = iota
	second
)

func incomplete(value choice) bool {
	switch value {
	case first:
		return true
	}
	return false
}
