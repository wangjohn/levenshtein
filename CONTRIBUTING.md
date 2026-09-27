# Contributing

## Build and test locally

Root module:

```sh
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./...
```

The runner compiles a generated copy of `internal/checktool`, which both
executors use to start pinned tools and read their results. After changing
it, regenerate the copy; a root test fails until you do:

```sh
GOTOOLCHAIN=local go generate ./internal/checktool
```

Go lint policy analyzers:

```sh
(cd runner/lint && GOTOOLCHAIN=local go test ./...)
```

The community linter compiles generated copies of the core linter's failure
guard (`runner/lint/cmd/levenshtein-lint/guard.go` and its test) and
generated-file adapter (`runner/lint/policy/generated.go`). After changing
one, regenerate the copies; a `runner/lint` test fails until you do:

```sh
(cd runner/lint && GOTOOLCHAIN=local go generate ./internal/copygen)
```

Community linter runtime and builder. The builder tests compile real linters, so they need the module proxy:

```sh
(cd runner/community && GOTOOLCHAIN=local go test ./...)
./scripts/test-example-rules
```

The runner's Dagger module needs its generated SDK before its own tests run. Run `dagger develop` from the repository root, where `dagger.json` lives:

```sh
dagger develop --compat=skip
(cd runner && GOTOOLCHAIN=local dagger run go test ./...)
```

`GOTOOLCHAIN=local` keeps these commands from downloading another toolchain when the host version differs from `.go-version`, so they run on the Go you have. `./verify` does the opposite: it names the pinned toolchain, so any host `go` builds the CLI with the version in `.go-version` and fetches it once if needed.

Repo self-checks, matching what CI runs:

```sh
./verify pre-merge
```

`./verify branch` runs the static Go checks natively: it needs Go 1.27.1 and the generated SDK from `dagger develop`, but no container runtime. `./verify pre-merge` adds `self-test`, and `./verify main` and `./verify branch-dagger` run the checks in Dagger; those need a Docker-compatible container runtime (Docker Desktop or Colima) and the pinned Dagger CLI. Install the CLI with:

```sh
./scripts/install-dagger
export PATH="$HOME/.local/bin:$PATH"
```

See ["Develop the shared checks"](docs/maintainers/development.md#develop-the-shared-checks) for the full local development recipe, including the broader `dagger develop` / `go test -race` / `./scripts/test-consumers` sequence CI runs.

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
