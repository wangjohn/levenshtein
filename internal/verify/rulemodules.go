package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/module"
)

// PlannedRuleModule is one rule module a go-lint check runs, as the plan
// reports it and the Dagger runner receives it. runner/community's
// ModuleConfig reads the same JSON; change both together.
type PlannedRuleModule struct {
	Path      string                       `json:"path"`
	Version   string                       `json:"version"`
	Namespace string                       `json:"namespace"`
	Select    []string                     `json:"select"`
	Advisory  []string                     `json:"advisory,omitempty"`
	Settings  map[string]map[string]string `json:"settings,omitempty"`
}

// communityPattern is one community selection pattern: an optional "-", a
// namespace, "_", and then a rule name, or a literal prefix ending in "*".
// runner/community has the original (Pattern); both tests load
// runner/testdata/community-patterns.json.
var communityPattern = regexp.MustCompile(`^-?[A-Za-z]+_([A-Za-z0-9_]*\*|[A-Za-z][A-Za-z0-9_]*)$`)

// communityCode is a literal community rule code, as settings keys and
// advisory names spell it.
var communityCode = regexp.MustCompile(`^[A-Za-z]+_[A-Za-z][A-Za-z0-9_]*$`)

// namespacePattern is lowercase ASCII letters only; runner/community explains
// why. reservedNamespaces are never a publisher's, and example belongs to the
// template in this repository.
var (
	namespacePattern   = regexp.MustCompile(`^[a-z]+$`)
	reservedNamespaces = []string{"lvrules", "example", "levenshtein", "lv"}
)

// exampleModule is the only module that may use the example namespace.
const exampleModule = "github.com/wangjohn/levenshtein/examples/rule-module"

// isCommunityPattern reports whether a go-lint pattern belongs to the
// community linter. Only community codes contain "_".
func isCommunityPattern(pattern string) bool {
	return strings.Contains(pattern, "_")
}

// patternNamespace is the lowercased namespace a community pattern names.
func patternNamespace(pattern string) string {
	namespace, _, _ := strings.Cut(strings.TrimPrefix(pattern, "-"), "_")
	return strings.ToLower(namespace)
}

// validateRuleModules checks everything about rule_modules that needs no
// build: pins, namespaces, pattern syntax, and that every community pattern a
// go-lint check uses names a declared namespace. Whether a pattern matches a
// rule is only known once the community linter is built.
func (cfg Config) validateRuleModules() error {
	namespaces := map[string]string{}
	for _, path := range slices.Sorted(maps.Keys(cfg.RuleModules)) {
		entry := cfg.RuleModules[path]
		if err := validateRuleModule(path, entry); err != nil {
			return fmt.Errorf("rule_modules %q: %w", path, err)
		}
		if other, taken := namespaces[entry.Namespace]; taken {
			return fmt.Errorf("rule_modules %q and %q both declare namespace %q; only one module per namespace can be used", other, path, entry.Namespace)
		}
		namespaces[entry.Namespace] = path
	}

	for _, id := range slices.Sorted(maps.Keys(cfg.Checks)) {
		check := cfg.Checks[id]
		var community []string
		for _, pattern := range check.lintChecks() {
			if isCommunityPattern(pattern) {
				community = append(community, pattern)
			}
		}
		if len(community) > 0 && !check.usesRuleModules() {
			return fmt.Errorf("check %q: lint.checks names community rules (%s) but the check sets rule_modules to false", id, strings.Join(community, ", "))
		}
		for _, pattern := range community {
			if _, ok := namespaces[patternNamespace(pattern)]; !ok {
				return fmt.Errorf("check %q: go-lint check %q names namespace %q, which no rule_modules entry declares", id, pattern, patternNamespace(pattern))
			}
		}
	}
	return cfg.validateAdvisory()
}

func validateRuleModule(path string, entry RuleModule) error {
	if err := module.CheckPath(path); err != nil {
		return err
	}
	if err := module.Check(path, entry.Version); err != nil || module.CanonicalVersion(entry.Version) != entry.Version {
		return fmt.Errorf("version %q must be an exact tag or pseudo-version for this module path, such as v1.4.0; branches and latest are not pins", entry.Version)
	}
	if !namespacePattern.MatchString(entry.Namespace) {
		return fmt.Errorf("namespace %q must be lowercase letters only", entry.Namespace)
	}
	if slices.Contains(reservedNamespaces, entry.Namespace) && (entry.Namespace != "example" || path != exampleModule) {
		return fmt.Errorf("namespace %q is reserved", entry.Namespace)
	}
	if len(entry.Select) == 0 {
		return errors.New("select must name at least one rule, such as \"" + entry.Namespace + "_*\"")
	}

	for _, field := range []struct {
		Name     string
		Patterns []string
	}{{"select", entry.Select}, {"advisory", entry.Advisory}} {
		for _, pattern := range field.Patterns {
			if !communityPattern.MatchString(pattern) || patternNamespace(pattern) != entry.Namespace {
				return fmt.Errorf("%s pattern %q must be a pattern in this module's namespace, such as \"%s_*\" or \"-%s_rule\"", field.Name, pattern, entry.Namespace, entry.Namespace)
			}
		}
	}
	seen := map[string]string{}
	for _, code := range slices.Sorted(maps.Keys(entry.Settings)) {
		if !communityCode.MatchString(code) || patternNamespace(code) != entry.Namespace {
			return fmt.Errorf("settings key %q must be one rule's code in this module's namespace, such as \"%s_rule\"", code, entry.Namespace)
		}
		if other, twice := seen[strings.ToLower(code)]; twice {
			return fmt.Errorf("settings keys %q and %q name the same rule; codes ignore case", other, code)
		}
		seen[strings.ToLower(code)] = code
		values := entry.Settings[code]
		for _, name := range slices.Sorted(maps.Keys(values)) {
			value := values[name]
			if name == "" || strings.ContainsAny(name, "=\x00") || strings.ContainsRune(value, 0) {
				return fmt.Errorf("settings for %q: invalid flag %q", code, name)
			}
		}
	}
	return nil
}

// validateAdvisory refuses an advisory rule no go-lint check selects: it
// would never report, so the entry is almost always a mistake. Only literal
// names can be checked before the build; the community linter checks that
// every advisory pattern matches a rule.
func (cfg Config) validateAdvisory() error {
	var selections [][]string
	for _, id := range slices.Sorted(maps.Keys(cfg.Checks)) {
		if check := cfg.Checks[id]; check.usesRuleModules() {
			selections = append(selections, cfg.communitySelection(check))
		}
	}

	for _, path := range slices.Sorted(maps.Keys(cfg.RuleModules)) {
		for _, pattern := range cfg.RuleModules[path].Advisory {
			if strings.HasPrefix(pattern, "-") || strings.HasSuffix(pattern, "*") {
				continue
			}
			if !slices.ContainsFunc(selections, func(patterns []string) bool { return communityAllowed(patterns, pattern) }) {
				return fmt.Errorf("rule_modules %q: advisory rule %q is not selected by any go-lint check", path, pattern)
			}
		}
	}
	return nil
}

// communitySelection is a check's community pattern list in the order the
// community linter applies it: every module's select, then the check's own.
func (cfg Config) communitySelection(check Check) []string {
	var patterns []string
	for _, path := range slices.Sorted(maps.Keys(cfg.RuleModules)) {
		patterns = append(patterns, cfg.RuleModules[path].Select...)
	}
	for _, pattern := range check.lintChecks() {
		if isCommunityPattern(pattern) {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// communityAllowed applies community patterns to one code, the last match
// winning, the way runner/community does.
func communityAllowed(patterns []string, code string) bool {
	selected := false
	for _, pattern := range patterns {
		body, disable := strings.CutPrefix(strings.ToLower(pattern), "-")
		lowered := strings.ToLower(code)
		prefix, glob := strings.CutSuffix(body, "*")
		if (glob && strings.HasPrefix(lowered, prefix)) || (!glob && body == lowered) {
			selected = !disable
		}
	}
	return selected
}

// plannedRuleModules is what a check runs from rule_modules, sorted by path,
// or nil when it runs none.
func (cfg Config) plannedRuleModules(check Check) []PlannedRuleModule {
	if !check.usesRuleModules() {
		return nil
	}

	var planned []PlannedRuleModule
	for _, path := range slices.Sorted(maps.Keys(cfg.RuleModules)) {
		entry := cfg.RuleModules[path]
		planned = append(planned, PlannedRuleModule{
			Path:      path,
			Version:   entry.Version,
			Namespace: entry.Namespace,
			Select:    entry.Select,
			Advisory:  entry.Advisory,
			Settings:  entry.Settings,
		})
	}
	return planned
}

// RuleModuleStatus is what a Levenshtein release says about one rule module
// version in runner/rule-modules.json.
type RuleModuleStatus string

const (
	// RuleModuleDeprecated versions still run, with a warning.
	RuleModuleDeprecated RuleModuleStatus = "deprecated"
	// RuleModuleWithdrawn versions are malicious or dangerous and are a
	// configuration error.
	RuleModuleWithdrawn RuleModuleStatus = "withdrawn"
)

// RuleModuleNotice is one entry of runner/rule-modules.json.
type RuleModuleNotice struct {
	Path        string           `json:"path"`
	Version     string           `json:"version"`
	Status      RuleModuleStatus `json:"status"`
	Reason      string           `json:"reason"`
	Replacement string           `json:"replacement,omitempty"`
}

// ruleModuleNotices reads the shipped list from the shared checkout. The file
// is part of the implementation snapshot, so a change to it reruns the checks
// that use rule modules.
func ruleModuleNotices(shared string) ([]RuleModuleNotice, error) {
	data, err := os.ReadFile(filepath.Join(shared, "runner", "rule-modules.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var shipped struct {
		Comment string             `json:"comment"`
		Modules []RuleModuleNotice `json:"modules"`
	}
	if err := decode(data, &shipped); err != nil {
		return nil, fmt.Errorf("runner/rule-modules.json: %w", err)
	}
	for _, notice := range shipped.Modules {
		if notice.Status != RuleModuleDeprecated && notice.Status != RuleModuleWithdrawn {
			return nil, fmt.Errorf("runner/rule-modules.json: %s@%s has unknown status %q", notice.Path, notice.Version, notice.Status)
		}
	}
	return shipped.Modules, nil
}

// CheckWithdrawnRuleModules refuses a plan that runs a rule module version the
// shared checkout's release lists as withdrawn. Checks never contact the
// catalog; the list ships with each release, so a pinned run always gives the
// same answer.
func CheckWithdrawnRuleModules(plan Plan, shared string) error {
	notices, err := ruleModuleNotices(shared)
	if err != nil {
		return err
	}

	for _, check := range plan.Checks {
		for _, planned := range check.RuleModules {
			for _, notice := range notices {
				if notice.Status == RuleModuleWithdrawn && notice.Path == planned.Path && notice.Version == planned.Version {
					message := fmt.Sprintf("rule_modules %q: %s is withdrawn: %s", planned.Path, planned.Version, notice.Reason)
					if notice.Replacement != "" {
						message += "; use " + notice.Replacement + " instead"
					}
					return errors.New(message)
				}
			}
		}
	}
	return nil
}

// encodeRuleModules is the JSON the Dagger runner receives for a check's rule
// modules.
func encodeRuleModules(planned []PlannedRuleModule) (string, error) {
	data, err := json.Marshal(planned)
	return string(data), err
}
