// Package complexity holds one function over gocognit's threshold of 30. The
// shipped default selection leaves gocognit off, so the default passes here and
// -checks=gocognit fails.
package complexity

// Grade is the result of scoring one submission.
type Grade string

const (
	GradePass Grade = "pass"
	GradeFail Grade = "fail"
	GradeSkip Grade = "skip"
)

// Score grades every submission against its answers. gocognit: each branch
// adds one plus its nesting depth, so the nested loops and conditions below add
// up to a cognitive complexity above 30.
func Score(submissions map[string][]string, answers map[string][]string, strict bool) map[string]Grade {
	grades := make(map[string]Grade, len(submissions))
	for name, submitted := range submissions {
		expected, ok := answers[name]
		if !ok {
			grades[name] = GradeSkip
			continue
		}
		correct := 0
		for i, answer := range submitted {
			if i >= len(expected) {
				if strict {
					correct--
				}
				continue
			}
			if answer == expected[i] {
				correct++
			} else if strict {
				for _, alternative := range expected {
					if alternative == answer {
						if correct > 0 {
							correct--
						}
					}
				}
			}
		}
		if correct*2 >= len(expected) && (!strict || len(submitted) == len(expected)) {
			grades[name] = GradePass
		} else {
			grades[name] = GradeFail
		}
	}
	return grades
}
