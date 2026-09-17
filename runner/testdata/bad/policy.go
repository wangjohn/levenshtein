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
