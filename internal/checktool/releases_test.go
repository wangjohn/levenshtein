package checktool

import (
	"strings"
	"testing"
)

func testPin(binary, name string) ReleasePin {
	return ReleasePin{
		Releases: "https://example.invalid/releases/",
		Version:  "1.2.3",
		Binary:   binary,
		Assets:   map[string]ReleaseAsset{"linux/arm64": {Name: name, SHA256: strings.Repeat("a", 64)}},
	}
}

// An engine's CPU variant does not hide the asset for its architecture, and a
// platform without a reviewed asset is an error rather than an unverified
// download.
func TestReleaseAssetIsOnlyTheReviewedOne(t *testing.T) {
	pin := testPin("tool-v1.2.3/tool", "tool.tar.gz")
	for _, platform := range []string{"linux/arm64", "linux/arm64/v8"} {
		if asset, err := pin.Asset("tool", platform); err != nil || asset.Name != "tool.tar.gz" {
			t.Errorf("%s: %+v %v", platform, asset, err)
		}
	}
	if _, err := pin.Asset("tool", "linux/amd64"); err == nil || !strings.Contains(err.Error(), "no reviewed tool 1.2.3 release asset for linux/amd64") {
		t.Errorf("an unpinned platform must be an error: %v", err)
	}
}

// A pin that could make either executor fetch or run something other than the
// reviewed file is refused.
func TestReleasePinRefusesWhatItCannotVerify(t *testing.T) {
	for name, pin := range map[string]ReleasePin{
		"no releases":           {Version: "1.2.3", Assets: testPin("", "tool").Assets},
		"no version":            {Releases: "https://example.invalid", Assets: testPin("", "tool").Assets},
		"no assets":             {Releases: "https://example.invalid", Version: "1.2.3"},
		"binary above archive":  testPin("../tool", "tool.tar.gz"),
		"absolute binary":       testPin("/usr/bin/tool", "tool.tar.gz"),
		"unclean binary":        testPin("dir/./tool", "tool.tar.gz"),
		"binary in a bare file": testPin("tool", "tool"),
		"asset in a directory":  testPin("", "dir/tool"),
		"asset above releases":  testPin("", ".."),
		"short checksum":        {Releases: "https://example.invalid", Version: "1.2.3", Assets: map[string]ReleaseAsset{"linux/arm64": {Name: "tool", SHA256: "abc"}}},
	} {
		if _, err := pin.Asset("tool", "linux/arm64"); err == nil {
			t.Errorf("%s: accepted %+v", name, pin)
		}
	}
	if err := testPin("../tool", "tool.tar.gz").Check("tool"); err == nil {
		t.Error("Check must refuse a binary outside its archive before any platform is chosen")
	}
}

func TestAssetURLNamesTheVersionedAsset(t *testing.T) {
	pin := testPin("", "tool_linux_arm64")
	asset := pin.Assets["linux/arm64"]

	if got, want := pin.AssetURL(asset, ""), "https://example.invalid/releases/v1.2.3/tool_linux_arm64"; got != want {
		t.Errorf("AssetURL = %q, want %q", got, want)
	}
	if got, want := pin.AssetURL(asset, "http://127.0.0.1:1"), "http://127.0.0.1:1/v1.2.3/tool_linux_arm64"; got != want {
		t.Errorf("AssetURL with an override = %q, want %q", got, want)
	}
}
