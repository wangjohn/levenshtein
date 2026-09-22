module example.com/levenshtein/bad

go 1.27.1

require (
	github.com/go-logr/logr v1.4.3
	github.com/rs/zerolog v1.34.0
	github.com/stretchr/testify v1.11.1
	go.uber.org/zap v1.27.0
	golang.org/x/exp v0.0.0-20260901000000-000000000000
)

// The fixture has to lint offline, so its third-party imports are local
// stand-ins.
replace (
	github.com/go-logr/logr => ./thirdparty/logr
	github.com/rs/zerolog => ./thirdparty/zerolog
	github.com/stretchr/testify => ./thirdparty/testify
	go.uber.org/zap => ./thirdparty/zap
	golang.org/x/exp => ./thirdparty/exp
)
