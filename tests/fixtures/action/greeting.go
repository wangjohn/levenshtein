// Package greeting is the consumer fixture for the GitHub Action smoke test.
package greeting

// Hello returns a greeting for name.
func Hello(name string) string {
	return "hello, " + name
}
