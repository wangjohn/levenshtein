package bad

import (
	"context"
	"strconv"
)

// nilnesserr: err is already known to be nil, so the second failure returns
// no error at all.
func Nilnesserr(first, second string) (int, error) {
	left, err := strconv.Atoi(first)
	if err != nil {
		return 0, err
	}
	right, secondErr := strconv.Atoi(second)
	if secondErr != nil {
		return 0, err
	}
	return left + right, nil
}

// fatcontext: each iteration wraps the previous context, so the chain grows
// with every loop.
func Fatcontext(ctx context.Context, keys []string) context.Context {
	for _, key := range keys {
		ctx = context.WithValue(ctx, contextKey(key), key)
	}
	return ctx
}

type contextKey string
