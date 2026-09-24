package renamed

func Old() int {
	//lint:ignore errs_panics renamed since // want `errs_panics is now errs_nopanic; update this directive`
	return 1
}

func Current() int {
	//lint:ignore errs_nopanic current name
	return 2
}

//lint:file-ignore ERRS_Panics,errs_sentinel whole file // want `ERRS_Panics is now errs_nopanic; update this directive`
