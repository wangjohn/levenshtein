package app

import (
	"example.com/app/kinds"
	"example.com/external/resource"
)

// External types are open-ended: the module being linted cannot own their
// spellings, so a literal of one is a value, not an enum alternative.
func request(name resource.Name) map[resource.Name]int {
	if name == resource.CPU {
		return nil
	}
	return map[resource.Name]int{
		resource.Name("nvidia.com/gpu"): 1,
		"example.com/fpga":              1,
	}
}

// A type from another package of the same module is still an enum.
func speed(kind kinds.Kind) kinds.Kind {
	if kind == "slow" { // want "use a typed constant"
		return kinds.Fast
	}
	return kinds.Kind("fast") // want "use a typed constant"
}

// A switch over an external type is judged like one over a plain string.
func accelerator(name resource.Name) bool {
	switch name { // want "needs a defined string type"
	case "nvidia.com/gpu", "amd.com/gpu":
		return true
	}
	return false
}
