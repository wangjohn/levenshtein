package main

import (
	"encoding/json"
	"testing"
)

// Every platform either executor runs on has a reviewed archive, and the
// engine's CPU variant does not hide the one for its architecture.
func TestZizmorPinCoversEveryPlatform(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	if tools.Zizmor.Version == "" {
		t.Fatal("toolchain.json must pin a zizmor version")
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64", "linux/arm64/v8", "darwin/amd64", "darwin/arm64"} {
		if _, err := tools.Zizmor.archive(platform); err != nil {
			t.Errorf("%s: %v", platform, err)
		}
	}
	if _, err := tools.Zizmor.archive("windows/amd64"); err == nil {
		t.Error("an unpinned platform must be an error, not an unverified download")
	}
}
