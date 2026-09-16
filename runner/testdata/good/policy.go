package good

type Status string

const Passed Status = "passed"

//levenshtein:record
type record struct {
	Status Status
	Value  int
}

func assembled() record {
	value := 1
	return record{Status: Passed, Value: value}
}
