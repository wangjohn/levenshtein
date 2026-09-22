// Package slices is a local stand-in for golang.org/x/exp/slices, so the
// fixture can exercise exptostd without resolving x/exp over the network.
package slices

func Contains[S ~[]E, E comparable](s S, v E) bool {
	for _, value := range s {
		if value == v {
			return true
		}
	}
	return false
}
