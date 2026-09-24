package verify

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"
)

// Format is how the CLI writes a report. Every format describes the same
// report; none of them changes a status or the exit code.
type Format string

const (
	FormatJSON   Format = "json"
	FormatText   Format = "text"
	FormatGitHub Format = "github"
	FormatSARIF  Format = "sarif"
)

// Formats lists every format, in the order help text names them.
var Formats = []Format{FormatJSON, FormatText, FormatGitHub, FormatSARIF}

// RenderOptions tunes the formats other than JSON. PathPrefix is joined in
// front of every repository-relative path, for a report whose source is a
// subdirectory of the checkout that annotations and SARIF are relative to.
type RenderOptions struct {
	PathPrefix string
}

// locatedKinds report findings at a source location. Every other kind reports
// one finding per module whose message carries the tool's own output, and
// its location names the module directory rather than a file.
var locatedKinds = map[CheckKind]bool{
	CheckGoLint:     true,
	CheckGoHTTP:     true,
	CheckGoSQL:      true,
	CheckGoMutation: true,
	CheckGoImports:  true,
	CheckGoGenerate: true,
	CheckGoApidiff:  true,
	CheckShellLint:  true,
	CheckSecrets:    true,
	CheckDepsVuln:   true,
}

// item is one check's result as the renderers see it: its findings sorted by
// location, and whether each one names a file.
type item struct {
	check    PlannedCheck
	result   Result
	findings []finding
}

func (it item) located(f finding) bool {
	return locatedKinds[it.check.Check.Kind] || f.Code == baselineStaleCode
}

// items pairs each result with its planned check, in plan order.
func items(report Report) []item {
	checks := report.planned()
	out := make([]item, 0, len(report.Results))
	for _, result := range report.Results {
		check := checks[result.ID]
		var findings []finding
		if sharedShape(check.Check.Kind) {
			findings = detailFindings(result.Details)
		}
		slices.SortStableFunc(findings, compareFindings)
		out = append(out, item{check: check, result: result, findings: findings})
	}
	return out
}

func compareFindings(a, b finding) int {
	return cmp.Or(
		cmp.Compare(a.Location.File, b.Location.File),
		cmp.Compare(a.Location.Line, b.Location.Line),
		cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.Code, b.Code),
		cmp.Compare(a.Message, b.Message),
	)
}

func (o RenderOptions) path(file string) string {
	if o.PathPrefix == "" || o.PathPrefix == "." {
		return file
	}
	return path.Join(o.PathPrefix, file)
}

// Render writes a report in one format.
func Render(w io.Writer, report Report, format Format, options RenderOptions) error {
	switch format {
	case FormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case FormatText:
		return renderText(w, report, options)
	case FormatGitHub:
		return renderGitHub(w, report, options)
	case FormatSARIF:
		return renderSARIF(w, report, options)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// renderText writes every finding that fails the run, one per line as
// file:line:col: CODE message, grouped by check in plan order and sorted by
// location within a check, then one status line per check and a total.
// Findings the baseline accepts are counted, not listed.
func renderText(w io.Writer, report Report, options RenderOptions) error {
	var out strings.Builder
	hidden := 0
	for _, it := range items(report) {
		for _, f := range it.findings {
			if f.Baselined {
				hidden++
				continue
			}
			first, rest, _ := strings.Cut(strings.TrimSpace(f.Message), "\n")
			if it.located(f) {
				fmt.Fprintf(&out, "%s: %s %s\n", textPosition(options.path(f.Location.File), f.Location), f.Code, first)
			} else {
				fmt.Fprintf(&out, "%s: %s\n", options.path(f.Location.File), f.Code)
				rest = strings.TrimSpace(f.Message)
			}
			if rest != "" {
				out.WriteString(indent(rest))
			}
			if f.Hint != "" {
				fmt.Fprintf(&out, "    hint: %s\n", f.Hint)
			}
		}
	}
	if out.Len() != 0 {
		out.WriteString("\n")
	}

	table := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	passed := 0
	for _, it := range items(report) {
		if it.result.Status == StatusPassed {
			passed++
		}
		row := fmt.Sprintf("%s\t%s", it.result.ID, it.result.Status)
		if note := textStatus(it); note != "" {
			row += "\t" + note
		}
		// A tabwriter reports its write errors from Flush, below.
		_, _ = io.WriteString(table, row+"\n")
	}
	if err := table.Flush(); err != nil {
		return err
	}
	for _, it := range items(report) {
		if it.result.Status != StatusPassed && it.result.Status != StatusFailed && strings.Contains(strings.TrimSpace(it.result.Error), "\n") {
			fmt.Fprintf(&out, "\n%s:\n%s", it.result.ID, indent(strings.TrimSpace(it.result.Error)))
		}
	}

	fmt.Fprintf(&out, "\n%s: %s, %d of %d checks passed", report.Run, report.Status, passed, len(report.Results))
	if hidden > 0 {
		fmt.Fprintf(&out, ", %d baselined %s not shown", hidden, plural(hidden, "finding", "findings"))
	}
	out.WriteString("\n")
	_, err := io.WriteString(w, out.String())
	return err
}

func textPosition(file string, at location) string {
	if at.Column > 0 {
		return fmt.Sprintf("%s:%d:%d", file, at.Line, at.Column)
	}
	return fmt.Sprintf("%s:%d", file, at.Line)
}

// textStatus is the note after a check's status: what it found, or why it
// did not reach a verdict.
func textStatus(it item) string {
	failing, baselined := 0, 0
	for _, f := range it.findings {
		if f.Baselined {
			baselined++
		} else {
			failing++
		}
	}

	var notes []string
	if failing > 0 {
		notes = append(notes, fmt.Sprintf("%d %s", failing, plural(failing, "finding", "findings")))
	}
	if baselined > 0 {
		notes = append(notes, fmt.Sprintf("%d baselined", baselined))
	}
	if it.result.Status != StatusPassed && failing == 0 && it.result.Error != "" {
		first, _, _ := strings.Cut(strings.TrimSpace(it.result.Error), "\n")
		notes = append(notes, first)
	}
	if it.result.Cache.Status == CacheHit {
		notes = append(notes, "cached")
	}
	return strings.Join(notes, "; ")
}

func indent(text string) string {
	var out strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		out.WriteString("    " + line + "\n")
	}
	return out.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// renderGitHub writes GitHub Actions workflow commands: an error annotation
// for every finding that fails the run, at its file and line when it has one,
// and one for every check that did not pass without a finding to show for it,
// such as a tool error or an incomplete run.
func renderGitHub(w io.Writer, report Report, options RenderOptions) error {
	var out strings.Builder
	for _, it := range items(report) {
		shown := 0
		for _, f := range it.findings {
			if f.Baselined {
				continue
			}
			shown++
			message := strings.TrimSpace(f.Message)
			if f.Hint != "" {
				message += "\n\nhint: " + f.Hint
			}
			properties := []string{}
			if it.located(f) {
				properties = append(properties, "file="+escapeProperty(options.path(f.Location.File)), fmt.Sprintf("line=%d", f.Location.Line))
				if f.Location.Column > 0 {
					properties = append(properties, fmt.Sprintf("col=%d", f.Location.Column))
				}
			}
			properties = append(properties, "title="+escapeProperty(fmt.Sprintf("%s (%s)", f.Code, it.result.ID)))
			fmt.Fprintf(&out, "::error %s::%s\n", strings.Join(properties, ","), escapeData(message))
		}

		unfinished := it.result.Status != StatusPassed && it.result.Status != StatusFailed
		if unfinished || (it.result.Status == StatusFailed && shown == 0) {
			message := strings.TrimSpace(it.result.Error)
			if message == "" {
				message = string(it.result.Status)
			}
			title := fmt.Sprintf("Levenshtein %s %s", it.result.ID, it.result.Status)
			fmt.Fprintf(&out, "::error title=%s::%s\n", escapeProperty(title), escapeData(message))
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

// escapeData and escapeProperty follow the escaping GitHub's runner undoes
// for workflow commands (actions/toolkit, packages/core/src/command.ts), so a
// message cannot end the command early or start another one.
func escapeData(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}

func escapeProperty(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(value)
}

// SARIF 2.1.0, only the parts Levenshtein fills in.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string          `json:"id"`
	ShortDescription sarifMessage    `json:"shortDescription"`
	HelpURI          string          `json:"helpUri,omitempty"`
	Help             *sarifMessage   `json:"help,omitempty"`
	Properties       sarifProperties `json:"properties"`
}

type sarifProperties struct {
	Tags []string `json:"tags,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool                `json:"executionSuccessful"`
	Notifications       []sarifNotification `json:"toolExecutionNotifications,omitempty"`
}

type sarifNotification struct {
	Level   sarifLevel   `json:"level"`
	Message sarifMessage `json:"message"`
}

type sarifResult struct {
	RuleID        string              `json:"ruleId"`
	RuleIndex     int                 `json:"ruleIndex"`
	Level         sarifLevel          `json:"level"`
	Message       sarifMessage        `json:"message"`
	Locations     []sarifLocation     `json:"locations"`
	BaselineState sarifBaselineState  `json:"baselineState,omitempty"`
	Properties    sarifResultProperty `json:"properties"`
}

type sarifResultProperty struct {
	Check string    `json:"check"`
	Kind  CheckKind `json:"kind"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

type sarifLevel string

const (
	sarifError   sarifLevel = "error"
	sarifWarning sarifLevel = "warning"
)

type sarifBaselineState string

const (
	sarifNew       sarifBaselineState = "new"
	sarifUnchanged sarifBaselineState = "unchanged"
)

// staticcheckCode is the shape of Staticcheck's own rule names, which its
// documentation lists by name.
var staticcheckCode = regexp.MustCompile(`^(SA|S|ST|QF|U)[0-9]+$`)

func helpURI(code string) string {
	if doc, ok := ruleDocs[code]; ok {
		return doc
	}
	if staticcheckCode.MatchString(code) {
		return "https://staticcheck.dev/docs/checks/#" + code
	}
	return ""
}

// renderSARIF writes one SARIF run for the whole report, with a rule per
// finding code. GitHub code scanning treats a run as one tool's analysis and
// refuses several runs of the same tool in one upload, so the checks share a
// run and each result names its check and kind in its properties. Only
// findings with a source location become results; a module-level finding, and
// a check that did not reach a verdict, become tool notifications, because
// code scanning needs a file and line for every result. Paths are relative to
// %SRCROOT%, the checkout upload-sarif resolves them against.
func renderSARIF(w io.Writer, report Report, options RenderOptions) error {
	all := items(report)
	rules, index := sarifRules(all)

	// A scan's results are an array even when empty; code scanning rejects null.
	results := []sarifResult{}
	var notifications []sarifNotification
	successful := true
	for _, it := range all {
		if it.result.Status != StatusPassed && it.result.Status != StatusFailed {
			successful = false
			notifications = append(notifications, sarifNotification{Level: sarifError, Message: sarifMessage{Text: fmt.Sprintf("%s %s: %s", it.result.ID, it.result.Status, strings.TrimSpace(it.result.Error))}})
		}

		for _, f := range it.findings {
			if !it.located(f) {
				if !f.Baselined {
					notifications = append(notifications, sarifNotification{Level: sarifError, Message: sarifMessage{Text: fmt.Sprintf("%s %s: %s", it.result.ID, f.Code, strings.TrimSpace(f.Message))}})
				}
				continue
			}

			level, state := sarifError, sarifBaselineState("")
			if report.Baseline != nil {
				state = sarifNew
			}
			if f.Baselined {
				level, state = sarifWarning, sarifUnchanged
			}
			uri := (&url.URL{Path: options.path(f.Location.File)}).String()
			results = append(results, sarifResult{
				RuleID:        f.Code,
				RuleIndex:     index[f.Code],
				Level:         level,
				Message:       sarifMessage{Text: strings.TrimSpace(f.Message)},
				Locations:     []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: uri, URIBaseID: "%SRCROOT%"}, Region: sarifRegion{StartLine: max(f.Location.Line, 1), StartColumn: f.Location.Column}}}},
				BaselineState: state,
				Properties:    sarifResultProperty{Check: it.result.ID, Kind: it.check.Check.Kind},
			})
		}
	}

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:        sarifTool{Driver: sarifDriver{Name: "Levenshtein", InformationURI: "https://github.com/wangjohn/levenshtein", Rules: rules}},
			Invocations: []sarifInvocation{{ExecutionSuccessful: successful, Notifications: notifications}},
			Results:     results,
		}},
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(log)
}

// sarifRules describes every code a located finding uses, sorted by code, and
// says where each one sits in that list for the results to refer to.
func sarifRules(all []item) ([]sarifRule, map[string]int) {
	kinds := map[string]CheckKind{}
	for _, it := range all {
		for _, f := range it.findings {
			if _, seen := kinds[f.Code]; !seen && it.located(f) {
				kinds[f.Code] = it.check.Check.Kind
			}
		}
	}

	codes := make([]string, 0, len(kinds))
	for code := range kinds {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	rules := make([]sarifRule, 0, len(codes))
	index := make(map[string]int, len(codes))
	for i, code := range codes {
		var help *sarifMessage
		if hint, ok := hints[code]; ok {
			help = &sarifMessage{Text: strings.ReplaceAll(hint, "{file}", "<file>")}
		}
		rules = append(rules, sarifRule{ID: code, ShortDescription: sarifMessage{Text: code}, HelpURI: helpURI(code), Help: help, Properties: sarifProperties{Tags: []string{string(kinds[code])}}})
		index[code] = i
	}
	return rules, index
}
