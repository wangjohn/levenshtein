package community

// Config is what one go-lint check asks of the community linter. The runner
// writes it as JSON from the check's planned rule modules and passes its path
// with -lvrules.config.
type Config struct {
	// Modules are the check's rule modules, as levenshtein.json declares them.
	Modules []ModuleConfig `json:"modules"`
	// Checks are the check's own lint.checks patterns that name community
	// rules. They apply after every module's select, so they win.
	Checks []string `json:"checks,omitempty"`
}

// ModuleConfig is one rule_modules entry of levenshtein.json.
type ModuleConfig struct {
	Path      string                       `json:"path"`
	Version   string                       `json:"version"`
	Namespace string                       `json:"namespace"`
	Select    []string                     `json:"select"`
	Advisory  []string                     `json:"advisory,omitempty"`
	Settings  map[string]map[string]string `json:"settings,omitempty"`
}

// Report is what the community linter writes to -lvrules.report once
// Staticcheck has finished. The file doubles as the completion marker: a run
// that leaves no report did not finish, whatever its exit code.
type Report struct {
	// Rules are the codes this run could report, including the linter's own
	// lvrules_* codes, with what the runner attaches to their findings.
	Rules []RuleReport `json:"rules"`
	// Warnings are problems that do not change the verdict.
	Warnings []Warning `json:"warnings,omitempty"`
	// Failures are rules that returned an error or panicked. Any failure makes
	// the check an error.
	Failures []Failure `json:"failures,omitempty"`
}

// RuleReport describes one selected rule.
type RuleReport struct {
	Code     string `json:"code"`
	Source   string `json:"source,omitempty"`
	URL      string `json:"url"`
	Advisory bool   `json:"advisory"`
}

// WarningKind names one kind of warning a check result carries.
type WarningKind string

const (
	// WarningRuleRenamed means a pattern, setting, or advisory entry uses a
	// rule's old name.
	WarningRuleRenamed WarningKind = "rule-renamed"
	// WarningRuleDeprecated means a selected rule is deprecated.
	WarningRuleDeprecated WarningKind = "rule-deprecated"
)

// Warning is one problem that does not change a check's verdict.
type Warning struct {
	Kind    WarningKind `json:"kind"`
	Message string      `json:"message"`
}

// Failure is one analyzer that returned an error or panicked, on every
// package where it did.
type Failure struct {
	// Code is the rule's code, or the name of a dependency analyzer that is
	// not a rule itself.
	Code string `json:"code"`
	// Source names the module that brought the analyzer in.
	Source   string   `json:"source"`
	Packages []string `json:"packages"`
	// Error is the first failure's error, so a report stays bounded.
	Error string `json:"error"`
}
