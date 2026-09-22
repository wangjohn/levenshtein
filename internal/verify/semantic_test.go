package verify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const semanticConfig = `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"}},"checks":{"semantic":{"kind":"semantic-lint","target":"app","environment":"host"%s}},"runs":{"branch":{"checks":["semantic"]},"audit":{"checks":["semantic"],"rerun_checks":true}}}`

func TestSemanticLintConfiguration(t *testing.T) {
	for _, extra := range []string{``, `,"semantic":{"base":"develop","model":"jev-1.13.0","timeout":"2m"}`} {
		cfg, err := Parse([]byte(strings.Replace(semanticConfig, "%s", extra, 1)))
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range []string{"branch", "audit"} {
			plan, err := cfg.Plan(t.TempDir(), run)
			if err != nil || len(plan.Checks) != 1 || plan.Checks[0].Check.Kind != CheckSemanticLint {
				t.Fatalf("%s %s: %+v %v", extra, run, plan, err)
			}
		}
	}

	// Caching, artifacts, preparation and build now live inside the command
	// object, so a semantic-lint check cannot express them at all.
	for name, extra := range map[string]string{
		"cache":      `,"cache":true`,
		"artifacts":  `,"artifacts":["out"]`,
		"loose base": `,"base":"develop"`,
	} {
		if _, err := Parse([]byte(strings.Replace(semanticConfig, "%s", extra, 1))); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}

	for name, extra := range map[string]string{
		"command":     `,"command":{"args":["true"]}`,
		"alias model": `,"semantic":{"model":"jev-latest"}`,
		"flag base":   `,"semantic":{"base":"--output=x"}`,
	} {
		cfg, err := Parse([]byte(strings.Replace(semanticConfig, "%s", extra, 1)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}

	for name, data := range map[string]string{
		"dagger executor":           `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"semantic":{"kind":"semantic-lint","target":"app","environment":"go"}},"runs":{"branch":{"checks":["semantic"]}}}`,
		"semantic on command check": `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"}},"checks":{"test":{"kind":"command","target":"app","environment":"host","command":{"args":["true"]},"semantic":{"base":"main"}}},"runs":{"branch":{"checks":["test"]}}}`,
		"command options on dagger": `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"go":{"executor":"dagger"}},"checks":{"lint":{"kind":"go-lint","target":"app","environment":"go","command":{"args":["true"]}}},"runs":{"branch":{"checks":["lint"]}}}`,
	} {
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

// TestSemanticLintRejectsCommittedCredentials covers the redirect that
// configuration could otherwise perform: levenshtein.json travels with the pull
// request, so a declared origin would choose where the CI secret is sent.
func TestSemanticLintRejectsCommittedCredentials(t *testing.T) {
	environment := `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"%s}},"checks":{"semantic":{"kind":"semantic-lint","target":"app","environment":"host"}},"runs":{"branch":{"checks":["semantic"]}}}`

	for name, data := range map[string]string{
		"environment env base url": strings.Replace(environment, "%s", `,"env":{"TYPESAFE_BASE_URL":"https://attacker.example"}`, 1),
		"environment env api key":  strings.Replace(environment, "%s", `,"env":{"TYPESAFE_API_KEY":"leaked"}`, 1),
		"pass_env api key":         strings.Replace(environment, "%s", `,"pass_env":["TYPESAFE_API_KEY"]`, 1),
	} {
		cfg, err := Parse([]byte(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, err = cfg.Plan(t.TempDir(), "branch")
		if err == nil || !strings.Contains(err.Error(), "TYPESAFE_") {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}

	// An unrelated variable and a base-URL pass_env entry stay usable.
	cfg, err := Parse([]byte(strings.Replace(environment, "%s", `,"env":{"GOFLAGS":"-mod=mod"},"pass_env":["TYPESAFE_BASE_URL"]`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Plan(t.TempDir(), "branch"); err != nil {
		t.Fatalf("unrelated environment entries rejected: %v", err)
	}
}

func TestSemanticOriginRequiresHTTPSOffLoopback(t *testing.T) {
	for _, raw := range []string{"https://api.typesafe.ai/", "http://127.0.0.1:8080", "http://localhost:9/v1", "http://[::1]:9"} {
		if _, err := semanticOrigin(raw); err != nil {
			t.Fatalf("%s rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://api.typesafe.ai", "http://evil.example:443", "ftp://api.typesafe.ai", "api.typesafe.ai"} {
		if origin, err := semanticOrigin(raw); err == nil {
			t.Fatalf("%s accepted as %q", raw, origin)
		}
	}
}

func semanticRequest(t *testing.T, source string) Request {
	t.Helper()
	return Request{Source: source, Shared: t.TempDir(), PlannedCheck: PlannedCheck{ID: "semantic", Check: Check{Kind: CheckSemanticLint, Semantic: &SemanticCheck{}}, Target: Target{Dir: ".", Workspace: ".", Inputs: []string{"."}}, Environment: Environment{Executor: ExecutorNative}}}
}

func TestSemanticLintRequiresAPIKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	result := (&Native{}).Execute(context.Background(), semanticRequest(t, t.TempDir()))
	if result.Status != StatusError || !strings.Contains(result.Error, "TYPESAFE_API_KEY") {
		t.Fatalf("missing key: %+v", result)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet", "--initial-branch=main")
	write("app/main.go", "package main\n\nfunc main() {}\n")
	write("testdata/bad.go", "package bad\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "Initial")
	run("switch", "--quiet", "-c", "feature")
	write("app/main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\t// Keep the greeting short for terminals.\n\tfmt.Println(\"hi\")\n}\n\nfunc helper(verbose bool) error {\n\treturn fmt.Errorf(\"failed\")\n}\n")
	write("testdata/bad.go", "package bad\n\nfunc Ignored() {}\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "Add greeting")
	return dir
}

func TestSemanticLintSeparatesItsTimeoutFromOuterCancellation(t *testing.T) {
	source := gitRepo(t)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	t.Setenv("TYPESAFE_API_KEY", "test")
	t.Setenv("TYPESAFE_BASE_URL", server.URL)
	t.Setenv("GITHUB_BASE_REF", "")
	req := semanticRequest(t, source)
	req.Check.Semantic.Timeout = "300ms"

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || result.Error != "semantic-lint timed out" {
		t.Fatalf("the check's own timeout: %+v", result)
	}

	// An expired outer deadline belongs to the run, not to this check.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	req.Check.Semantic.Timeout = "5m"
	if result = (&Native{}).Execute(ctx, req); result.Status != StatusCancelled {
		t.Fatalf("an outer deadline must not be reported as the check's timeout: %+v", result)
	}
}

func TestSemanticLintExecutesAdvisoryCheck(t *testing.T) {
	source := gitRepo(t)
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     map[string]any            `json:"state"`
			Questions map[string]map[string]any `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if file, ok := req.State["file"].(map[string]any); ok {
			mu.Lock()
			paths = append(paths, file["path"].(string))
			mu.Unlock()
		}
		answers := map[string]any{}
		for id, q := range req.Questions {
			if q["type"] == "score" {
				answers[id] = map[string]any{"type": "score", "score": 1.0, "confidence": 0.9}
			} else {
				answers[id] = map[string]any{"type": "noul", "noul": 0.9}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-1.13.0", "answers": answers, "usage": map[string]any{"input_tokens": 10}})
	}))
	defer server.Close()

	// The kind reads its variables from the host without pass_env; consumers only add a secret.
	t.Setenv("TYPESAFE_API_KEY", "test")
	t.Setenv("TYPESAFE_BASE_URL", server.URL)
	t.Setenv("GITHUB_BASE_REF", "")
	req := semanticRequest(t, source)

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusPassed || !strings.HasPrefix(result.Stdout, "semantic-lint (advisory)") {
		t.Fatalf("advisory execution: %+v", result)
	}

	t.Setenv("GITHUB_BASE_REF", "release-9")
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "release-9") {
		t.Fatalf("CI base ref must be honored: %+v", result)
	}
	req.Check.Semantic.Base = "main"
	if result = (&Native{}).Execute(context.Background(), req); result.Status != StatusPassed {
		t.Fatalf("configured base must override the CI base ref: %+v", result)
	}
	req.Check.Semantic.Base = ""
	t.Setenv("GITHUB_BASE_REF", "")
	var details struct {
		Outcome  string           `json:"mode"`
		Findings []map[string]any `json:"findings"`
		Base     string           `json:"base"`
	}
	if err := json.Unmarshal(result.Details, &details); err != nil || details.Outcome != "advisory" || details.Base == "" || len(details.Findings) == 0 {
		t.Fatalf("details: %s %v", result.Details, err)
	}
	for _, path := range paths {
		if strings.HasPrefix(path, "testdata/") {
			t.Fatalf("fixture judged: %v", paths)
		}
	}

	req.Target = Target{Dir: "docs", Workspace: ".", Inputs: []string{"docs"}}
	if err := os.MkdirAll(filepath.Join(source, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusPassed || !strings.Contains(result.Stdout, "no reviewable") {
		t.Fatalf("out-of-scope change must produce no judgments: %+v", result)
	}

	req.Target = Target{Dir: ".", Workspace: ".", Inputs: []string{"."}}
	req.Environment.Env = map[string]string{"TYPESAFE_BASE_URL": server.URL} // Configuration cannot redirect the origin.
	t.Setenv("TYPESAFE_BASE_URL", "http://127.0.0.1:9")
	req.Check.Semantic.Timeout = "5s"
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError {
		t.Fatalf("only the host value selects the origin, so the unreachable one must error: %+v", result)
	}

	t.Setenv("TYPESAFE_BASE_URL", "http://api.typesafe.invalid")
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || !strings.Contains(result.Error, "https") {
		t.Fatalf("a plain-http origin off loopback must be refused: %+v", result)
	}
}

// Committed PATH or GIT_* values would pick which git produces the diff.
func TestSemanticLintRejectsCommittedGitEnvironment(t *testing.T) {
	environment := `{"version":1,"targets":{"app":{"dir":".","inputs":["."]}},"environments":{"host":{"executor":"native"%s}},"checks":{"semantic":{"kind":"semantic-lint","target":"app","environment":"host"}},"runs":{"branch":{"checks":["semantic"]}}}`
	for name, extra := range map[string]string{
		"PATH":    `,"env":{"PATH":"tools"}`,
		"GIT_DIR": `,"env":{"GIT_DIR":"elsewhere/.git"}`,
	} {
		cfg, err := Parse([]byte(strings.Replace(environment, "%s", extra, 1)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := cfg.Plan(t.TempDir(), "branch"); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
}

// A base ref from the environment gets the same shape check as a configured one.
func TestSemanticLintValidatesTheEnvironmentBaseRef(t *testing.T) {
	source := gitRepo(t)
	t.Setenv("TYPESAFE_API_KEY", "test")
	t.Setenv("TYPESAFE_BASE_URL", "")
	t.Setenv("GITHUB_BASE_REF", "--output=x")

	result := (&Native{}).Execute(context.Background(), semanticRequest(t, source))
	if result.Status != StatusError || !strings.Contains(result.Error, "invalid semantic-lint base") {
		t.Fatalf("flag-shaped base ref: %+v", result)
	}
}
