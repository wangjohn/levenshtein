//go:build integration

package verify

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"dagger.io/dagger"
)

// The Dagger half of TestFileSetConformance: what the container receives is
// exactly the key's file set less the private files, for a git-discovery kind
// (no ignored file, including one whose name holds pattern characters) and for
// a Go kind (the ignored files the Go toolchain can load, links included).
func TestDaggerImportMatchesTheKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := dagger.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	root := fileSetRepository(t)
	stats.configure(t.TempDir())

	for _, tt := range []struct {
		kind CheckKind
		want []string
	}{{kind: CheckSecrets, want: listedFiles}, {kind: CheckGoVet, want: goFiles}} {
		t.Run(string(tt.kind), func(t *testing.T) {
			req := Request{Source: root, PlannedCheck: planFor(t, root, tt.kind, ExecutorDagger)}
			keyed := keyedFiles(t, req)
			if !slices.Equal(keyed, tt.want) {
				t.Fatalf("key hashes %v, want %v", keyed, tt.want)
			}

			directory, err := daggerSource(ctx, client, req)
			if err != nil {
				t.Fatal(err)
			}
			exported := t.TempDir()
			if _, err := directory.Export(ctx, exported); err != nil {
				t.Fatal(err)
			}

			var imported []string
			err = filepath.WalkDir(exported, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				rel, err := filepath.Rel(exported, path)
				imported = append(imported, filepath.ToSlash(rel))
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(imported)
			if want := withoutPrivate(keyed); !slices.Equal(imported, want) {
				t.Fatalf("Dagger imported %v, want the key's files less private ones, %v", imported, want)
			}
		})
	}
}
