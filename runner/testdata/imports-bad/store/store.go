// Package store is supposed to use the standard library and core alone.
package store

import (
	"net/http"
	"os"
)

// Save writes the status of a request to url.
func Save(path, url string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return os.WriteFile(path, []byte(response.Status), 0o600)
}
