package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validateFiles validates the Dagger import of inputs less excludes, enumerated
// from the filesystem.
func validateFiles(t *testing.T, source string, inputs, excludes []string) error {
	t.Helper()
	_, err := importExcludes(t.Context(), sessionAt(""), fileSet{Root: source, Inputs: inputs, Excludes: excludes, Discovery: DiscoveryFilesystem})
	return err
}

func TestDaggerInputsAreLiteralPaths(t *testing.T) {
	for _, input := range []string{"../personal", "/private", "services/*", "!personal", "[ab]", "{a,b}", "a?", "a\nb"} {
		if _, err := daggerIncludes([]string{input}); err == nil {
			t.Errorf("accepted pattern or escaping input %q", input)
		}
	}
	if _, err := daggerIncludes(nil); err == nil {
		t.Fatal("empty inputs would import the whole source")
	}
}

func TestDaggerSourceRejectsAliasesButDoesNotInspectExcludedTrees(t *testing.T) {
	source := t.TempDir()
	for _, dir := range []string{"product", "personal"} {
		if err := os.Mkdir(filepath.Join(source, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(source, "personal", "external")); err != nil {
		t.Fatal(err)
	}
	if err := validateFiles(t, source, []string{"product", "missing.go"}, nil); err != nil {
		t.Fatalf("inspected excluded content or rejected optional missing input: %v", err)
	}

	for _, alias := range []string{"product/alias", "alias"} {
		t.Run(alias, func(t *testing.T) {
			path := filepath.Join(source, alias)
			destination, err := filepath.Rel(filepath.Dir(path), filepath.Join(source, "personal"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(destination, path); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Remove(path) }()

			for _, input := range []string{alias, filepath.Join(alias, "missing.go")} {
				if err := validateFiles(t, source, []string{input}, nil); err == nil || !strings.Contains(err.Error(), "symlink") {
					t.Fatalf("accepted symlink input %q: %v", input, err)
				}
			}
			if alias == "product/alias" {
				if err := validateFiles(t, source, []string{"product"}, nil); err == nil {
					t.Fatal("accepted a symlink nested inside the allowed directory")
				}
				if err := validateFiles(t, source, []string{"product"}, []string{"product/alias"}); err != nil {
					t.Fatalf("excluded subtree still inspected: %v", err)
				}
			}
		})
	}
}
