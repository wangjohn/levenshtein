# Community lint rules: rationale

Background for the [community lint rules](community-rules.md) proposal: how other
projects handle third-party rules, why community rules run in their own linter,
the alternatives considered, and the design reviews that shaped the spec. The
spec is the source of truth; this page records why it looks the way it does.

## What other projects do

| Project | How rules are added | Pinning and trust | Discovery |
| --- | --- | --- | --- |
| [golangci-lint module plugins](https://golangci-lint.run/docs/plugins/module-plugins/) | `.custom-gcl.yml` lists Go modules; `golangci-lint custom` rebuilds the binary with them compiled in. Plugins call `register.Plugin` and return `[]*analysis.Analyzer` | Module version, verified by `go.sum` and the checksum database | Its [linters list](https://golangci-lint.run/docs/linters/) covers bundled linters only; there is no plugin index |
| golangci-lint `.so` plugins | Go `plugin` package, loaded at run time | Must match the host's Go version, platform, and every shared dependency exactly | Deprecated in favor of module plugins |
| [TFLint](https://github.com/terraform-linters/tflint/blob/master/docs/user-guide/plugins.md) | Separate binaries speaking gRPC through `go-plugin` | Exact `version` only, no ranges; GitHub artifact attestations or PGP signatures | `tflint-ruleset-*` naming and a template repository |
| [ESLint](https://eslint.org/docs/latest/extend/plugins) | npm packages loaded in process; rules addressed as `plugin/rule` | npm lockfile; `eslint` as a peer dependency | `eslint-plugin-*` naming, npm keywords, awesome-eslint |
| [Semgrep Registry](https://github.com/semgrep/semgrep-rules) | Declarative YAML rules | Reviewed PRs with a CLA and a rule quality check in CI | Hosted registry; every rule needs `# ruleid:` and `# ok:` test cases |
| [pre-commit](https://pre-commit.com/) | Hook repositories that run as subprocesses | `rev` must be an immutable tag or SHA; `autoupdate --freeze` | A hooks page curated by PR |
| [Vale](https://vale.sh/docs/keys/packages) | Declarative style packages, `vale sync` | Versioned release archive URLs | Package explorer built from a library index |
| [nogo](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst) | Analyzers compiled into the build | Bazel module versions | None |
| [Ruff](https://docs.astral.sh/ruff/faq/) | No plugins by design; popular flake8 plugins are reimplemented under new prefixes | Not applicable | Not applicable |

Five patterns recur:

1. **One rule interface the ecosystem already uses.** For Go that interface is
   `*analysis.Analyzer`. golangci-lint, nogo, gopls, `go vet -vettool`, and
   Staticcheck's `lintcmd` all accept it, so a rule author writes it once.
2. **Build from source instead of loading binaries.** Go's `.so` plugins broke
   whenever the toolchain or a shared dependency changed, and golangci-lint
   replaced them with rebuilds. Dylint rebuilds against the exact Rust
   toolchain for the same reason.
3. **Exact, immutable pins.** TFLint rejects version ranges, and pre-commit
   caches by `rev` on the assumption that it never moves. Go retractions and
   crates.io yanks warn, and never break a build that already pins the version.
4. **Rule IDs owned by the plugin.** ESLint's `plugin/rule`, RuboCop's
   `Department/Cop`, and Vale's `Style.Rule` mean the same thing in every
   repository, so documentation, ignore comments, and answers online all carry
   over.
5. **Discovery that grows in steps.** First a naming convention, then a catalog
   curated by PR, and only later a hosted explorer. The catalog is data in
   git, so adding a rule is just a pull request.

## Why a separate linter

A prototype built both ways. What it showed:

| In one binary | In a separate binary |
| --- | --- |
| A community analyzer that exports a package fact crashed the whole linter with `interface conversion: analysis.Fact is *levenshtein.pkgFact, not *deprecated.IsDeprecated`, because Staticcheck's runner hands every package fact to its own `deprecated` analyzer. This happened even with the rule deselected | The same analyzer ran cleanly |
| A module could raise `golang.org/x/tools` under the core rules through minimum version selection and silently change core findings. Preventing that meant authors could never require a newer dependency than the pinned linter | Only Staticcheck and Go must stay at their pins. Other shared dependencies may move, and they only affect community rules |
| A community rule that crashes, writes to stdout, or calls `os.Exit(0)` takes core findings with it; `os.Exit(0)` makes the check pass | Core findings are unaffected. The community half fails with an error naming its modules, and a completion marker catches an early `os.Exit(0)` (see [errors](community-rules.md#errors)) |
| One package load | A second package load. On this repository's root module (4 CPUs, cold Staticcheck cache), the core linter took about 6.5 s and a community linter with two rules about 2.5 s, almost all of it loading. Checks without community rules pay nothing |

## Alternatives considered

| Option | Why it is not the recommendation |
| --- | --- |
| Compile community rules into `levenshtein-lint` (golangci-lint's model, and the spec's first draft) | The prototype crashed on a rule that exports a package fact, even with the rule deselected; a rule can hide core findings; and pinning shared dependencies to the core versions made authors' dependencies hostage to Levenshtein releases |
| Go `.so` plugins | The version skew that led golangci-lint to deprecate them; no Windows support |
| One vet tool per module (`go vet -vettool`, `unitchecker`) | Isolates each module, but every module loads the program again, findings leave `lintcmd`'s JSON, and `//lint:ignore` stops applying. The chosen design keeps one extra load for all community rules together |
| RPC plugins, as in TFLint | Needs a protocol designed and versioned here for something `go/analysis` already defines |
| Declarative rules only (Semgrep, ruleguard) | Lower risk and easy to review, but cannot express type-aware rules like `LV1001` or `LV1006`. They could be a second tier later: `go-ruleguard` bundles are also Go modules |
| A catalog in this repository | Puts every community rule in the main repository's review queue, which is what this proposal exists to avoid |
| Fetching catalog status during checks | Breaks reproducible pinned runs and needs network during checks; a module list shipped in each release does neither |

## Design reviews

Two rounds of independent review, each with one reviewer for product and user
experience and one for implementation, found the problems below. The spec resolves them. Findings marked "verified" were reproduced against a
prototype.

### First round

| Finding | Resolution |
| --- | --- |
| A rule exporting a package fact crashed the combined linter, even deselected (verified) | [Separate process](community-rules.md#running-community-rules) |
| "Off unless selected" still ran every declared rule, so a rule could hide or crash core findings (verified) | Separate process; only rules some check selects are registered |
| The dependency conflict check failed modules that only added dependencies, and kept authors on core versions (verified) | Only the Staticcheck and Go pins are checked |
| Namespaces with digits joined core categories: `sa1_nopanic` matched `SA1*` (verified) | [Lowercase letters only](community-rules.md#rule-names) |
| The pattern expression rejected `_`, so community codes could not be selected (verified) | [Selection](community-rules.md#selection) updates both copies |
| A consumer's `all` would turn on every community rule | `all` means core rules |
| Analyzer errors were swallowed and passed silently (verified) | Rule failures become [check errors](community-rules.md#errors) |
| Result keys cannot include sums resolved inside Dagger | [Keys](community-rules.md#result-keys) use declared entries |
| Import aliases collided with namespaces such as `community` or `type` (verified) | Positional aliases `m0`, `m1`, ... |
| Duplicate names were dropped silently, and `-list-checks` showed blank titles (verified) | Validation at build; a title line for `Doc` |
| Consumer-chosen namespaces made codes mean different things in different repositories | Publisher-owned namespaces; aliases only for collisions |
| Findings did not say where a rule came from or link to its docs | `source` and `url` on every finding |
| No rule settings, advisory mode, upgrade path, rename path, or autofix story | `settings`, [advisory](community-rules.md#advisory-findings), [upgrades](community-rules.md#upgrades-and-withdrawals), [renames](community-rules.md#renames-deprecation-and-graduation), fixes through `singlechecker -fix` |
| No governance, security reporting, or removal policy for the catalog | [Governance](community-rules.md#the-catalog) and `status` |
| "Verified" suggested an audit | "Builds with Levenshtein vX" |
| `levenshtein` as a package name and topic is hard to find | `lvrules` package, `levenshtein-lint-rules` topic |
| The example reported panics in tests and `main_test`, exempted `Mustard`, and missed package-level code (verified) | Fixed, with fixtures that fail when any exemption is removed |

### Second round

| Finding | Resolution |
| --- | --- |
| The lint step cannot type-check code without module sources, and the pinned Dagger cannot disable networking (verified) | A download step feeds sources as a plain directory; the network gap is stated, with a warning for private repositories, and closing it is a phase 3 item |
| The failure wrapper missed analyzers reached through `Requires`, reported failures inside the go-build cache, and could not catch `os.Exit(0)` (verified) | Clone the whole `Requires` graph; a failure file instead of findings; a completion marker |
| A directive mixing core and community codes was reported as unused by one linter or the other (verified) | `lvrules_mixed` asks for one directive per linter |
| `go mod tidy` could resolve different versions over time for a module with an incomplete `go.mod` (verified) | The build, and `scripts/test-example-rules`, fail on "finding module for package" |
| Advisory findings had no way into a passing result (verified: `-fail` gives severity `warning` and exit 0) | `GoLint` returns findings for passes; job summary and `jq` recipe show them |
| Most "exit 2" errors are only knowable after the build | [Errors](community-rules.md#errors) split by when they can be detected; `namespace` in the entry lets the CLI route patterns |
| Warnings had nowhere to appear, and vanished on cache hits | A `warnings` list stored with results, with every code listed |
| `withdrawn` needed network at run time, was bypassed by the cache, and broke pinned runs | A module list shipped in each release takes effect when the consumer updates |
| One namespace per module clashed with Go major versions and organizations with several modules | Publisher entries with several module paths; topic namespaces |
| Community rules made native `go-lint` checks a configuration error, blocking this repository's own setup | Native checks skip community rules with a warning; `"lint": {"rule_modules": false}` opts one check out |
| The precedence of `select`, `lint.checks`, `advisory`, and `-all` was undefined | An ordered pattern list and a table of effects |
| `Doc` normalization duplicated text in `-explain` (verified) | Only add a title line when there is none |
| Two modules returning one analyzer would share settings | Rejected at build |
| "Your first rule" needed files a template does not have | `gonew` scaffolding and a reusable `lvrules-check` action |
| The example required the exact pinned Go and `x/tools`, used `@latest`, and had no reserved namespace | Go 1.24 and `x/tools` v0.40.0; exact pins; `example` reserved |
| No compatibility policy for the `lvrules` contract, and hand-typed catalog metadata | Optional additive exports; catalog rule lists generated from the module |
| The catalog's `verified` field contradicted its label, `deprecated` also meant "broken", and governance had no named owners or approval for dependency changes | `builds_with`, a separate `broken` status, two named maintainers, and approval for `go.mod` changes |
| The Renovate regex depended on key order (verified) | A JSONata manager |
| The example reported panics in generated files (verified) | Skipped, with a fixture |
| Phase 1's gate was a single request, and the top-level `rules` field collided with the word for one rule | Two unrelated requests; the field is `rule_modules` |
