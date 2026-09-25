package doccheck

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// indexed returns the repository paths the Markdown in text links to,
// resolved from file and without fragments.
func indexed(file, text string) map[string]bool {
	pages := map[string]bool{}
	for _, l := range parse(text).links {
		target, _, _ := strings.Cut(l.target, "#")
		if target == "" || scheme.MatchString(target) {
			continue
		}
		pages[path.Join(path.Dir(file), target)] = true
	}
	return pages
}

// docs/README.md is the index of the documentation, so every page under
// docs/ must appear in it.
func TestDocsIndexListsEveryPage(t *testing.T) {
	docs := loadDocs(t)
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := indexed("docs/README.md", string(data))

	for file := range docs {
		if strings.HasPrefix(file, "docs/") && file != "docs/README.md" && !listed[file] {
			t.Errorf("docs/README.md does not list %s", file)
		}
	}
}

// The project README's Documentation section picks pages from the index, so
// each of its pages must also be in docs/README.md.
func TestReadmeDocumentationIsInTheIndex(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(readme), "\n## Documentation\n")
	if !found {
		t.Fatal("README.md has no Documentation section")
	}
	section, _, _ = strings.Cut(section, "\n## ")

	index, err := os.ReadFile(filepath.Join(repoRoot, "docs", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := indexed("docs/README.md", string(index))
	for page := range indexed("README.md", section) {
		if strings.HasPrefix(page, "docs/") && page != "docs/README.md" && !listed[page] {
			t.Errorf("README.md's Documentation section links %s, which docs/README.md does not list", page)
		}
	}
}
