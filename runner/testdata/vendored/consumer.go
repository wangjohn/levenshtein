package vendored

import "example.invalid/offline"

// The dependency exists only in vendor; resolving it over the network must fail.
func Value() string { return offline.Value }
