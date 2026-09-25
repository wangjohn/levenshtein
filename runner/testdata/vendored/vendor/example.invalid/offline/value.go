package offline

const Value = "vendored"

// Resource names a few well-known values and accepts any other, like
// Kubernetes's corev1.ResourceName.
type Resource string

const CPU Resource = "cpu"
