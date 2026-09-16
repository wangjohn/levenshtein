//go:build !darwin && !linux

package verify

import (
	"fmt"
	"os"
)

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	return fmt.Errorf("cache writes require macOS or Linux")
}

func atomicWriteRoot(root *os.Root, path string, data []byte, mode os.FileMode) error {
	return fmt.Errorf("cache writes require macOS or Linux")
}
