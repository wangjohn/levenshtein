package consumer

import "records"

func change(r records.Result) { r.Count = 1 }
func create() {
	r := records.Result{} // want "construct r with a struct literal"
	r.Count = 1
}
