// Package counter has a data race its test exercises, so the race detector
// fails the test and go-test reports a finding even though no assertion fails.
package counter

// Count increments one variable from two goroutines without synchronization.
func Count() int {
	n := 0
	done := make(chan struct{})
	go func() {
		n++
		close(done)
	}()
	n++
	<-done
	return n
}
