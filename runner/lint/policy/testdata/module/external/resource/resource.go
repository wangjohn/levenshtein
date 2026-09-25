// Package resource stands in for a library type such as Kubernetes's
// corev1.ResourceName: a defined string type with a few well-known constants
// whose domain stays open to any name a caller spells.
package resource

type Name string

const CPU Name = "cpu"
