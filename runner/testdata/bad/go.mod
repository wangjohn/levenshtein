module example.com/levenshtein/bad

go 1.27.1

require github.com/stretchr/testify v1.11.1

// The fixture has to lint offline, so testify is a local stand-in.
replace github.com/stretchr/testify => ./thirdparty/testify
