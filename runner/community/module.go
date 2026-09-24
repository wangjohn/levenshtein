package community

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Module is one rule module as the generated main registers it: the pin from
// levenshtein.json and what the module's lvrules package exports.
type Module struct {
	// Path and Version are the module's pin in levenshtein.json.
	Path    string
	Version string
	// Namespace is the module's lvrules.Namespace.
	Namespace string
	// Renamed is the module's lvrules.Renamed, mapping an old rule name to its
	// new one, or nil when the module exports none.
	Renamed map[string]string
	// Analyzers is the module's lvrules.Analyzers().
	Analyzers []*analysis.Analyzer
}

// Source names the module the way findings and messages do: path@version.
func (m Module) Source() string {
	return m.Path + "@" + m.Version
}

// ExampleModule is the only module that may use the reserved example
// namespace: the template in this repository's examples/rule-module.
const ExampleModule = "github.com/wangjohn/levenshtein/examples/rule-module"

// reservedNamespaces are never a publisher's namespace. lvrules names the
// community linter's own codes, and example belongs to ExampleModule.
var reservedNamespaces = []string{"lvrules", "example", "levenshtein", "lv"}

// namespacePattern is lowercase ASCII letters only. With no digits, no
// Staticcheck category pattern such as "SA1*" can match a community code, and
// the "_" that joins namespace and name appears in no core code.
var namespacePattern = regexp.MustCompile(`^[a-z]+$`)

// namePattern is an ASCII Go identifier. Patterns and ignore directives spell
// rule names, so they are kept to characters every tool handles the same way.
var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// ValidNamespace reports why a namespace cannot be used by the module at path,
// or nil when it can.
func ValidNamespace(path, namespace string) error {
	if !namespacePattern.MatchString(namespace) {
		return fmt.Errorf("namespace %q must be lowercase letters only", namespace)
	}
	if slices.Contains(reservedNamespaces, namespace) && (namespace != "example" || path != ExampleModule) {
		return fmt.Errorf("namespace %q is reserved", namespace)
	}
	return nil
}

// rule is one analyzer a module contributes, under its namespaced code.
type rule struct {
	Code       string
	Module     *Module
	Analyzer   *analysis.Analyzer
	Deprecated bool
}

// moduleRules validates a module's exports and returns its rules in the
// module's order, plus its renames from lowercased old code to current code.
func moduleRules(m *Module) ([]*rule, map[string]string, error) {
	if err := ValidNamespace(m.Path, m.Namespace); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", m.Source(), err)
	}
	if len(m.Analyzers) == 0 {
		return nil, nil, fmt.Errorf("%s: lvrules.Analyzers() returns no analyzers", m.Source())
	}

	var rules []*rule
	names := map[string]bool{}
	for i, analyzer := range m.Analyzers {
		if analyzer == nil {
			return nil, nil, fmt.Errorf("%s: lvrules.Analyzers()[%d] is nil", m.Source(), i)
		}
		if err := validAnalyzer(analyzer); err != nil {
			return nil, nil, fmt.Errorf("%s: analyzer %q: %w", m.Source(), analyzer.Name, err)
		}
		folded := strings.ToLower(analyzer.Name)
		if names[folded] {
			return nil, nil, fmt.Errorf("%s: two analyzers are named %q, ignoring case", m.Source(), analyzer.Name)
		}
		names[folded] = true
		rules = append(rules, &rule{
			Code:       m.Namespace + "_" + analyzer.Name,
			Module:     m,
			Analyzer:   analyzer,
			Deprecated: deprecated(analyzer.Doc),
		})
	}

	renames := map[string]string{}
	for old, current := range m.Renamed {
		if !namePattern.MatchString(old) || !namePattern.MatchString(current) {
			return nil, nil, fmt.Errorf("%s: lvrules.Renamed entry %q -> %q must map one rule name to another", m.Source(), old, current)
		}
		if names[strings.ToLower(old)] {
			return nil, nil, fmt.Errorf("%s: lvrules.Renamed lists %q as an old name, but a rule still uses it", m.Source(), old)
		}
		index := slices.IndexFunc(rules, func(rule *rule) bool { return strings.EqualFold(rule.Analyzer.Name, current) })
		if index < 0 {
			return nil, nil, fmt.Errorf("%s: lvrules.Renamed maps %q to %q, which is not a rule", m.Source(), old, current)
		}
		renames[strings.ToLower(m.Namespace+"_"+old)] = rules[index].Code
	}
	return rules, renames, nil
}

// validAnalyzer holds a rule to the contract in docs/community-rules.md: a
// name patterns can spell, a one-line summary, and a page that explains it.
func validAnalyzer(analyzer *analysis.Analyzer) error {
	if !namePattern.MatchString(analyzer.Name) {
		return fmt.Errorf("its Name must be an ASCII Go identifier")
	}
	if strings.TrimSpace(analyzer.Doc) == "" {
		return fmt.Errorf("its Doc is empty; start it with a one-line summary")
	}
	if analyzer.Run == nil {
		return fmt.Errorf("its Run function is nil")
	}
	page, err := url.Parse(analyzer.URL)
	if analyzer.URL == "" || err != nil || !slices.Contains(webSchemes, page.Scheme) || page.Host == "" {
		return fmt.Errorf("its URL %q must be an absolute http(s) page that says what the rule reports, why, how to fix it, and how to suppress it", analyzer.URL)
	}
	return nil
}

// webSchemes are the URL schemes a rule's page may use.
var webSchemes = []string{"https", "http"}

// deprecated reports whether a Doc carries a paragraph that starts with
// "Deprecated:", the Go convention for deprecation notices.
func deprecated(doc string) bool {
	for paragraph := range strings.SplitSeq(doc, "\n\n") {
		if strings.HasPrefix(strings.TrimSpace(paragraph), "Deprecated:") {
			return true
		}
	}
	return false
}

// titled gives a Doc the title Staticcheck's -list-checks shows. Staticcheck
// only takes a title from a Doc with a blank line after its first paragraph,
// so a one-paragraph Doc gains one after its first line.
func titled(doc string) string {
	if strings.Contains(doc, "\n\n") {
		return doc
	}
	title, rest, _ := strings.Cut(doc, "\n")
	return title + "\n\n" + rest
}
