// Package sum has tests that pass under the race detector, so go-test passes
// it.
package sum

import "sync"

// Total adds the values concurrently, guarding the running sum with a mutex.
func Total(values []int) int {
	var (
		mu    sync.Mutex
		total int
		wg    sync.WaitGroup
	)
	for _, value := range values {
		wg.Go(func() {
			mu.Lock()
			defer mu.Unlock()
			total += value
		})
	}
	wg.Wait()
	return total
}
