package verify

import (
	"maps"
	"os/exec"
	"path/filepath"
	"testing"
)

// Settings a developer keeps in go env -w change what go vet and go test
// report, so they must change the toolchain identity a native key carries.
// Settings that only choose where modules come from must not.
func TestToolchainIdentityCoversResultChangingGoSettings(t *testing.T) {
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
	} {
		if identity(settings, nil) == base {
			t.Errorf("go env -w %s did not change the toolchain identity", name)
		}
	}
	if identity("", map[string]string{"CGO_ENABLED": "0"}) == identity("", map[string]string{"CGO_ENABLED": "1"}) {
		t.Error("CGO_ENABLED did not change the toolchain identity")
	}
}
