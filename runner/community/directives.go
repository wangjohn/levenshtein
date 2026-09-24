package community

import (
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The community linter's own codes. They use the reserved lvrules namespace,
// so no module can claim them, and every check that runs community rules
// selects them.
const (
	CodeMixed   = "lvrules_mixed"
	CodeRenamed = "lvrules_renamed"
)

// DirectivesURL documents both codes.
const DirectivesURL = "https://github.com/wangjohn/levenshtein/blob/main/docs/community-rules.md#ignore-directives"

// directive is one //lint:ignore or //lint:file-ignore comment and the codes
// it names, split the way Staticcheck splits them (analysis/lint's
// parseDirective).
type directive struct {
	Comment *ast.Comment
	Codes   []string
}

func directives(file *ast.File) []directive {
	var found []directive
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if codes, ok := ignoredCodes(comment.Text); ok {
				found = append(found, directive{Comment: comment, Codes: codes})
			}
		}
	}
	return found
}

// ignoredCodes returns the codes an ignore directive names, or false for any
// other comment.
func ignoredCodes(text string) ([]string, bool) {
	body, ok := strings.CutPrefix(text, "//lint:")
	if !ok {
		return nil, false
	}
	fields := strings.Split(body, " ")
	if (fields[0] != "ignore" && fields[0] != "file-ignore") || len(fields) < 2 {
		return nil, false
	}
	return strings.Split(fields[1], ","), true
}

// communityCode reports whether a code, or a glob in a directive, belongs to
// the community linter. Only community codes contain "_".
func communityCode(code string) bool {
	return strings.Contains(code, "_")
}

// mixedAnalyzer reports a directive that names core and community codes
// together. Each linter checks only its own directives for staleness, and each
// would report the other half of such a directive as unused.
func mixedAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: CodeMixed,
		Doc:  "report ignore directives that name core and community codes together\n\nEach linter checks only its own codes, so write one directive per linter on consecutive lines.",
		URL:  DirectivesURL,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, found := range directives(file) {
					var core, community []string
					for _, code := range found.Codes {
						if communityCode(code) {
							community = append(community, code)
						} else {
							core = append(core, code)
						}
					}
					if len(core) > 0 && len(community) > 0 {
						pass.Reportf(found.Comment.Pos(), "this directive names core codes (%s) and community codes (%s); write one directive for each on consecutive lines", strings.Join(core, ","), strings.Join(community, ","))
					}
				}
			}
			return nil, nil
		},
	}
}

// renamedAnalyzer reports a directive that uses a rule's old name. The old
// name no longer suppresses anything, and Staticcheck would not report the
// directive as unused, because the old name is no longer a rule.
func renamedAnalyzer(renames map[string]string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: CodeRenamed,
		Doc:  "report ignore directives that use a community rule's old name\n\nA renamed rule no longer answers to its old name, so the directive suppresses nothing.",
		URL:  DirectivesURL,
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, found := range directives(file) {
					for _, code := range found.Codes {
						if current, ok := renames[strings.ToLower(code)]; ok {
							pass.Reportf(found.Comment.Pos(), "%s", renamedMessage(code, current))
						}
					}
				}
			}
			return nil, nil
		},
	}
}

func renamedMessage(old, current string) string {
	return fmt.Sprintf("%s is now %s; update this directive", old, current)
}
