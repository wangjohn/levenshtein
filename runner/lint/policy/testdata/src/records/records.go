package records

type Result struct {
	Count   int
	Message string
	Nested  struct{ Value int }
}
type Alias = Result

func construction() Result {
	r := Result{} // want "construct r with a struct literal"
	r.Count = 1
	r.Message = "ready"
	return r
}
func zero() {
	var r Result // want "construct r with a struct literal"
	r.Nested.Value = 3
}
func pointer() {
	r := new(Result) // want "construct r with a struct literal"
	r.Count = 2
}
func anonymous() {
	r := &struct{ Count int }{} // want "construct r with a struct literal"
	r.Count = 2
}
func alias() {
	var r Alias // want "construct r with a struct literal"
	r.Count = 2
}
func updates(existing *Result) {
	existing.Count = 1
	r := Result{Count: 1}
	consume(&r)
	r.Count = 2
	r.Count++
	r.Count = r.Count + 1
	copy := r
	copy.Count = 3
}
func escaped() {
	r := Result{}
	p := &r
	p.Count = 1
	var decoded Result
	consume(&decoded)
	decoded.Count = 2
}
func complete() Result { return Result{Count: 1} }
func compound()        { var r Result; r.Count++ }
func factory()         { r := complete(); r.Count = 1 }
func consume(*Result)  {}

func conditional(flag bool) Result {
	r := Result{} // want "construct r with a struct literal"
	if flag {
		r.Count = 1
	} else {
		r.Count = 2
	}
	return r
}
func nested(flag bool) {
	if flag {
		r := Result{} // want "construct r with a struct literal"
		r.Count = 1
	}
}
func readBeforeWrite() {
	r := Result{}
	if r.Count == 0 {
		r.Count = 1
	}
}

func localType() {
	type Local struct{ N int }
	v := Local{} // want "construct v with a struct literal"
	v.N = 3
}
func parenthesized() {
	r := Result{} // want "construct r with a struct literal"
	(r).Count = 1
}
func multiple() {
	a, b := Result{}, Result{} // want "construct a with a struct literal" "construct b with a struct literal"
	a.Count = 1
	b.Count = 2
}
func captured() {
	r := Result{}
	f := func() { r.Count = 2 }
	f()
	r.Count = 3
}
func closure() {
	f := func() {
		r := Result{} // want "construct r with a struct literal"
		r.Count = 1
	}
	f()
}
func loop() {
	for i := 0; i < 2; i++ {
		r := Result{} // want "construct r with a struct literal"
		r.Count = i
	}
}
