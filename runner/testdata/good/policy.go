package good

type Status string

const Passed Status = "passed"

type Record struct {
	Status Status
	Value  int
}

func Assembled() Record {
	value := 1
	return Record{Status: Passed, Value: value}
}
