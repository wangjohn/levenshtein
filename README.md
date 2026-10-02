# Levenshtein

Consistent Go verification for repositories, developers, CI, and coding agents.

Levenshtein gives several Go repositories one reviewed set of lint rules and tool versions, run through `./verify`. Pin a release, choose the checks your repository needs, and update the pin when you are ready to adopt shared changes. Coding agents can use the same command and actionable findings as the people reviewing their code.

The defaults combine bug checks with deliberate readability preferences, including typed choices and constructing structs in one literal. They are a shared house policy; a style finding does not mean the code has a runtime bug. Repositories can [adjust rule selection](docs/configuration.md#lint-selection), suppress a finding with a reason, or [baseline existing findings](docs/configuration.md#baseline) while fixing them.

## Example

Given this `config.go`:

```go
package config

import (
	"io"
	"os"
)

func ReadConfig(path string) ([]byte, error) {
	f, err := os.Open(path)
	defer f.Close()
	if err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func IsFinished(status string) bool {
	return status == "done" || status == "failed"
}
```

Levenshtein reports:

```text
config.go:10:2: should check error returned from os.Open() before deferring f.Close() (SA5001)
config.go:10:15: unchecked error (errcheck)
config.go:18:9: string choice with multiple alternatives needs a defined string type and typed constants (LV1001)
```

## Try it in one command

To see what the rules find in a Go module, run this from its root. It needs `go` 1.21 or later and access to a module proxy, so Go can download the pinned toolchain and dependencies, with no clone, container, or config:

```sh
go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@v0.2.0 ./...
```

It prints one `file:line:col: message (CODE)` line per finding and exits with `1` when there are any. This runs the `go-lint` rules alone: `go vet`, `go-mod`, `govulncheck`, the [baseline](docs/configuration.md#baseline), [community rules](docs/community-rules.md), and caching come with `verify` below. `@v0.2.0` selects the existing `runner/lint/v0.2.0` release tag; update the version deliberately when adopting a newer release. See [running the linter directly](docs/rules.md#running-the-linter-directly).

## What it checks

- **Staticcheck**: everything except six style rules about naming and doc comments.
- **Bug-finding analyzers**: `errcheck`, `exhaustive`, `bodyclose`, `nilness`, `errorlint`, `contextcheck`, go-critic's likely-bug checks, structured-logging mistakes, and others.
- **Modernize rules**: five rules for newer Go features, each fixable with `go fix`.
- **Levenshtein's own rules**: typed constants for enum-like strings, building structs in one literal, three formatting rules, and tests that cannot fail (LV1006).
- **Other tools, on by default**: `go vet`, `go mod tidy -diff` and `go mod verify`, and, in the `main` run, `govulncheck`.
- **Other tools, opt-in**: `actionlint` for GitHub Actions (`workflow-lint`), `zizmor`'s offline security audits of workflows and composite actions (`workflow-security`), and `go test -race ./...` (`go-test`). Beyond Go, ShellCheck lints shell scripts (`shell-lint`), gitleaks scans files for committed secrets (`secrets`), and osv-scanner checks npm, Python, Rust, Ruby, and other lockfiles for known vulnerabilities (`deps-vuln`), each pinned by version and checksum.
- **Module-wide checks** (opt-in): `go-imports` enforces the layering rules a repository declares, reporting each forbidden import where it is written; `go-generate` runs `go generate ./...` in a scratch copy and reports every generated file that is out of date, with the diff; `go-apidiff` fails a branch that breaks a library module's exported API; and `go-mutation` fails when a test does not catch a deliberate bug in changed code.

[Check kinds](docs/check-kinds.md) lists every check and which runs include it by default.

To silence a finding, use Staticcheck's usual comment: `//lint:ignore CODE reason`.

Rules Levenshtein doesn't ship can come from [community rule modules](docs/community-rules.md): ordinary Go modules of `go/analysis` analyzers that a repo pins in `levenshtein.json`. They run in their own process beside the shipped rules and report into the same results.
To turn the rules on in a repository that already has findings, name a [baseline](docs/configuration.md#baseline) file in `levenshtein.json` and record them with `verify main --write-baseline`. Recorded findings are reported but don't fail, new ones do, and fixing a recorded one means deleting its entry, so the file only shrinks.

## Quick start

The source launcher needs Go 1.21+ on `PATH` and access to a module proxy for the pinned toolchain and dependencies. The default checks also need a running Docker-compatible runtime, such as Docker or Colima. Levenshtein runs on Linux and macOS. [Setup](docs/setup.md#prerequisites) covers native checks and download restrictions; a [prebuilt release archive](docs/releases.md#running-an-archive) avoids building the CLI on the host.

```sh
git clone --branch v0.2.0 --depth 1 https://github.com/wangjohn/levenshtein.git
./levenshtein/verify --source /absolute/path/to/myapp
```

The first run downloads and builds the tools. Cacheable checks that passed can reuse their verdict while their declared inputs, configuration, and implementation stay unchanged. Use a run with `rerun_checks` when you need fresh execution, such as a nightly verification.

`verify` prints a JSON report. It exits with `0` if everything passes, `1` if a check fails, and `2` if the command or config is wrong. To see just the findings, one per line:

```sh
./levenshtein/verify --source /absolute/path/to/myapp --format text
```

`--format` also writes GitHub Actions annotations or a SARIF file for code scanning. Findings with a mechanical fix, such as `gofmt -w`, carry a hint; Levenshtein never changes your files. The [CLI reference](docs/reference/cli.md) lists every flag and exit code.

Advisory findings, from [community rules](docs/community-rules.md) a repo marks advisory, are reported without failing the check.

## Runs

A run is a named group of checks. If your repo has one Go module at its root, you don't need any config:

```sh
./levenshtein/verify --source ./myapp            # branch (default): lint, vet, and module manifests
./levenshtein/verify pre-merge --source ./myapp  # lint, vet, and module manifests
./levenshtein/verify main --source ./myapp       # lint, vet, module manifests, and govulncheck
./levenshtein/verify go-lint --source ./myapp    # one check
```

For several modules or your own runs, add a `levenshtein.json` to your repo:

```json
{
  "version": 1,
  "targets": {
    "api": {"dir": "services/api", "inputs": ["services/api", "go.work", "go.work.sum"]}
  },
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "lint": {"kind": "go-lint", "target": "api", "environment": "go"},
    "vuln": {"kind": "go-vuln", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["lint"]},
    "main": {"checks": ["lint", "vuln"], "rerun_checks": true}
  }
}
```

- `inputs` lists the files a check can see. A change to any of them makes the check run again.
- `rerun_checks` makes a run skip the cache. Use it for nightly runs.
- A `command` check runs your own script, like your test suite, next to the lint checks.

See [docs/configuration.md](docs/configuration.md) for everything else.

## In CI

Levenshtein runs inside your existing CI. On GitHub Actions it is one step, `uses: wangjohn/levenshtein@3d47ab4c589fdf3a30107b6dd3f0816c1f346c85 # v0.2.0`, which runs `main` on a schedule, `branch` on pushes and draft pull requests, and `pre-merge` on every other event, such as a ready pull request or a merge queue, and annotates failing findings on the pull request. On other providers, your CI job checks out your repo and Levenshtein side by side, then runs `verify`. [docs/consumer-ci.md](docs/consumer-ci.md) has both.

Pin Levenshtein to a commit SHA or a release tag, not a branch. That way rule changes reach your repo only when you choose to update.

## Why a separate repo?

Lint config usually gets copied from repo to repo, and then the copies drift apart. One repo is on an old Staticcheck. Another turned off a rule years ago. A third never got the rule that would have caught last week's bug.

With Levenshtein, the rules and tool versions live in one place. Improve a rule once and each repository can adopt it by updating its pin. Repositories start from shared defaults and can configure their own selections and runs.

The default Dagger executor uses pinned Go and tool versions in a container, with checks reading the declared source inputs. This reduces differences between developer machines and CI; findings still depend on the selected checks, inputs, and configuration. [Native execution](docs/configuration.md#native-go-checks) uses the host's Go and environment, so provision the required Go version and prerequisites explicitly. Community rule modules execute in Dagger: released version 1 configurations skip them with a warning on native `go-lint`. **Upcoming:** configuration version 2 rejects that combination unless the check explicitly opts out with `lint.rule_modules=false`; version 1 keeps its existing behavior.

The name refers to Levenshtein edit distance as a metaphor for bringing code closer to a shared standard. The tool reports check results and findings; it does not calculate a numerical edit distance or an overall code-quality score.

Levenshtein fits teams maintaining several Go repositories that want shared defaults and deliberate updates. If you have a single Go repo and you're happy with your `golangci-lint` setup, keeping it may be simpler. [Levenshtein or golangci-lint?](docs/faq.md#levenshtein-or-golangci-lint) compares the two.

## Status

The examples pin v0.2.0; this checkout also contains unreleased changes listed in the [changelog](CHANGELOG.md). The config format is versioned; [versioning](docs/versioning.md) says what a release may change and what to expect when you bump your pin. The built-in lint rules are for Go. The other built-in checks cover GitHub Actions workflows, shell scripts, committed secrets, and non-Go dependency lockfiles. Anything else, such as another language's linter or test suite, runs as a `command` check. [Semantic lint](docs/semantic-lint.md) is a separate experimental, opt-in review through an external paid service; it is not part of the default Go checks.

## Documentation

[docs/README.md](docs/README.md) indexes every page. The ones most people need:

- [Setup](docs/setup.md): install and run locally
- [Go lint rules](docs/rules.md): every rule and why it's on, and [rule selection](docs/rule-selection.md) for the ones left off
- [Check kinds](docs/check-kinds.md): every check, its executors, and its default runs
- [Configuration](docs/configuration.md): targets, runs, commands, and caching, with every field in the [configuration reference](docs/reference/config.md)
- [CLI reference](docs/reference/cli.md): flags, output formats, and exit codes
- [Using it in CI](docs/consumer-ci.md): GitHub Actions and other providers
- [Troubleshooting](docs/troubleshooting.md): common errors and what to do about them
- [Community lint rules](docs/community-rules.md): publish rules as a Go module, or run someone else's
- [Coding agents](docs/agents.md): templates for Claude Code hooks, `AGENTS.md`, a workflow, and a starter config
- [Mutation testing](docs/mutation.md): an optional check that your tests catch deliberate bugs in changed code
- [Semantic lint](docs/semantic-lint.md) (experimental): an optional review by a language model, through an external paid service
- [Architecture](docs/architecture.md): how `verify` works

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security issue, see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). Copyright 2026 John Wang; see [NOTICE](NOTICE).
