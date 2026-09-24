package main

import (
	"encoding/json"
	"testing"
)

// Every platform either executor runs on has a reviewed asset for each pinned
// release, and the engine's CPU variant does not hide the one for its
// architecture.
func TestReleasePinsCoverEveryPlatform(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	for tool, pin := range map[string]releasePin{"shellcheck": tools.ShellCheck} {
		for _, platform := range []string{"linux/amd64", "linux/arm64", "linux/arm64/v8", "darwin/amd64", "darwin/arm64"} {
			if _, err := pin.asset(tool, platform); err != nil {
				t.Errorf("%s %s: %v", tool, platform, err)
			}
		}
		if _, err := pin.asset(tool, "windows/amd64"); err == nil {
			t.Errorf("%s: an unpinned platform must be an error, not an unverified download", tool)
		}
	}
	if tools.ShellCheck.Binary == "" {
		t.Error("ShellCheck ships in an archive")
	}
}
