package verify

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

const templatesDir = "../../templates"

// The starter configuration must be one a consumer can use as it is: every
// run plans against a repository with a Go module at its root.
func TestStarterConfigurationPlans(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(templatesDir, "levenshtein.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Baseline == "" {
		t.Fatal("the starter configuration should name a baseline file")
	}

	source := t.TempDir()
	for _, run := range []string{"branch", "pre-merge", "main"} {
		plan, err := cfg.Plan(source, run)
		if err != nil {
			t.Fatalf("run %s: %v", run, err)
		}
		for _, check := range plan.Checks {
			if check.Environment.Executor != ExecutorNative {
				t.Errorf("run %s: %s needs a container runtime; the starter runs natively", run, check.ID)
			}
		}
	}
}

func TestClaudeSettingsTemplate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(templatesDir, "claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := decode(data, &settings); err != nil {
		t.Fatal(err)
	}

	for event, script := range map[string]string{"Stop": "levenshtein-stop.sh", "PostToolUse": "levenshtein-gofmt.sh"} {
		groups := settings.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: want one hook, got %+v", event, groups)
		}
		hook := groups[0].Hooks[0]
		if hook.Type != "command" || hook.Command != `"$CLAUDE_PROJECT_DIR"/.claude/hooks/`+script || hook.Timeout <= 0 {
			t.Errorf("%s hook: %+v", event, hook)
		}
		info, err := os.Stat(filepath.Join(templatesDir, "claude", "hooks", script))
		if err != nil || info.Mode().Perm()&0111 == 0 {
			t.Errorf("%s must be an executable file: %v", script, err)
		}
	}
	if matcher := settings.Hooks["PostToolUse"][0].Matcher; matcher != "Edit|MultiEdit|Write" {
		t.Errorf("PostToolUse matcher = %q", matcher)
	}
}

// hookTools skips a hook test on a host without the tools the hooks use.
func hookTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed: %v", tool, err)
		}
	}
}

// runHook runs a hook template with the given JSON on stdin and environment,
// and returns its exit code and stderr.
func runHook(t *testing.T, script, input string, env ...string) (int, string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(templatesDir, "claude", "hooks", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), path)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(testgit.Env(), env...) // The hooks run git.
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stderr.String()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, stderr.String()
}

func TestStopHookTemplate(t *testing.T) {
	hookTools(t, "bash", "git", "jq")
	project := t.TempDir()
	if out, err := testgit.Command(t.Context(), "git", "", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}

	// A stand-in launcher records that it ran and exits as told.
	levenshtein := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	launcher := "#!/usr/bin/env bash\necho \"$@\" > " + marker + "\necho 'a.go:1:1: LV1005 not formatted'\nexit ${FAKE_STATUS:-0}\n"
	if err := os.WriteFile(filepath.Join(levenshtein, "verify"), []byte(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	input := `{"hook_event_name": "Stop", "cwd": "` + project + `", "stop_hook_active": false}`
	env := []string{"LEVENSHTEIN=" + levenshtein, "CLAUDE_PROJECT_DIR=" + project}

	if code, stderr := runHook(t, "levenshtein-stop.sh", input, env...); code != 0 {
		t.Fatalf("no Go change must let the agent stop: %d %s", code, stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the run must be skipped when no Go file changed")
	}

	if err := os.WriteFile(filepath.Join(project, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runHook(t, "levenshtein-stop.sh", input, append(env, "FAKE_STATUS=0")...); code != 0 {
		t.Fatalf("a passing run must let the agent stop: %d %s", code, stderr)
	}
	args, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(args), "branch --source "+project+" --format text") {
		t.Fatalf("launcher arguments: %q %v", args, err)
	}

	code, stderr := runHook(t, "levenshtein-stop.sh", input, append(env, "FAKE_STATUS=1", "LEVENSHTEIN_RUN=quick")...)
	if code != 2 || !strings.Contains(stderr, "a.go:1:1: LV1005 not formatted") || !strings.Contains(stderr, "quick run fails") {
		t.Fatalf("a failing run must block with the findings: %d %s", code, stderr)
	}

	active := strings.Replace(input, `"stop_hook_active": false`, `"stop_hook_active": true`, 1)
	if code, _ := runHook(t, "levenshtein-stop.sh", active, append(env, "FAKE_STATUS=1", "LEVENSHTEIN_STOP_ONCE=1")...); code != 0 {
		t.Fatalf("LEVENSHTEIN_STOP_ONCE must let a continued turn stop: %d", code)
	}

	if code, stderr := runHook(t, "levenshtein-stop.sh", input, append(env, "FAKE_STATUS=2")...); code != 1 || !strings.Contains(stderr, "could not run") {
		t.Fatalf("a broken setup must not block the agent: %d %s", code, stderr)
	}
	if code, _ := runHook(t, "levenshtein-stop.sh", input, "LEVENSHTEIN="+filepath.Join(levenshtein, "absent"), "CLAUDE_PROJECT_DIR="+project); code != 1 {
		t.Fatalf("a missing launcher must not block the agent: %d", code)
	}
}

func TestStopHookChecksUnpushedCommits(t *testing.T) {
	hookTools(t, "bash", "git", "jq")
	remote := t.TempDir()
	project := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := testgit.Command(t.Context(), "git", "", append([]string{"-C", project, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if out, err := testgit.Command(t.Context(), "git", "", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "start")
	git("remote", "add", "origin", remote)
	git("push", "-q", "-u", "origin", "main")

	levenshtein := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	launcher := "#!/usr/bin/env bash\ntouch " + marker + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(levenshtein, "verify"), []byte(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	input := `{"hook_event_name": "Stop", "cwd": "` + project + `", "stop_hook_active": false}`
	env := []string{"LEVENSHTEIN=" + levenshtein, "CLAUDE_PROJECT_DIR=" + project}

	if code, stderr := runHook(t, "levenshtein-stop.sh", input, env...); code != 0 {
		t.Fatalf("a pushed branch must let the agent stop: %d %s", code, stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the run must be skipped when everything is pushed")
	}

	if err := os.WriteFile(filepath.Join(project, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.go")
	git("commit", "-q", "-m", "add a")

	if code, stderr := runHook(t, "levenshtein-stop.sh", input, env...); code != 0 {
		t.Fatalf("a passing run must let the agent stop: %d %s", code, stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("a committed Go change the branch has not pushed must run the checks")
	}

	// A new branch has no upstream; its commits since the remote's default
	// branch are unpushed.
	git("push", "-q", "origin", "main")
	git("remote", "set-head", "origin", "main")
	git("switch", "-q", "-c", "agent")
	if err := os.WriteFile(filepath.Join(project, "b.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "b.go")
	git("commit", "-q", "-m", "add b")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	if code, stderr := runHook(t, "levenshtein-stop.sh", input, env...); code != 0 {
		t.Fatalf("a passing run must let the agent stop: %d %s", code, stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("a committed Go change on a branch without an upstream must run the checks")
	}
}

func TestGofmtHookTemplate(t *testing.T) {
	hookTools(t, "bash", "jq", "gofmt")
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	input := func(path string) string {
		encoded, err := json.Marshal(map[string]any{"tool_name": "Edit", "tool_input": map[string]string{"file_path": path}})
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}

	for name, tc := range map[string]struct {
		path string
		code int
		want string
	}{
		"formatted":   {write("good.go", "package a\n"), 0, ""},
		"unformatted": {write("bad.go", "package a\nfunc  f() {}\n"), 2, "is not gofmt-formatted (LV1005). Run: gofmt -w "},
		"broken":      {write("broken.go", "package a\nfunc {\n"), 2, "does not parse"},
		"not Go":      {write("notes.txt", "func  f() {}\n"), 0, ""},
		"missing":     {filepath.Join(dir, "absent.go"), 0, ""},
	} {
		code, stderr := runHook(t, "levenshtein-gofmt.sh", input(tc.path))
		if code != tc.code || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: exit %d, stderr %q", name, code, stderr)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "bad.go")); err != nil || string(data) != "package a\nfunc  f() {}\n" {
		t.Fatalf("the hook must never rewrite a file: %q %v", data, err)
	}
}
