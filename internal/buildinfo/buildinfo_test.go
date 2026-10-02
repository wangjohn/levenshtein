package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		version     string
		commit      string
		info        *debug.BuildInfo
		wantVersion string
		wantCommit  string
		wantState   State
	}{
		{name: "unavailable", wantVersion: "development", wantCommit: "unavailable", wantState: StateUnavailable},
		{name: "partial release", version: "1.2.3", wantVersion: "development", wantCommit: "unavailable", wantState: StateUnavailable},
		{name: "release", version: "1.2.3", commit: "abc", wantVersion: "1.2.3", wantCommit: "abc", wantState: StateClean},
		{name: "dirty", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "true"}, {Key: "-ldflags", Value: "secret"}}}, wantVersion: "development", wantCommit: "abc", wantState: StateDirty},
		{name: "clean", info: &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "false"}}}, wantVersion: "development", wantCommit: "abc", wantState: StateClean},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := identify(test.version, test.commit, test.info)
			if got.Version != test.wantVersion || got.Commit != test.wantCommit || got.State != test.wantState || got.GoVersion == "" || got.OS == "" || got.Arch == "" {
				t.Fatalf("identity: %+v", got)
			}
		})
	}
}
