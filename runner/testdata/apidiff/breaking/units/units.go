// Package units converts lengths.
package units

// Meter is the base unit.
const Meter = 1.0

// Convert turns a length in the named unit into meters. Its new parameter
// breaks every caller.
func Convert(length float64, unit string) float64 {
	if unit == "cm" {
		return length / 100
	}
	return length
}
