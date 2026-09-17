package typed

type Job struct{ Priority string }

func priority(job Job, choice string) {
	switch job.Priority { // want "string choice with multiple alternatives"
	case "high", "medium", "low":
	}
	if choice == "small" || choice == "medium" || choice == "large" { // want "string choice with multiple alternatives"
	}
	if "small" != choice && "large" != choice { // want "string choice with multiple alternatives"
	}
	local := choice
	switch local { // want "string choice with multiple alternatives"
	case "one":
	case "two":
	}
}
func ordinary(filename string, a, b Job, status Status) {
	if filename == "README.md" {
	}
	if filename == "" || filename == "README.md" {
	}
	if a.Priority == "high" || b.Priority == "low" {
	}
	switch filename {
	case "README.md":
	}
	switch status {
	case Passed, Failed:
	}
	if a.Priority == "high" && a.Priority == "low" {
	}
}

const high = "high"

func constants(priority string) {
	switch priority { // want "string choice with multiple alternatives"
	case "", high, "lo" + "w":
	}
	if priority == "" || priority == high || priority == "low" { // want "string choice with multiple alternatives"
	}
}
func nestedChoices(a, b string) {
	switch a { // want "string choice with multiple alternatives"
	case "one", "two":
		switch b { // want "string choice with multiple alternatives"
		case "three", "four":
		}
	}
}
func unstable(values []string, f func() string) {
	switch f() {
	case "one", "two":
	}
	if values[0] == "one" || values[0] == "two" {
	}
}

type Priority string
type PriorityAlias = Priority

func definedChoices(priority Priority, alias PriorityAlias) {
	switch priority { // want "string choice with multiple alternatives"
	case "high", "low":
	}
	if priority == "high" || priority == "low" { // want "string choice with multiple alternatives"
	}
	if "high" != alias && "low" != alias { // want "string choice with multiple alternatives"
	}
	if priority == "high" { // One special value does not establish an enum.
	}
}

func localConstants(priority Priority) {
	const high Priority = "high"
	const low Priority = "low"
	switch priority {
	case high, low:
	}
	if priority == high || priority == low {
	}
	if priority == high || priority == "low" { // want "string choice with multiple alternatives"
	}
}
