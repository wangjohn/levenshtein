# Levenshtein

Shared, pinned Go lint and verification for many repos.

A comprehensive, carefully chosen set of Go lint rules, run by one `./verify` command that gives the same result locally, in CI, and for coding agents. The name comes from Levenshtein distance, the number of single-character edits between two strings. <!-- TODO(maintainer): confirm or replace this explanation of the name. -->

Coding agents write a lot of code quickly, and they fix whatever their tools point out. That makes lint rules more important than ever. Good rules will catch unchecked errors, leaked resources, and sloppy code before anyone reviews the change, and the agent fixes those problems on its own. When much of your code isn't written by hand, lint rules are the most reliable way to keep a repo clean and well written.

Levenshtein turns on nearly all of Staticcheck, nearly forty other analyzers, about twenty of go-critic's bug checks, and a handful of its own rules. Each rule is there because it catches bugs or makes code clearer, and [docs/rules.md](docs/rules.md) gives the reason for every one. Rules that only enforce someone's taste in naming or comments are off, so a finding is usually worth fixing.

Every repo uses the same rules. Each one pins a revision of Levenshtein and runs `./verify`, which runs the checks in a container with pinned versions of Go and every tool. A result doesn't depend on whose laptop or which CI provider ran it. When you improve a rule, you do it once, and each repo picks it up when it bumps its pin.

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
config.go:10:2: SA5001 should check error returned from os.Open() before deferring f.Close()
config.go:10:15: errcheck unchecked error
config.go:18:9: LV1001 string choice with multiple alternatives needs a defined string type and typed constants
```

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

## Try it in one command

To see what the rules find in a Go module, run this from its root. It needs only `go` 1.21 or later, which downloads the pinned toolchain itself, with no clone, container, or config:

```sh
go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@latest ./...
```

It prints one `file:line:col: message (CODE)` line per finding and exits with `1` when there are any. This runs the `go-lint` rules alone: `go vet`, `go-mod`, `govulncheck`, the [baseline](docs/configuration.md#baseline), [community rules](docs/community-rules.md), and caching come with `verify` below. `@latest` is the newest `runner/lint/vX.Y.Z` release tag, or the newest commit on `main` while there is none; to pin, name a tag or a commit instead. See [running the linter directly](docs/checks.md#running-the-linter-directly).

## Quick start

> [!NOTE]
> This README describes `main`, which is ahead of the latest release, v0.1.0. v0.1.0 has no `--format`, `--render`, or baseline flags, no `annotations` or `sarif` action inputs, no community rule modules, and ten check kinds rather than nineteen. If you pin v0.1.0, read [its README](https://github.com/wangjohn/levenshtein/tree/v0.1.0) instead. <!-- Delete this note when the next release ships. -->

You need `go` (any version) and Docker or another Docker-compatible runtime, such as Colima. Levenshtein runs on Linux and macOS.

```sh
git clone https://github.com/wangjohn/levenshtein
./levenshtein/verify --source ./myapp
```

The first run takes a few minutes while it downloads and builds the tools. After that, runs are fast, and checks that passed are skipped until their files change.

`verify` prints a JSON report. It exits with `0` if everything passes, `1` if a check fails, and `2` if the command or config is wrong. To see just the findings, one per line:

```sh
./levenshtein/verify --source ./myapp --format text
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

With Levenshtein, the rules and tool versions live in one place. Improve a rule once and every repo can pick it up. Developers, CI, and coding agents all get the same results.

If you have a single Go repo and you're happy with your `golangci-lint` setup, you probably don't need this. [Levenshtein or golangci-lint?](docs/faq.md#levenshtein-or-golangci-lint) compares the two.

## Status

Levenshtein is new and is being tried out on a few Go repos. The config format is versioned, and changes to it will be listed in the [changelog](CHANGELOG.md); [versioning](docs/versioning.md) says what a release may change and what to expect when you bump your pin. The built-in lint rules are for Go. The other built-in checks cover GitHub Actions workflows, shell scripts, committed secrets, and non-Go dependency lockfiles. Anything else, such as another language's linter or test suite, runs as a `command` check.

## Documentation

[docs/README.md](docs/README.md) indexes every page. The ones most people need:

- [Setup](docs/setup.md): install and run locally
- [Checks](docs/checks.md): every rule and why it's on or off
- [Check kinds](docs/check-kinds.md): every check, its executors, and its default runs
- [Configuration](docs/configuration.md): targets, runs, commands, and caching
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

[MIT](LICENSE)
