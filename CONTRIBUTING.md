# Contributing

## Start here

A small documentation correction or a regression fixture for an existing rule
is a useful first contribution. Read the [public roadmap](docs/roadmap.md) for
priorities and starter tasks. For a behavior change, open an issue describing
an input, the current result, and the expected result before committing to a
large implementation. Rule proposals use the process below.

Use Go from [.go-version](.go-version), Git, jq, and a module proxy/cache that can
resolve the pinned dependencies. From the repository root:

```sh
./scripts/test-contributor fast
```

The entry point works from any directory and never installs tools. All tiers
stop on the first failure. Missing tools fail with a diagnostic; selecting a
smaller tier is an explicit choice, not a passing full run. Network failures
and an unavailable engine also fail. Ordinary Go tests may skip platform-only
cases; read their output and the CI matrix before claiming platform coverage.

| Tier | Command | Coverage and prerequisites |
| --- | --- | --- |
| Fast/unit | `./scripts/test-contributor fast` | Root build/tests (including documentation links and generated-copy freshness), lint and community module tests, example rule module (including its race tests). Go, Git and jq; community builder tests compile real linters and may download modules. |
| Native integration | `./scripts/test-contributor native` | Root, lint, community race tests, example module, and every integration test classified native by `scripts/test-integration`. No container; pinned helper downloads may need network access. |
| Container integration | `./scripts/test-contributor integration` | Native tier, SDK generation, Dagger integration and runner unit tests, `./verify pre-merge`, consumer and shared-check regressions. Pinned Dagger CLI and a working Docker/Colima or remote Dagger engine. |
| Full local CI | `./scripts/test-contributor full-ci` | Container tier plus formatting, Python/shell lint, SDK lock/security, release pins, workflow/ruleset consistency, tool/check fixtures, language contracts, and workflow-required fuzz smoke. Also needs ShellCheck, Ruff, uv, jq, Python, Rust/cargo and fetched release tags. |

Install the reviewed fixture tools (uv, Python and Rust) with
`./scripts/install-fixture-tools`; follow [language fixtures](docs/language-fixtures.md)
for their environment. Install ShellCheck through your system package manager and Ruff at the version
named in `.github/workflows/verify.yml`.
Install Dagger with `./scripts/install-dagger` and add its destination to PATH.
[Development](docs/maintainers/development.md#tools) covers the engine and SDK.
`full-ci` checks tool availability; CI's workflow pins remain authoritative for
versions. Fetch release tags before the full tier if using a shallow clone:

```sh
git fetch --no-tags --depth=1 origin '+refs/tags/v*:refs/tags/v*'
```

The full local tier approximates a **ready PR**, not every GitHub job. CI still
must exercise the composite GitHub Action's passing/failing cases, Linux archive
packaging and extracted consumer smoke, macOS root tests, and live ruleset
comparison (`scripts/test-rulesets --live`, requiring GitHub access). Live GitHub security audits, artifact
uploads, attestations and dependency review also run on GitHub. Advisory
semantic review needs an external paid service; mutation and the scheduled
fresh `./verify main` audit have separate event policies. See the
[CI event mapping](docs/maintainers/ci.md#jobs) and
[release checks](docs/maintainers/releases.md). A local full pass does not
replace required statuses. Draft PRs omit some ready-PR checks.

Fuzz smoke is included when `.github/workflows/verify.yml` invokes
`scripts/test-fuzz`; it is not a CI step at revisions predating that script.
A workflow-required but missing script fails, rather than silently skipping.

## Work on the relevant module

Keep changes near their tests, then run the appropriate tier above. For a
focused iteration, the commands below are useful; they do not replace CI.

| Area | Focused command / maintenance |
| --- | --- |
| Root CLI/verifier and docs | `GOTOOLCHAIN=local go test ./...` (relative links and anchors are checked here). |
| Shared tool parsers | After changing `internal/checktool`, run `GOTOOLCHAIN=local go generate ./internal/checktool`; commit the generated `runner/internal/checktool` copy. |
| Core lint | `(cd runner/lint && GOTOOLCHAIN=local go test ./...)`. After changing the failure guard or generated-file adapter, run `(cd runner/lint && GOTOOLCHAIN=local go generate ./internal/copygen)` and commit the community copies. |
| Community runtime/builder | `(cd runner/community && GOTOOLCHAIN=local go test ./...)`; builder tests need module downloads. |
| Example rule module | `./scripts/test-example-rules` tests it and builds it with the community builder. Preserve its documented release pins. |
| Dagger runner / SDK | Run `dagger develop --compat=skip` from the root, then `(cd runner && GOTOOLCHAIN=local dagger run go test -count=1 ./...)`. Generated SDK files are ignored; do not commit them. Changes to the adapter/dependencies also need `./scripts/test-sdk-security`, which regenerates twice and scans the resulting runtime. |

`GOTOOLCHAIN=local` uses the installed toolchain; the entry point checks it
against `.go-version`. The source launcher `./verify` provisions the pinned
toolchain instead. Run `./verify go-lint` for Go changes after SDK generation;
the container/full tiers include it through `pre-merge`.

## Optional local hooks

`lefthook.yml` describes a `pre-commit` that runs `gofmt` over staged Go files
and `go build ./...`, and a `pre-push` that runs `go test ./...`. Nothing
installs them for you:

```sh
brew install lefthook
lefthook install     # lefthook uninstall to stop
```

They are a convenience, not a gate: CI runs the same checks either way.

## Code style

Follow the conventions in [AGENTS.md](AGENTS.md) (spacing, struct literals, typed choices for finite values). Run the shared [Go lint rules](docs/rules.md) (`./verify go-lint`) when changing Go code.

## Proposing a new rule

Open a [new rule issue](https://github.com/wangjohn/levenshtein/issues/new?template=new_rule.yml) before writing code. A rule turns on for every repository that bumps its pin, so each one has to earn its place, and [rule selection](docs/rule-selection.md) records why every rule is on or off.

The bar a proposal has to meet:

- **Evidence that it fires on real code.** A rule is on because it was measured to find bugs in Levenshtein, in the fixtures that stand in for a consumer, or in the [open-source codebases measured](docs/rule-selection.md#measured-on-other-codebases), with each finding judged a bug, taste, or a false alarm. The exception is a [known bug pattern](docs/rules.md#known-bug-patterns): a pattern that is a bug rather than a matter of style, whose false alarms are rare, and whose fix is local.
- **Not taste.** Rules that enforce naming, comment wording, or one of two equally clear spellings stay off. A rule whose threshold is a judgment call, such as a complexity limit, can ship [off by default](docs/rules.md#opt-in-complexity-gocognit) for repositories to opt in to.
- **An existing analyzer first.** Prefer an established `go/analysis` analyzer or go-critic checker over a new house rule. [Considered and off](docs/rule-selection.md#considered-and-off) lists the ones already measured and left out, with the reason.
- **Rules Levenshtein will not ship** can still run: publish them as a [community rule module](docs/community-rules.md).

A pull request that adds a rule includes:

- **Fixtures.** Code the rule must flag and similar code it must not. House rules keep theirs in `runner/lint/policy/testdata`. An upstream analyzer gets a case in `runner/testdata/bad` and its code in `expectedBadCodes` in `runner/main.go`, so `self-test` fails if it stops reporting; `runner/testdata/good` must still pass.
- **A [rules](docs/rules.md) entry** saying what the rule reports, why it is on, and any setting that differs from upstream. A go-critic checker also updates the list `TestCriticSelection` pins.
- **A changelog entry** saying that consumers may see new findings when they bump their pin.

## Pull requests

- Keep PRs small and focused on one change.
- Add or update tests for any behavior change.
- Keep docs in sync with the code they describe; update the relevant file under `docs/` in the same PR. `go test ./...` fails on a relative link or `#anchor` that does not resolve.
- Add an entry under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md) for any change a consumer can see: a new or changed check, rule, flag, configuration field, action input, or output, and any fix that changes a verdict.

## Maintainers

Levenshtein is maintained by [@wangjohn](https://github.com/wangjohn). `.github/CODEOWNERS` assigns every path to that account, so it is asked to review every pull request.
