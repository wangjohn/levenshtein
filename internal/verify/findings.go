package verify

import (
	"encoding/json"
	"path/filepath"

	"github.com/wangjohn/levenshtein/internal/checktool"
)

// finding and location are checktool's Finding and Location, the diagnostic
// shape the Dagger module attaches as its levenshteinFindings extension, so a
// report says the same thing however the check was executed.
//
// Hint and Baselined are the exception: no executor sets them. The CLI adds
// them to a finished report (see hints.go and baseline.go), after any result
// was cached, so they never reach a cached result or the runner.
type finding struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location location `json:"location"`
	// Source names the rule module a community finding came from, as
	// path@version; core findings leave it out.
	Source string `json:"source,omitempty"`
	// URL documents the rule that reported the finding, when it has a page.
	URL string `json:"url,omitempty"`
	// Advisory findings are reported without failing the check.
	Advisory bool `json:"advisory"`
	// Hint is a one-line fix the CLI adds to a finished report.
	Hint string `json:"hint,omitempty"`
	// Baselined findings are recorded in the repository's baseline and do
	// not fail the check.
	Baselined bool `json:"baselined,omitempty"`
}

type location = checktool.Location

// toolFindings is what checktool made of a tool's run, as native findings.
func toolFindings(found []checktool.Finding, err error) ([]finding, error) {
	if err != nil || found == nil {
		return nil, err
	}

	findings := make([]finding, 0, len(found))
	for _, diagnostic := range found {
		findings = append(findings, finding{
			Code:     diagnostic.Code,
			Message:  diagnostic.Message,
			Location: diagnostic.Location,
			Source:   diagnostic.Source,
			URL:      diagnostic.URL,
			Advisory: diagnostic.Advisory,
		})
	}
	return findings, nil
}

// repositoryPath reports a tool's file location the way the Dagger path does:
// relative to the source root, so the same diagnostic reads the same whichever
// executor produced it. A path outside the root is left alone.
func repositoryPath(root, file string) string {
	if root == "" || !filepath.IsAbs(file) {
		return file
	}

	rel, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(rel) {
		return file
	}
	return rel
}

// findingsDetails encodes diagnostics into the Details envelope the Dagger path
// produces, so consumers read one shape.
func findingsDetails(findings []finding) json.RawMessage {
	details, err := json.Marshal(struct {
		Findings []finding `json:"findings"`
	}{findings})
	if err != nil {
		return nil
	}
	return details
}
