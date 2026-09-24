package mixed

func Mixed() int {
	//lint:ignore SA4006,errs_nopanic both linters // want `this directive names core codes \(SA4006\) and community codes \(errs_nopanic\); write one directive for each on consecutive lines`
	return 1
}

func Split() int {
	//lint:ignore SA4006 core only
	//lint:ignore errs_nopanic community only
	return 2
}

//lint:file-ignore errs_*,U1000 whole file // want `this directive names core codes \(U1000\) and community codes \(errs_\*\)`

// Not a directive: //lint:ignore SA4006,errs_nopanic
func Prose() int { return 3 }
