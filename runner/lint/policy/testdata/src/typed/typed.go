package typed

type Status string

const Passed Status = "passed"
const Failed Status = "failed"
const raw = "passed"

type Alias = string

type Bad struct {
	Status string // want "field Status needs a defined string type"
	kind   Alias  // want "field kind needs a defined string type"
}

type Result struct {
	Status  Status
	Message string
	Path    string
}

func check(r Result, input string) Status {
	_ = Result{Status: "passed"} // want "use a typed constant"
	_ = Result{Status: Passed, Message: "free text"}
	_ = Result{Status: ""}    // The zero value is not an enum alternative.
	if r.Status == "passed" { // want "use a typed constant"
	}
	if r.Status == Passed {
	}
	switch r.Status {
	case "failed": // want "use a typed constant"
	case Passed:
	}
	r.Status = "passed"     // want "use a typed constant"
	var s Status = "failed" // want "use a typed constant"
	_ = s
	_ = Status("passed")    // want "use a typed constant"
	_ = Status(input)       // Dynamic boundary conversion is allowed.
	_ = Result{Status: raw} // want "use a typed constant"
	return "passed"         // want "use a typed constant"
}
