package consumer

import "records"

func change(r records.Result) {
	r.Count = 1 // want "construct Result with a struct literal"
}
