package semantic

import (
	"regexp"
	"strings"
)

type sentenceItem struct {
	Sentence string `json:"sentence"`
	Line     int    `json:"line"`
}

// docUnit is one Markdown hunk with its enclosing section and the added
// sentences that make absolute claims.
type docUnit struct {
	Path      string
	Line      int
	Diff      string
	After     string
	Prose     bool
	Sentences []sentenceItem
}

const (
	maxSectionLines = 120
	maxSectionChars = 12000
)

var (
	headingLine  = regexp.MustCompile(`^#{1,6}\s`)
	absoluteWord = regexp.MustCompile(`(?i)\b(always|never|cannot|can't|guarantee[sd]?|complete(ly)?|immutable|impossible)\b`)
)

func docUnits(path string, src []byte, hunks []Hunk) []docUnit {
	lines := strings.Split(string(src), "\n")
	fenced := fenceState(lines)

	var units []docUnit
	for _, hunk := range hunks {
		if hunk.NewLines == 0 {
			continue
		}
		first, last := hunk.NewStart, hunk.NewStart+hunk.NewLines-1

		prose := false
		var sentences []sentenceItem
		for offset, text := range hunk.Added {
			line := first + offset
			if line-1 < len(fenced) && fenced[line-1] {
				continue
			}
			trimmed := strings.TrimSpace(text)
			if trimmed == "" || strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "```") || headingLine.MatchString(trimmed) {
				continue
			}
			prose = true
			for _, sentence := range splitSentences(trimmed) {
				if absoluteWord.MatchString(sentence) {
					sentences = append(sentences, sentenceItem{Sentence: sentence, Line: line})
				}
			}
		}

		units = append(units, docUnit{
			Path:      path,
			Line:      first,
			Diff:      hunk.Diff,
			After:     truncate(section(lines, first, last), maxSectionChars),
			Prose:     prose,
			Sentences: sentences,
		})
	}
	return units
}

// fenceState marks lines inside fenced code blocks, which are not prose.
func fenceState(lines []string) []bool {
	inside := make([]bool, len(lines))
	open := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			open = !open
			inside[i] = true
			continue
		}
		inside[i] = open
	}
	return inside
}

// section returns the enclosing heading section, narrowed around the hunk when long.
func section(lines []string, first, last int) string {
	start := first
	for start > 1 && !headingLine.MatchString(lines[start-2]) {
		start--
	}
	if start > 1 {
		start--
	}
	end := last
	for end < len(lines) && !headingLine.MatchString(lines[end]) {
		end++
	}

	if end-start+1 > maxSectionLines {
		margin := max((maxSectionLines-(last-first+1))/2, 5)
		if first-margin > start {
			start = first - margin
		}
		if last+margin < end {
			end = last + margin
		}
	}
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

// splitSentences cuts prose at terminal punctuation followed by whitespace.
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	runes := []rune(text)
	for i, r := range runes {
		terminal := r == '.' || r == '!' || r == '?'
		if !terminal || i+1 < len(runes) && runes[i+1] != ' ' {
			continue
		}
		sentence := strings.TrimSpace(string(runes[start : i+1]))
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
		start = i + 1
	}
	if rest := strings.TrimSpace(string(runes[start:])); rest != "" {
		sentences = append(sentences, rest)
	}
	return sentences
}
