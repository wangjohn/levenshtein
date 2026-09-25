package verify

import (
	"encoding/json"
)

// sharedShape reports whether a kind's details carry findings in the shared
// {"findings": [...]} shape. semantic-lint keeps its own advisory report under
// the same key, with a different finding shape.
func sharedShape(kind CheckKind) bool {
	return kind != CheckSemanticLint
}

// detailFindings decodes the findings a result's details carry, or none when
// the details have no findings in the shared shape.
func detailFindings(details json.RawMessage) []finding {
	var envelope struct {
		Findings []finding `json:"findings"`
	}
	if len(details) == 0 || json.Unmarshal(details, &envelope) != nil {
		return nil
	}
	return envelope.Findings
}

// replaceFindings returns details with their findings replaced and every
// other field, such as go-mutation's summary, kept as it was.
func replaceFindings(details json.RawMessage, findings []finding) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(details) != 0 && json.Unmarshal(details, &fields) != nil {
		return details
	}
	if fields == nil { // Details that are JSON null.
		fields = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		return details
	}
	fields["findings"] = encoded
	replaced, err := json.Marshal(fields)
	if err != nil {
		return details
	}
	return replaced
}

// planned indexes a report's planned checks by ID, which is how a result names
// the check it came from.
func (r Report) planned() map[string]PlannedCheck {
	checks := make(map[string]PlannedCheck, len(r.Plan.Checks))
	for _, check := range r.Plan.Checks {
		checks[check.ID] = check
	}
	return checks
}

// mapFindings rewrites the findings of every result whose details are in the
// shared shape and carry at least one, leaving every other field alone.
func (r Report) mapFindings(rewrite func(PlannedCheck, []finding) []finding) Report {
	checks := r.planned()
	results := make([]Result, 0, len(r.Results))
	for _, result := range r.Results {
		check, ok := checks[result.ID]
		findings := detailFindings(result.Details)
		if ok && sharedShape(check.Check.Kind) && len(findings) != 0 {
			result = result.withDetails(replaceFindings(result.Details, rewrite(check, findings)))
		}
		results = append(results, result)
	}
	r.Results = results
	return r
}

func (r Result) withDetails(details json.RawMessage) Result {
	r.Details = details
	return r
}

// reportStatus is what a whole run concludes from its results: passed only
// when there was at least one check and every one passed.
func reportStatus(results []Result) Status {
	if len(results) == 0 {
		return StatusIncomplete
	}
	for _, result := range results {
		if result.Status != StatusPassed {
			return StatusFailed
		}
	}
	return StatusPassed
}
