// Package units converts lengths.
package units

// Meter is the base unit.
const Meter = 1.0

// Convert turns centimeters into meters.
func Convert(centimeters float64) float64 {
	return centimeters / 100
}
