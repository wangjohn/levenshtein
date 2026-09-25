package verify

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite docs/check-kinds.md from the kind descriptors")

const (
	kindsDocStart = "<!-- check-kinds:start -->\n"
	kindsDocEnd   = "<!-- check-kinds:end -->\n"
)

// The table in docs/check-kinds.md is generated from kindSpecs, so the
// documented kinds cannot drift from what the verifier does. Run with -update
// to rewrite it.
func TestCheckKindsDoc(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "check-kinds.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	start, end := strings.Index(doc, kindsDocStart), strings.Index(doc, kindsDocEnd)
	if start < 0 || end < start {
		t.Fatalf("%s needs the %q and %q markers, in that order", path, strings.TrimSpace(kindsDocStart), strings.TrimSpace(kindsDocEnd))
	}

	want := doc[:start+len(kindsDocStart)] + kindsTable() + doc[end:]
	if *update {
		if err := os.WriteFile(path, []byte(want), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if doc != want {
		t.Fatalf("%s is out of date with kindSpecs; run go test -run TestCheckKindsDoc -update in internal/verify", path)
	}
}

// kindsTable renders one row per kind, in kindSpecs order.
func kindsTable() string {
	var out strings.Builder
	out.WriteString("| Kind | What it checks | Executors | Results cached | Baseline | Target | Default runs |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, spec := range kindSpecs {
		cells := []string{"`" + string(spec.kind) + "`", spec.summary, executorsCell(spec), cacheCell(spec), yesNo(spec.baseline), targetCell(spec), defaultRunsCell(spec)}
		out.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return out.String()
}

func executorsCell(spec kindSpec) string {
	var executors []string
	if spec.dagger != "" {
		executors = append(executors, string(ExecutorDagger))
	}
	if spec.native != nil {
		executors = append(executors, string(ExecutorNative))
	}
	return strings.Join(executors, ", ")
}

// cacheCell follows CachedExecutor.Execute: the always-fresh and
// base-dependent kinds are never cached, and otherwise a Dagger or shared Go
// kind is, while a native-only kind is cached only when a command opts in.
func cacheCell(spec kindSpec) string {
	switch {
	case spec.alwaysFresh != "":
		return "Never: " + spec.alwaysFresh
	case spec.baseDependent != "":
		return "Never by the CLI: " + spec.baseDependent
	case spec.dagger != "":
		return "Yes"
	case spec.kind == CheckCommand:
		return "Only with `command.cache`"
	default:
		return "Never"
	}
}

func targetCell(spec kindSpec) string {
	if spec.rootOnly {
		return "Repository root"
	}
	return "Any"
}

func defaultRunsCell(spec kindSpec) string {
	switch {
	case !spec.defaultCheck:
		return "Not in the defaults"
	case len(spec.defaultRuns) == 0:
		return "Its own run only"
	default:
		return "`" + strings.Join(spec.defaultRuns, "`, `") + "`"
	}
}

func yesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}
