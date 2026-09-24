package community

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// staleDirective is how Staticcheck reports an ignore directive that
// suppressed nothing (lintcmd/lint.go in honnef.co/go/tools v0.8.1).
const staleDirective = "this linter directive didn't match anything; should it be removed?"

// Severities in Staticcheck's JSON output. A rule -fail leaves out reports as
// a warning.
const (
	severityError   = "error"
	severityWarning = "warning"
)

// jsonDiagnostic is the part of one Staticcheck JSON line this package reads.
// The line itself is kept, so every other field passes through unchanged.
type jsonDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
}

// advise rewrites Staticcheck's JSON output so a stale directive that names
// only advisory rules is advisory too, and returns the exit code that goes
// with the result: 1 while any diagnostic is still an error, and 0 otherwise.
// Staticcheck's own exit code stands when it is neither 0 nor 1, or when it
// reported 1 without printing a diagnostic, since both mean it could not
// finish.
func advise(output []byte, exit int, advisory func(code string) bool) ([]byte, int, error) {
	if exit != 0 && exit != 1 {
		return output, exit, nil
	}

	var rewritten bytes.Buffer
	failing, diagnostics := 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var diagnostic jsonDiagnostic
		if err := json.Unmarshal(line, &diagnostic); err != nil {
			return nil, 0, fmt.Errorf("reading Staticcheck's JSON output: %w", err)
		}
		diagnostics++

		if diagnostic.Code == "staticcheck" && diagnostic.Message == staleDirective && diagnostic.Severity == severityError && staleAdvisory(diagnostic, advisory) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(line, &fields); err != nil {
				return nil, 0, err
			}
			fields["severity"], _ = json.Marshal(severityWarning)
			encoded, err := json.Marshal(fields)
			if err != nil {
				return nil, 0, err
			}
			line = encoded
			diagnostic.Severity = severityWarning
		}
		if diagnostic.Severity != severityWarning {
			failing++
		}
		rewritten.Write(line)
		rewritten.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("reading Staticcheck's JSON output: %w", err)
	}

	if exit == 1 && diagnostics == 0 {
		return rewritten.Bytes(), 1, nil
	}
	if failing > 0 {
		return rewritten.Bytes(), 1, nil
	}
	return rewritten.Bytes(), 0, nil
}

// staleAdvisory reports whether the stale directive at a diagnostic's
// position names only advisory rules. Staticcheck reports the directive's own
// position, so the directive is read back from the source file.
func staleAdvisory(diagnostic jsonDiagnostic, advisory func(code string) bool) bool {
	data, err := os.ReadFile(filepath.Clean(diagnostic.Location.File))
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	if diagnostic.Location.Line < 1 || diagnostic.Location.Line > len(lines) {
		return false
	}
	line := lines[diagnostic.Location.Line-1]
	column := diagnostic.Location.Column - 1
	if column < 0 || column > len(line) {
		return false
	}
	codes, ok := ignoredCodes(strings.TrimRight(line[column:], "\r"))
	if !ok {
		return false
	}
	for _, code := range codes {
		if !advisory(code) {
			return false
		}
	}
	return true
}
