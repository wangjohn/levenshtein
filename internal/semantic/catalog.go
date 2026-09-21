package semantic

import "fmt"

// CatalogVersion changes whenever question wording, criteria, or thresholds
// change, so recorded answers can be compared across revisions.
const CatalogVersion = "0.2"

// Scope is the unit of state a question is asked about.
type Scope string

const (
	ScopeGoHunk   Scope = "go-hunk"
	ScopeDocsHunk Scope = "docs-hunk"
	ScopeChange   Scope = "change"
)

// Severity orders findings; nothing here changes the check outcome in advisory mode.
type Severity string

const (
	SeverityImportant Severity = "important"
	SeverityMinor     Severity = "minor"
	SeverityNit       Severity = "nit"
)

// Direction says which side of the threshold is a finding. Quality questions
// are phrased in the positive and fire when the probability is low.
type Direction string

const (
	DirectionHigh Direction = "high"
	DirectionLow  Direction = "low"
)

// Items names the preselected list a question is asked once per element of.
// Code decides what is in each list; the model only judges each element.
type Items string

const (
	ItemsNone      Items = ""
	ItemsComments  Items = "comments"
	ItemsErrors    Items = "errors"
	ItemsSentences Items = "sentences"
	ItemsCommits   Items = "commits"
)

// Question is one bounded judgment. Text refers to state fields in backticks;
// `item.` is rewritten to the concrete list element when Items is set.
type Question struct {
	ID            string
	Scope         Scope
	Primitive     Primitive
	Severity      Severity
	Threshold     float64
	Direction     Direction
	ConfidenceMin float64
	Items         Items
	Text          string
	Inspect       string
	True          string
	False         string
	Levels        []string
	Issue         string
}

// Catalog is the advisory question set, in the order findings are reported.
var Catalog = []Question{
	{
		ID:        "comment_explains_why",
		Scope:     ScopeGoHunk,
		Primitive: PrimitiveNoul,
		Severity:  SeverityMinor,
		Threshold: 0.4,
		Direction: DirectionLow,
		Items:     ItemsComments,
		Text:      "Does `item.comment` give a reason for `item.code` that a reader could not infer from the code itself?",
		Inspect:   "item",
		True:      "The comment states a purpose, constraint, or consequence not visible in the adjacent code, such as why a value is reset or what a caller relies on.",
		False:     "The comment restates what the adjacent code does, or names the operation without giving a reason.",
		Issue:     "The comment restates the code instead of explaining why.",
	},
	{
		ID:        "error_message_actionable",
		Scope:     ScopeGoHunk,
		Primitive: PrimitiveNoul,
		Severity:  SeverityMinor,
		Threshold: 0.4,
		Direction: DirectionLow,
		Items:     ItemsErrors,
		Text:      "Does the error built by `item.call` tell a reader what was wrong and which value, path, or identifier was involved?",
		Inspect:   "item",
		True:      "The message names the failing input or condition and the violated expectation, or wraps an inner error with %w that carries that detail.",
		False:     "The message is generic, such as 'invalid input' or 'operation failed', with no value named and no wrapped error.",
		Issue:     "The error message does not say what was wrong or which value was involved.",
	},
	{
		ID:        "duplicates_package_helper",
		Scope:     ScopeGoHunk,
		Primitive: PrimitiveNoul,
		Severity:  SeverityImportant,
		Threshold: 0.7,
		Direction: DirectionHigh,
		Text:      "Does the new code in `hunk.after` perform an operation that a function in `neighbours.package_functions` already provides?",
		Inspect:   "hunk.after",
		True:      "The new code reimplements the same operation inline instead of calling the listed function.",
		False:     "The new code calls the listed function, or performs a different operation that only shares names or types with it.",
		Issue:     "The new code appears to reimplement an existing package helper.",
	},
	{
		ID:        "evaluative_wording",
		Scope:     ScopeDocsHunk,
		Primitive: PrimitiveNoul,
		Severity:  SeverityNit,
		Threshold: 0.8,
		Direction: DirectionHigh,
		Text:      "Does `hunk.diff` add wording that praises or evaluates the software instead of stating what it does or requires?",
		Inspect:   "hunk.diff",
		True:      "Added text uses evaluative terms such as robust, seamless, powerful, easy, or best-in-class.",
		False:     "Added text states behavior, requirements, limits, or examples, even when the facts are favorable.",
		Issue:     "Added documentation evaluates the software instead of describing it.",
	},
	{
		ID:        "unscoped_guarantee",
		Scope:     ScopeDocsHunk,
		Primitive: PrimitiveNoul,
		Severity:  SeverityMinor,
		Threshold: 0.75,
		Direction: DirectionHigh,
		Items:     ItemsSentences,
		Text:      "Does `item.sentence` assert an absolute guarantee about behavior without naming the mechanism, condition, or scope that enforces it?",
		Inspect:   "item.sentence",
		True:      "The sentence makes an absolute claim and neither it nor its neighbors in `hunk.after` say how or where it is enforced.",
		False:     "The sentence names the enforcing mechanism, states its scope, or explicitly disclaims the guarantee.",
		Issue:     "An absolute claim names no mechanism or scope that enforces it.",
	},
	{
		ID:            "scope_creep",
		Scope:         ScopeChange,
		Primitive:     PrimitiveScore,
		Severity:      SeverityImportant,
		Threshold:     1.5,
		Direction:     DirectionHigh,
		ConfidenceMin: 0.6,
		Text:          "How many independent changes does this pull request bundle, judging from `commits` and `files`?",
		Inspect:       "commits",
		Levels: []string{
			"One change, with every edit serving it, including its tests and docs.",
			"One primary change plus a small unrelated tweak alongside it.",
			"Two or more independent changes that could each be reviewed and reverted on their own.",
		},
		Issue: "The change bundles independent work that could be reviewed separately.",
	},
	{
		ID:        "commit_subject_matches",
		Scope:     ScopeChange,
		Primitive: PrimitiveNoul,
		Severity:  SeverityNit,
		Threshold: 0.4,
		Direction: DirectionLow,
		Items:     ItemsCommits,
		Text:      "Does `item.subject` describe the change visible in `item.files` and `item.hunk_headers`?",
		Inspect:   "item",
		True:      "The subject names the behavior, rule, or policy that changed, and the touched files fit it.",
		False:     "The subject is generic, such as fix, update, or wip, or names something the touched files do not reflect.",
		Issue:     "The commit subject does not describe the files it changes.",
	},
	{
		ID:        "behavior_change_undocumented",
		Scope:     ScopeChange,
		Primitive: PrimitiveNoul,
		Severity:  SeverityImportant,
		Threshold: 0.7,
		Direction: DirectionHigh,
		Text:      "Does `change.summary` alter a configuration key, CLI flag, output field, default, or check behavior a user would rely on, while `docs_diff` does not mention it?",
		Inspect:   "change.summary",
		True:      "A user-visible key, flag, field, default, or behavior changed and no added documentation text mentions it.",
		False:     "The change is internal, or `docs_diff` describes it.",
		Issue:     "A user-visible change has no matching documentation change.",
	},
	{
		ID:        "describes_unimplemented",
		Scope:     ScopeChange,
		Primitive: PrimitiveNoul,
		Severity:  SeverityImportant,
		Threshold: 0.7,
		Direction: DirectionHigh,
		Text:      "Does `docs_diff` describe a capability as available or implemented that `change.summary` does not add?",
		Inspect:   "docs_diff",
		True:      "Added documentation presents a capability as available, and nothing in `change.summary` implements it.",
		False:     "The capability is implemented in the change, or the text marks it as planned or next.",
		Issue:     "Documentation presents as available something this change does not implement.",
	},
}

var catalogByID = indexCatalog()

func indexCatalog() map[string]Question {
	index := map[string]Question{}
	for _, q := range Catalog {
		if _, dup := index[q.ID]; dup {
			panic(fmt.Sprintf("duplicate semantic question %q", q.ID))
		}
		index[q.ID] = q
	}
	return index
}

func questionsFor(scope Scope) []Question {
	var selected []Question
	for _, q := range Catalog {
		if q.Scope == scope {
			selected = append(selected, q)
		}
	}
	return selected
}
