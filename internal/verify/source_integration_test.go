//go:build integration

package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dagger.io/dagger"
)

func TestDaggerSourceBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := dagger.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	source := t.TempDir()
	files := []string{
		"services/api/main.go", "services/api/.env", "services/api/.env.example",
		"contracts/schema.json", "go.work", "personal/sentinel.txt", "unselected.txt",
		"services/api/generated/client.go",
		".env.example", ".git/config", "services/api/.git/config",
	}
	for _, file := range files {
		path := filepath.Join(source, file)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("synthetic fixture only"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	directory, err := daggerSource(client, source, []string{"services/api", "contracts", "go.work", "optional.go"}, []string{"services/api/generated"})
	if err != nil {
		t.Fatal(err)
	}
	exported := t.TempDir()
	if _, err := directory.Export(ctx, exported); err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{"services/api/main.go", "services/api/.env.example", "contracts/schema.json", "go.work"} {
		if _, err := os.Stat(filepath.Join(exported, file)); err != nil {
			t.Errorf("declared input %s was not imported: %v", file, err)
		}
	}
	for _, file := range []string{"personal", "unselected.txt", "services/api/.env", ".env.example", ".git", "services/api/.git", "services/api/generated"} {
		if _, err := os.Lstat(filepath.Join(exported, file)); !os.IsNotExist(err) {
			t.Errorf("excluded path %s entered the Dagger source: %v", file, err)
		}
	}
}
