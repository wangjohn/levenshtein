package store

import (
	"net/http/httptest" // The store rule excludes tests.
	"path/filepath"
	"testing"
)

func TestSave(t *testing.T) {
	_ = httptest.NewRecorder()
	if err := Save(filepath.Join(t.TempDir(), "name"), "ada"); err != nil {
		t.Fatal(err)
	}
}
