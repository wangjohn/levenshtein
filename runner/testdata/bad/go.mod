module example.com/levenshtein/bad

go 1.27.1

require (
	github.com/stretchr/testify v1.11.1
	golang.org/x/exp v0.0.0-20260901000000-000000000000
)

// The fixture has to lint offline, so testify and x/exp are local stand-ins.
replace (
	github.com/stretchr/testify => ./thirdparty/testify
	golang.org/x/exp => ./thirdparty/exp
)
