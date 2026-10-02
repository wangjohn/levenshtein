// Package buildinfo identifies the CLI build without exposing build flags.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Version and Commit are set together by release linker flags.
var Version string

var Commit string

type State string

const (
	StateClean       State = "clean"
	StateDirty       State = "dirty"
	StateUnavailable State = "unavailable"
)

type Identity struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	State     State  `json:"state"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func Current() Identity {
	info, _ := debug.ReadBuildInfo()
	return identify(Version, Commit, info)
}

func identify(version, commit string, info *debug.BuildInfo) Identity {
	state := StateUnavailable
	goVersion := runtime.Version()
	if version == "" || commit == "" {
		version, commit = "development", "unavailable"
		if info != nil {
			for _, setting := range info.Settings {
				//lint:ignore LV1001 build setting keys are an open Go schema
				switch setting.Key {
				case "vcs.revision":
					commit = setting.Value
				case "vcs.modified":
					//lint:ignore LV1001 Go build setting values are an open schema
					switch setting.Value {
					case "true":
						state = StateDirty
					case "false":
						state = StateClean
					}
				}
			}
		}
	} else {
		state = StateClean
	}
	if info != nil && info.GoVersion != "" {
		goVersion = info.GoVersion
	}
	return Identity{Version: version, Commit: commit, State: state, GoVersion: goVersion, OS: runtime.GOOS, Arch: runtime.GOARCH}
}
