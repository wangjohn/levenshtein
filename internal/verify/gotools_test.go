package verify

import (
	"context"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Settings a developer keeps in go env -w change what go vet and go test
// report, so they must change the toolchain identity a native key carries.
// Settings that only choose where modules come from must not.
func TestToolchainIdentityCoversResultChangingGoSettings(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is unavailable")
	}
	req := nativeRequest(t)
	identity := func(settings string, env map[string]string) string {
		t.Helper()
		file := filepath.Join(t.TempDir(), "env")
		writeFile(t, file, settings)
		req.Environment.Env = map[string]string{"GOENV": file}
		maps.Copy(req.Environment.Env, env)
		found, err := sessionAt("").toolchainIdentity(t.Context(), req.Source, nativeEnv(req, nil))
		if err != nil {
			t.Fatal(err)
		}
		return digest(found)
	}

	base := identity("", nil)
	if got := identity("GOPRIVATE=example.com/private\nGOPROXY=https://proxy.example.com\n", nil); got != base {
		t.Error("module download settings changed the toolchain identity")
	}
	for name, settings := range map[string]string{
		"GOFLAGS":      "GOFLAGS=-tags=integration\n",
		"GOEXPERIMENT": "GOEXPERIMENT=jsonv2\n",
		"CC":           "CC=/nonexistent/cc\n",
		"GOFIPS140":    "GOFIPS140=latest\n",
	} {
		if identity(settings, nil) == base {
			t.Errorf("go env -w %s did not change the toolchain identity", name)
		}
	}
	if identity("", map[string]string{"CGO_ENABLED": "0"}) == identity("", map[string]string{"CGO_ENABLED": "1"}) {
		t.Error("CGO_ENABLED did not change the toolchain identity")
	}
	if identity("", map[string]string{"GODEBUG": "panicnil=1"}) == base {
		t.Error("GODEBUG did not change the toolchain identity")
	}
}

func helperFixture(t *testing.T, marker string) Request {
	t.Helper()
	req := nativeRequest(t)
	writeFile(t, filepath.Join(req.Shared, "go.mod"), "module example.com/helper\n\ngo 1.21\n")
	writeFile(t, filepath.Join(req.Shared, "main.go"), "package main\nimport \"fmt\"\nvar marker = \""+marker+"\"\nfunc main() { fmt.Print(marker) }\n")
	return req
}

func TestHelperPathsKeepConcurrentImplementationsAndBuildSettings(t *testing.T) {
	t.Parallel()
	tool := helper{Name: "marker", Module: ".", Pkg: "."}
	root := t.TempDir()
	revisions := []Request{helperFixture(t, "revision-a"), helperFixture(t, "revision-b")}
	paths := make([]string, 6)
	var builds sync.WaitGroup
	for i := range paths {
		builds.Go(func() {
			env := os.Environ()
			if i >= 2 && i < 4 {
				env = goEnv(env, []string{"GOFLAGS=-ldflags=-X=main.marker=settings-" + strconv.Itoa(i)})
			}
			path, err := build(t.Context(), revisions[i%2], goRun{Root: root, Env: env}, tool)
			if err != nil {
				t.Error(err)
				return
			}
			paths[i] = path
		})
	}
	builds.Wait()
	if t.Failed() {
		t.FailNow()
	}

	// Execution deliberately waits until all other revisions and settings
	// have built into the same cache. The former tools/marker path ran the
	// last writer's marker for every consumer here.
	for i, path := range paths {
		want := "revision-" + string(rune('a'+i%2))
		if i >= 2 && i < 4 {
			want = "settings-" + strconv.Itoa(i)
		}
		run, err := runTool(t.Context(), root, []string{path}, os.Environ(), time.Minute)
		if err != nil || run.ExitCode != 0 || run.Stdout != want {
			t.Errorf("consumer %d: run=%+v error=%v, want %q", i, run, err, want)
		}
	}
	reused, err := build(t.Context(), revisions[0], goRun{Root: root, Env: os.Environ()}, tool)
	if err != nil || reused != paths[0] {
		t.Errorf("equivalent build path=%q error=%v, want %q", reused, err, paths[0])
	}
	assertNoHelperStages(t, root)
}

func assertNoHelperStages(t *testing.T, root string) {
	t.Helper()
	stages, err := filepath.Glob(filepath.Join(root, "tools", ".build-*"))
	if err != nil || len(stages) != 0 {
		t.Errorf("private build artifacts remain: %v, error=%v", stages, err)
	}
}

func TestHelperFailedAndCancelledBuildsLeaveNoExecutables(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			t.Parallel()
			req := helperFixture(t, "unused")
			root := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			} else {
				writeFile(t, filepath.Join(req.Shared, "main.go"), "invalid Go source")
			}

			path, err := build(ctx, req, goRun{Root: root, Env: os.Environ()}, helper{Name: "marker", Module: ".", Pkg: "."})
			if err == nil || path != "" {
				t.Fatalf("path=%q error=%v, want a failed build", path, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, "tools"))
			if err != nil || len(entries) != 0 {
				t.Errorf("failed build left artifacts: %v, error=%v", entries, err)
			}
		})
	}
}

func TestHelperReuseRejectsModifiedOrSymlinkExecutables(t *testing.T) {
	t.Parallel()
	for _, symlink := range []bool{false, true} {
		t.Run(strconv.FormatBool(symlink), func(t *testing.T) {
			t.Parallel()
			req := helperFixture(t, "original")
			work := goRun{Root: t.TempDir(), Env: os.Environ()}
			tool := helper{Name: "marker", Module: ".", Pkg: "."}
			path, err := build(t.Context(), req, work, tool)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink("/bin/sh", path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("tampered"), 0500); err != nil {
				t.Fatal(err)
			}

			if returned, err := build(t.Context(), req, work, tool); err == nil || returned != "" {
				t.Errorf("reused unsafe executable %q: %v", returned, err)
			}
			assertNoHelperStages(t, work.Root)
		})
	}
}

func TestHelperCancellationWhileWaitingToPublish(t *testing.T) {
	t.Parallel()
	req := helperFixture(t, "cancelled-publication")
	work := goRun{Root: t.TempDir(), Env: os.Environ()}
	tool := helper{Name: "marker", Module: ".", Pkg: "."}
	path, err := build(t.Context(), req, work, tool)
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Base(filepath.Dir(path))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockFile(t.Context(), filepath.Join(work.Root, "locks", "tool-"+identity), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req.session = sessionAt("")
	req.session.waiting = func(string) { cancel() }

	returned, err := build(ctx, req, work, tool)
	if !errors.Is(err, context.Canceled) || returned != "" {
		t.Errorf("publication path=%q error=%v, want cancellation", returned, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cancelled publication left executable: %v", err)
	}
	assertNoHelperStages(t, work.Root)
}
