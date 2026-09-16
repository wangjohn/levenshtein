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
