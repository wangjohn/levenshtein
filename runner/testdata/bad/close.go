package bad

import "os"

func CloseBeforeError(name string) error {
	file, err := os.Open(name)
	defer file.Close() // SA5001: error must be checked before scheduling Close.
	if err != nil {
		return err
	}
	return nil
}

func NeverReturns(file *os.File) {
	for {
		defer file.Close() // SA5003: this function never reaches deferred cleanup.
	}
}

func RangeChannel(files <-chan *os.File) {
	for file := range files {
		defer file.Close() // SA9001: cleanup waits until the channel loop finishes.
	}
}
