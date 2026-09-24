// Package build compiles levenshtein-community-lint for a set of pinned rule
// modules. It generates a module that requires the pinned Staticcheck, this
// community module, and each rule module at its pin, resolves it without
// letting anything move off a pin, and builds it with the local toolchain.
//
// Every failure is an error that names the module responsible and says what is
// known, never a guessed fix.
package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// Request is what one build needs. The runner writes it as JSON.
type Request struct {
	// Go is the pinned Go version, as in .go-version.
	Go string `json:"go"`
	// Staticcheck is the pinned honnef.co/go/tools version.
	Staticcheck string `json:"staticcheck"`
	// Community is the directory holding this module, runner/community.
	Community string `json:"community"`
	// Modules are the rule modules to compile in, in levenshtein.json order.
	Modules []Module `json:"modules"`
}

// Module is one rule module pin.
type Module struct {
	Path      string `json:"path"`
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
	// Dir replaces the module with a local directory. Only this repository's
	// own tests use it, for the example module; levenshtein.json cannot.
	Dir string `json:"dir,omitempty"`
}

func (m Module) source() string {
	return m.Path + "@" + m.Version
}

// Result is what a build reports beside the binary.
type Result struct {
	// Retracted lists pinned versions their authors have retracted, with the
	// rationale the author gave.
	Retracted []Retraction `json:"retracted,omitempty"`
}

// Retraction is one retracted pin.
type Retraction struct {
	Module    string   `json:"module"`
	Version   string   `json:"version"`
	Rationale []string `json:"rationale,omitempty"`
}

// generatedModule is the module path of the generated linter module.
const generatedModule = "levenshtein.local/communitylint"

// communityModule is this module's path.
const communityModule = "github.com/wangjohn/levenshtein/runner/community"

// staticcheckModule is the module that provides Staticcheck.
const staticcheckModule = "honnef.co/go/tools"

// Binary is the name of the built linter.
const Binary = "levenshtein-community-lint"

// Build compiles the linter into out/Binary, working in work, which must be
// an empty directory.
func Build(ctx context.Context, req Request, work, out string) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	g := goCommand{Dir: work}
	if err := writeManifest(req, work); err != nil {
		return Result{}, err
	}
	for _, m := range req.Modules {
		if err := checkGoVersion(ctx, g, req.Go, m); err != nil {
			return Result{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), stub(req.Modules), 0o600); err != nil {
		return Result{}, err
	}

	if err := tidy(ctx, g, req); err != nil {
		return Result{}, err
	}
	if err := checkGoLine(work, req.Go); err != nil {
		return Result{}, err
	}
	if err := checkPins(ctx, g, req); err != nil {
		return Result{}, err
	}
	retracted, err := retractions(ctx, g, req.Modules)
	if err != nil {
		return Result{}, err
	}

	var found []exports
	for _, m := range req.Modules {
		exported, err := inspect(ctx, g, m)
		if err != nil {
			return Result{}, err
		}
		found = append(found, exported)
	}
	generated, err := generate(req.Modules, found)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), generated, 0o600); err != nil {
		return Result{}, err
	}

	if output, err := g.run(ctx, "build", "-trimpath", "-o", filepath.Join(out, Binary), "."); err != nil {
		return Result{}, fmt.Errorf("the community linter does not compile against the pinned dependencies:\n%s", output.combined())
	}
	return Result{Retracted: retracted}, nil
}

func (req Request) validate() error {
	if !version.IsValid("go" + req.Go) {
		return fmt.Errorf("invalid Go version %q", req.Go)
	}
	if req.Staticcheck == "" || !filepath.IsAbs(req.Community) {
		return errors.New("a build needs the Staticcheck pin and the community module's absolute directory")
	}
	if len(req.Modules) == 0 {
		return errors.New("a build needs at least one rule module")
	}

	seen := map[string]bool{}
	for _, m := range req.Modules {
		if err := module.Check(m.Path, m.Version); err != nil {
			return err
		}
		if module.CanonicalVersion(m.Version) != m.Version {
			return fmt.Errorf("%s: %q is not an exact version", m.Path, m.Version)
		}
		if m.Dir != "" && !filepath.IsAbs(m.Dir) {
			return fmt.Errorf("%s: replacement directory %q must be absolute", m.Path, m.Dir)
		}
		if seen[m.Path] || m.Path == communityModule || m.Path == staticcheckModule {
			return fmt.Errorf("%s cannot be a rule module here", m.Path)
		}
		seen[m.Path] = true
	}
	return nil
}

// writeManifest writes the generated module's go.mod: the pinned Go, this
// module from its directory, the pinned Staticcheck, and every rule module.
func writeManifest(req Request, work string) error {
	file := &modfile.File{}
	if err := file.AddModuleStmt(generatedModule); err != nil {
		return err
	}
	if err := file.AddGoStmt(req.Go); err != nil {
		return err
	}
	if err := file.AddRequire(communityModule, "v0.0.0"); err != nil {
		return err
	}
	if err := file.AddReplace(communityModule, "", req.Community, ""); err != nil {
		return err
	}
	if err := file.AddRequire(staticcheckModule, req.Staticcheck); err != nil {
		return err
	}
	for _, m := range req.Modules {
		if err := file.AddRequire(m.Path, m.Version); err != nil {
			return err
		}
		if m.Dir != "" {
			if err := file.AddReplace(m.Path, m.Version, m.Dir, ""); err != nil {
				return err
			}
		}
	}

	data, err := file.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(work, "go.mod"), data, 0o600)
}

// checkGoVersion refuses a module whose go directive is newer than the pinned
// Go, which the local toolchain could not build.
func checkGoVersion(ctx context.Context, g goCommand, pinned string, m Module) error {
	path := filepath.Join(m.Dir, "go.mod")
	if m.Dir == "" {
		output, err := g.run(ctx, "mod", "download", "-json", m.source())
		var download struct {
			GoMod string `json:"GoMod"`
			Error string `json:"Error"`
		}
		if jsonErr := json.Unmarshal(output.Stdout, &download); jsonErr != nil || download.Error != "" || err != nil || download.GoMod == "" {
			return fmt.Errorf("%s: downloading the module failed: %s", m.source(), firstNonEmpty(download.Error, output.combined()))
		}
		path = download.GoMod
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("%s: %w", m.source(), err)
	}
	file, err := modfile.ParseLax(path, data, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", m.source(), err)
	}
	if file.Module == nil || file.Module.Mod.Path != m.Path {
		return fmt.Errorf("%s: its go.mod does not declare module %s", m.source(), m.Path)
	}
	if file.Go != nil && version.Compare("go"+file.Go.Version, "go"+pinned) > 0 {
		return fmt.Errorf("%s requires go %s, but this release pins go %s", m.source(), file.Go.Version, pinned)
	}
	return nil
}

// stub imports every package the linter will, so tidy can resolve the module
// before the generator reads the rule modules' exports.
func stub(modules []Module) []byte {
	var text bytes.Buffer
	text.WriteString("package main\n\nimport (\n")
	fmt.Fprintf(&text, "\t_ %q\n\t_ %q\n", communityModule, staticcheckModule+"/lintcmd")
	for _, m := range modules {
		fmt.Fprintf(&text, "\t_ %q\n", m.Path+"/lvrules")
	}
	text.WriteString(")\n\nfunc main() {}\n")
	return text.Bytes()
}

// tidy resolves the generated module. Tidy reports "finding module for
// package" when some module imports a package no go.mod in the graph
// requires; it then picks whatever is newest on the proxy, so the build would
// change from one day to the next. That is refused even though tidy succeeds.
func tidy(ctx context.Context, g goCommand, req Request) error {
	output, err := g.run(ctx, "mod", "tidy")
	text := output.combined()
	// The stub imports each module's lvrules package, so a module without one
	// also makes tidy look for another module to provide it; name the cause.
	for _, m := range req.Modules {
		if strings.Contains(text, "does not contain package "+m.Path+"/lvrules") {
			return fmt.Errorf("%s has no lvrules package; see docs/community-rules.md#the-contract", m.source())
		}
	}
	if missing := lines(text, "finding module for package"); len(missing) > 0 {
		return fmt.Errorf("a rule module or one of its dependencies imports a package that no go.mod requires, so the build would depend on the module proxy's current state:\n%s", strings.Join(missing, "\n"))
	}
	if err == nil {
		return nil
	}

	if newer := lines(text, "requires go >="); len(newer) > 0 {
		return fmt.Errorf("a rule module's dependency needs a newer Go than this release pins (go %s):\n%s", req.Go, strings.Join(newer, "\n"))
	}
	return fmt.Errorf("resolving the rule modules failed:\n%s", text)
}

// checkGoLine refuses a resolution that raised the generated module's go line.
// Tidy fails when a dependency needs a newer Go than the local toolchain, but
// on a toolchain newer than the pin it succeeds and raises the line instead,
// which would build locally what the pinned toolchain refuses.
func checkGoLine(work, pinned string) error {
	path := filepath.Join(work, "go.mod")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	file, err := modfile.ParseLax(path, data, nil)
	if err != nil {
		return err
	}
	if file.Go == nil || file.Go.Version != pinned {
		found := "no go line"
		if file.Go != nil {
			found = "go " + file.Go.Version
		}
		return fmt.Errorf("a rule module's dependency needs a newer Go than this release pins (go %s): resolving it raised the go line to %s", pinned, found)
	}
	return nil
}

// checkPins refuses a resolution that moved Staticcheck or any rule module
// off its pin, naming the chain of requirements that moved it.
func checkPins(ctx context.Context, g goCommand, req Request) error {
	output, err := g.run(ctx, "list", "-m", "-f", "{{.Path}} {{.Version}}", "all")
	if err != nil {
		return fmt.Errorf("listing the resolved modules: %s", output.combined())
	}
	resolved := map[string]string{}
	for line := range strings.Lines(string(output.Stdout)) {
		if path, v, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			resolved[path] = v
		}
	}

	pins := []Module{{Path: staticcheckModule, Version: req.Staticcheck}}
	pins = append(pins, req.Modules...)
	for _, pin := range pins {
		got := resolved[pin.Path]
		if got == pin.Version {
			continue
		}
		graph, err := g.run(ctx, "mod", "graph")
		if err != nil {
			return fmt.Errorf("reading the module graph: %s", graph.combined())
		}
		return moved(pin, got, string(graph.Stdout))
	}
	return nil
}

// moved explains why a pin resolved to another version.
func moved(pin Module, got, graph string) error {
	subject := "Staticcheck"
	pinned := "this release pins " + pin.Version
	if pin.Path != staticcheckModule {
		subject = pin.source()
		pinned = "levenshtein.json pins " + pin.Version
	}

	chain := Chain(graph, generatedModule, pin.Path+"@"+got)
	if len(chain) == 0 {
		return fmt.Errorf("%s resolved to %s, but %s", subject, got, pinned)
	}
	message := fmt.Sprintf("%s requires %s %s", chain[0], pin.Path, got)
	if len(chain) > 1 {
		message += fmt.Sprintf(" (through %s)", strings.Join(chain[1:], ", "))
	}
	return fmt.Errorf("%s, but %s", message, pinned)
}

// Chain finds the shortest path of requirements in go mod graph output from
// root to target (path@version) and returns the modules along it, without root
// or target. It returns nil when target is unreachable.
func Chain(graph, root, target string) []string {
	edges := map[string][]string{}
	for line := range strings.Lines(graph) {
		from, to, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			edges[from] = append(edges[from], to)
		}
	}

	previous := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node == target {
			var chain []string
			for step := previous[node]; step != root; step = previous[step] {
				chain = append(chain, step)
			}
			slices.Reverse(chain)
			return chain
		}
		for _, next := range edges[node] {
			if _, seen := previous[next]; !seen {
				previous[next] = node
				queue = append(queue, next)
			}
		}
	}
	return nil
}

// retractions lists the pins their authors have retracted. The module proxy
// is asked afresh on every build; a pin replaced by a local directory has no
// retractions.
func retractions(ctx context.Context, g goCommand, modules []Module) ([]Retraction, error) {
	var retracted []Retraction
	for _, m := range modules {
		if m.Dir != "" {
			continue
		}
		output, err := g.run(ctx, "list", "-m", "-retracted", "-json", m.source())
		if err != nil {
			return nil, fmt.Errorf("%s: checking for retractions failed: %s", m.source(), output.combined())
		}
		var listed struct {
			Retracted []string `json:"Retracted"`
		}
		if err := json.Unmarshal(output.Stdout, &listed); err != nil {
			return nil, fmt.Errorf("%s: reading go list output: %w", m.source(), err)
		}
		if listed.Retracted != nil {
			retracted = append(retracted, Retraction{Module: m.Path, Version: m.Version, Rationale: listed.Retracted})
		}
	}
	return retracted, nil
}

// goCommand runs the local go command in the generated module. GOTOOLCHAIN
// is local, so no other toolchain is ever fetched; GOFLAGS=-mod=mod lets tidy
// and build write go.sum; and GOWORK is off, so no workspace around the
// directory can change the resolution.
type goCommand struct {
	Dir string
}

func (g goCommand) run(ctx context.Context, args ...string) (goOutput, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = g.Dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return goOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

// goOutput keeps a go command's streams apart: stdout carries the JSON or
// listing a caller parses, and stderr the progress lines, such as
// "go: downloading", that must never be parsed with it.
type goOutput struct {
	Stdout []byte
	Stderr []byte
}

// combined is both streams, for an error message.
func (o goOutput) combined() string {
	return strings.TrimSpace(string(o.Stdout) + "\n" + string(o.Stderr))
}

func lines(text, substring string) []string {
	var matched []string
	for line := range strings.Lines(text) {
		if strings.Contains(line, substring) {
			matched = append(matched, strings.TrimSpace(line))
		}
	}
	return matched
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
