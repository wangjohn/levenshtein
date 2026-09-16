package records

//levenshtein:record
type Result struct { // want Result:"record"
	Count   int
	Message string
	Nested  struct{ Value int }
}

type Alias = Result
type Mutable struct{ Count int }
type Embedded struct{ Result }

func build() Result {
	r := Result{Count: 1}
	r.Count = 2                       // want "construct Result with a struct literal"
	r.Count++                         // want "construct Result with a struct literal"
	r.Nested.Value = 1                // want "construct Result with a struct literal"
	r.Count, r.Message = 3, "message" // want "construct Result with a struct literal" "construct Result with a struct literal"
	_ = &r.Count                      // want "construct Result with a struct literal"
	p := &r
	p.Count = 4 // want "construct Result with a struct literal"
	var e Embedded
	e.Count = 9 // want "construct Result with a struct literal"
	var a Alias
	a.Count = 5                    // want "construct Result with a struct literal"
	for r.Count = range []int{1} { // want "construct Result with a struct literal"
	}
	return Result{Count: r.Count, Message: "complete"}
}

func allowed() {
	var m Mutable
	m.Count++
	_ = &Result{Count: 1}
	var decoded Result
	consume(&decoded) // Decoding/callee effects are outside this syntactic rule.
	decoded = Result{Count: 2}
}

func consume(*Result) {}

//levenshtein:record
type Wrong int // want "levenshtein:record requires a struct type"

//levenshtein:record
type WrongAlias = Result // want "levenshtein:record must mark the original type"

func localMarker() {
	//levenshtein:record
	type Local struct{ N int } // want "levenshtein:record requires a package-level struct type"
}
