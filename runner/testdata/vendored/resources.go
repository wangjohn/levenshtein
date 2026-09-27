package vendored

import "example.invalid/offline"

// GPU spells a value of another module's open-ended string type. LV1001 only
// asks for typed constants of this module's own types.
func GPU() offline.Resource {
	return offline.Resource("nvidia.com/gpu")
}
