package good

import "os"

func Read(name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return nil
}

// A helper returns once per iteration, so its deferred cleanup runs promptly.
func ReadAll(names []string) error {
	for _, name := range names {
		if err := Read(name); err != nil {
			return err
		}
	}
	return nil
}

func CloseFiles(files <-chan *os.File) {
	for file := range files {
		func() {
			defer file.Close()
		}()
	}
}
