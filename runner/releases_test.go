package main

import (
	"encoding/json"
	"testing"

	"dagger/levenshtein/internal/checktool"
)

// Every platform either executor runs on has a reviewed asset for each pinned
// release, and the engine's CPU variant does not hide the one for its
// architecture.
func TestReleasePinsCoverEveryPlatform(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	for tool, pin := range map[string]checktool.ReleasePin{"shellcheck": tools.ShellCheck, "osv-scanner": tools.OSVScanner, "zizmor": tools.Zizmor} {
		for _, platform := range []string{"linux/amd64", "linux/arm64", "linux/arm64/v8", "darwin/amd64", "darwin/arm64"} {
			if _, err := pin.Asset(tool, platform); err != nil {
				t.Errorf("%s %s: %v", tool, platform, err)
			}
		}
		if _, err := pin.Asset(tool, "windows/amd64"); err == nil {
			t.Errorf("%s: an unpinned platform must be an error, not an unverified download", tool)
		}
	}
	if tools.ShellCheck.Binary == "" || tools.Zizmor.Binary != "zizmor" {
		t.Error("ShellCheck and zizmor ship in archives, zizmor at the archive's root")
	}
	if tools.OSVScanner.Binary != "" {
		t.Error("osv-scanner ships as a bare binary")
	}
}
