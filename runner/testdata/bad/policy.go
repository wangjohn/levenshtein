package bad

type untyped struct {
	Priority string
}

type record struct{ Value int }

func scattered() record {
	r := record{}
	r.Value = 1
	return r
}

func choose(value untyped) {
	switch value.Priority {
	case "high", "low":
	}
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

// LV1003: two fields share one declaration.
type pair struct {
	left, right string
}

// LV1004: the next declaration has no blank line before it.
type adjacent struct {
	Value int
}
type follower struct {
	Value int
}
