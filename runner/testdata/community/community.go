// Package community is the self-test's consumer of the fixture rule module.
package community

func forbidden() int { return 1 }

// Reported calls forbidden, which fixture_forbidden reports.
func Reported() int {
	return forbidden()
}

// Ignored calls forbidden under a directive.
func Ignored() int {
	//lint:ignore fixture_forbidden the self-test ignores this call
	return forbidden()
}

// Mixed names a core code and a community code in one directive.
func Mixed() int {
	//lint:ignore SA4006,fixture_forbidden one directive for two linters
	return 2
}
