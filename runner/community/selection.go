package community

import (
	"flag"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Pattern is one community selection pattern: an optional "-", a namespace,
// "_", and then a rule name, or a literal prefix ending in "*". Every copy of
// it loads runner/testdata/community-patterns.json in its tests.
var Pattern = regexp.MustCompile(`^-?[A-Za-z]+_([A-Za-z0-9_]*\*|[A-Za-z][A-Za-z0-9_]*)$`)

// matches reports whether a pattern without its "-" selects a code, ignoring
// case. A trailing "*" makes the rest a literal prefix, so "errs_*" is the
// whole namespace and "errs_no*" the errs rules whose names start with "no".
func matches(pattern, code string) bool {
	pattern, code = strings.ToLower(pattern), strings.ToLower(code)
	if prefix, glob := strings.CutSuffix(pattern, "*"); glob {
		return strings.HasPrefix(code, prefix)
	}
	return pattern == code
}

// setting is one analyzer flag value levenshtein.json asks for.
type setting struct {
	Rule  *rule
	Flag  string
	Value string
}

// plan is what one check runs: the selected rules and their Staticcheck
// flags, everything the report says about them, and the settings to apply.
type plan struct {
	Selected []*rule
	Advisory map[string]bool
	Renames  map[string]string
	Settings []setting
	Report   Report
}

// resolver holds every rule the compiled-in modules contribute, by lowercased
// code, while one check's configuration is resolved against them.
type resolver struct {
	modules  map[string]*Module
	rules    map[string]*rule
	ordered  []*rule
	renames  map[string]string
	warnings []Warning
	warned   map[string]bool
}

// resolve checks a check's configuration against the compiled-in modules and
// works out what the check runs. Every problem is an error naming its module:
// a pattern, advisory entry, or setting that matches nothing is almost always
// a misspelling that would otherwise leave a rule silently off.
func resolve(modules []Module, cfg Config) (plan, error) {
	r, err := newResolver(modules, cfg)
	if err != nil {
		return plan{}, err
	}

	var patterns []string
	for _, module := range cfg.Modules {
		if len(module.Select) == 0 {
			return plan{}, fmt.Errorf("%s: select must name at least one rule", module.Path)
		}
		if err := own(module, module.Select, "select"); err != nil {
			return plan{}, err
		}
		if err := own(module, module.Advisory, "advisory"); err != nil {
			return plan{}, err
		}
		patterns = append(patterns, module.Select...)
	}
	patterns = append(patterns, cfg.Checks...)
	enabled, err := r.apply(patterns, map[string]bool{})
	if err != nil {
		return plan{}, err
	}

	advisory := map[string]bool{}
	for _, module := range cfg.Modules {
		if _, err := r.apply(module.Advisory, advisory); err != nil {
			return plan{}, fmt.Errorf("advisory: %w", err)
		}
	}
	settings, err := r.settings(cfg.Modules)
	if err != nil {
		return plan{}, err
	}

	var selected []*rule
	report := Report{}
	for _, rule := range r.ordered {
		code := strings.ToLower(rule.Code)
		if !enabled[code] {
			continue
		}
		selected = append(selected, rule)
		report.Rules = append(report.Rules, RuleReport{Code: rule.Code, Source: rule.Module.Source(), URL: rule.Analyzer.URL, Advisory: advisory[code]})
		if rule.Deprecated {
			r.warn(WarningRuleDeprecated, "%s (%s) is deprecated: %s", rule.Code, rule.Module.Source(), notice(rule.Analyzer.Doc))
		}
	}
	for _, own := range []string{CodeMixed, CodeRenamed} {
		report.Rules = append(report.Rules, RuleReport{Code: own, URL: DirectivesURL})
	}
	report.Warnings = r.warnings

	return plan{Selected: selected, Advisory: advisory, Renames: r.renames, Settings: settings, Report: report}, nil
}

func newResolver(modules []Module, cfg Config) (*resolver, error) {
	r := &resolver{modules: map[string]*Module{}, rules: map[string]*rule{}, renames: map[string]string{}, warned: map[string]bool{}}
	owners := map[*analysis.Analyzer]string{}
	for i := range modules {
		module := &modules[i]
		if _, ok := r.modules[module.Namespace]; ok {
			return nil, fmt.Errorf("two modules use namespace %q", module.Namespace)
		}
		r.modules[module.Namespace] = module

		rules, renames, err := moduleRules(module)
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			if owner, shared := owners[rule.Analyzer]; shared {
				return nil, fmt.Errorf("%s and %s both contribute the analyzer %q; they would share its settings, so only one of them can", owner, module.Source(), rule.Analyzer.Name)
			}
			owners[rule.Analyzer] = module.Source()
			r.rules[strings.ToLower(rule.Code)] = rule
			r.ordered = append(r.ordered, rule)
		}
		maps.Copy(r.renames, renames)
	}

	configured := map[string]bool{}
	for _, entry := range cfg.Modules {
		module, ok := r.modules[entry.Namespace]
		switch {
		case !ok || module.Path != entry.Path:
			return nil, fmt.Errorf("levenshtein.json declares namespace %q for %s, but no compiled-in module has that namespace; is the namespace spelled as the module's lvrules.Namespace?", entry.Namespace, entry.Path)
		case module.Version != entry.Version:
			return nil, fmt.Errorf("%s: this linter was built with %s", entry.Path, module.Source())
		case configured[entry.Path]:
			return nil, fmt.Errorf("%s is configured twice", entry.Path)
		}
		configured[entry.Path] = true
	}
	for _, module := range modules {
		if !configured[module.Path] {
			return nil, fmt.Errorf("%s is compiled in but not configured", module.Source())
		}
	}
	return r, nil
}

// apply runs patterns over state in order, the last match winning, and
// returns state. Each pattern must name a declared namespace and match at
// least one of its rules.
func (r *resolver) apply(patterns []string, state map[string]bool) (map[string]bool, error) {
	for _, pattern := range patterns {
		if !Pattern.MatchString(pattern) {
			return nil, fmt.Errorf("pattern %q is not a community pattern such as \"errs_*\", \"errs_no*\", or \"-errs_nopanic\"", pattern)
		}
		body, disable := strings.CutPrefix(pattern, "-")
		namespace, _, _ := strings.Cut(strings.ToLower(body), "_")
		module, ok := r.modules[namespace]
		if !ok {
			return nil, fmt.Errorf("pattern %q names namespace %q, which no configured rule module uses", pattern, namespace)
		}
		body = r.current(body, fmt.Sprintf("pattern %q", pattern))

		matched := false
		for _, rule := range r.ordered {
			if rule.Module == module && matches(body, rule.Code) {
				state[strings.ToLower(rule.Code)] = !disable
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("pattern %q matches no rule in %s; its rules are %s", pattern, module.Source(), strings.Join(r.codes(module), ", "))
		}
	}
	return state, nil
}

// current maps an old rule code to its new one, warning where it was used.
// Globs are left alone: they only ever match current names.
func (r *resolver) current(code, where string) string {
	renamed, ok := r.renames[strings.ToLower(code)]
	if !ok {
		return code
	}
	r.warn(WarningRuleRenamed, "%s uses %s, which is now %s; update it", where, code, renamed)
	return renamed
}

// settings resolves each module's settings to rule flags. Two keys that reach
// the same rule, through case or an old name, are refused: one would silently
// override the other.
func (r *resolver) settings(modules []ModuleConfig) ([]setting, error) {
	var settings []setting
	for _, module := range modules {
		keys := map[*rule]string{}
		for _, code := range slices.Sorted(maps.Keys(module.Settings)) {
			resolved := strings.ToLower(r.current(code, fmt.Sprintf("the setting for %q", code)))
			rule, ok := r.rules[resolved]
			if !ok || rule.Module.Path != module.Path {
				return nil, fmt.Errorf("%s: settings name %q, which is not one of its rules (%s)", module.Path, code, strings.Join(r.codes(r.modules[module.Namespace]), ", "))
			}
			if other, twice := keys[rule]; twice {
				return nil, fmt.Errorf("%s: settings %q and %q both configure %s", module.Path, other, code, rule.Code)
			}
			keys[rule] = code

			values := module.Settings[code]
			for _, flag := range slices.Sorted(maps.Keys(values)) {
				if rule.Analyzer.Flags.Lookup(flag) == nil {
					return nil, fmt.Errorf("%s: %s has no setting %q; its settings are %s", rule.Module.Source(), rule.Code, flag, flagNames(rule.Analyzer))
				}
				settings = append(settings, setting{Rule: rule, Flag: flag, Value: values[flag]})
			}
		}
	}
	return settings, nil
}

// own checks that a module's select or advisory patterns stay in its own
// namespace, so one module's entry never changes another's rules.
func own(module ModuleConfig, patterns []string, field string) error {
	for _, pattern := range patterns {
		if !Pattern.MatchString(pattern) {
			return fmt.Errorf("%s: %s pattern %q is not a community pattern such as \"errs_*\", \"errs_no*\", or \"-errs_nopanic\"", module.Path, field, pattern)
		}
		namespace, _, _ := strings.Cut(strings.TrimPrefix(pattern, "-"), "_")
		if !strings.EqualFold(namespace, module.Namespace) {
			return fmt.Errorf("%s: %s pattern %q must start with the module's namespace, %s_", module.Path, field, pattern, module.Namespace)
		}
	}
	return nil
}

func (r *resolver) warn(kind WarningKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if r.warned[message] {
		return
	}
	r.warned[message] = true
	r.warnings = append(r.warnings, Warning{Kind: kind, Message: message})
}

func (r *resolver) codes(module *Module) []string {
	var codes []string
	for _, rule := range r.ordered {
		if rule.Module == module {
			codes = append(codes, rule.Code)
		}
	}
	return codes
}

func flagNames(analyzer *analysis.Analyzer) string {
	var names []string
	analyzer.Flags.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// notice is the Deprecated: paragraph of a Doc, on one line.
func notice(doc string) string {
	for paragraph := range strings.SplitSeq(doc, "\n\n") {
		if text, ok := strings.CutPrefix(strings.TrimSpace(paragraph), "Deprecated:"); ok {
			return strings.Join(strings.Fields(text), " ")
		}
	}
	return ""
}
