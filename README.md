# Levenshtein

A comprehensive, carefully chosen set of Go lint rules for all your repos.

Coding agents write a lot of code quickly, and they fix whatever their tools point out. That makes lint rules more important than ever. Good rules will catch unchecked errors, leaked resources, and sloppy code before anyone reviews the change, and the agent fixes those problems on its own. When much of your code isn't written by hand, lint rules are the most reliable way to keep a repo clean and well written.

Levenshtein turns on nearly all of Staticcheck, more than twenty other analyzers, and a handful of its own rules. Each rule is there because it catches bugs or makes code clearer, and [docs/checks.md](docs/checks.md) gives the reason for every one. Rules that only enforce someone's taste in naming or comments are off, so a finding is usually worth fixing.

Every repo uses the same rules. Each one pins a revision of Levenshtein and runs `./verify`, which runs the checks in a container with pinned versions of Go and every tool. A result doesn't depend on whose laptop or which CI provider ran it. When you improve a rule, you do it once, and each repo picks it up when it bumps its pin.

## Example

Given this file:

```go
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
- **Levenshtein's own rules**: typed constants for enum-like strings, building structs in one literal, and three formatting rules.
- **Other tools**: `go vet`, `go mod tidy -diff` and `go mod verify`, `govulncheck`, and `actionlint` for GitHub Actions. `zizmor`'s offline security audits of workflows and composite actions are available as the opt-in `workflow-security` check, and `go test -race ./...` as the opt-in `go-test` check. Beyond Go, ShellCheck lints shell scripts as the opt-in `shell-lint` check, pinned by version and checksum.
- **Module-wide checks** (opt-in): `go-imports` enforces the layering rules a repository declares, reporting each forbidden import where it is written; `go-generate` runs `go generate ./...` in a scratch copy and reports every generated file that is out of date, with the diff, and `go-apidiff` fails a branch that breaks a library module's exported API.

To silence a finding, use Staticcheck's usual comment: `//lint:ignore CODE reason`.

Rules Levenshtein doesn't ship can come from [community rule modules](docs/community-rules.md): ordinary Go modules of `go/analysis` analyzers that a repo pins in `levenshtein.json`. They run in their own process beside the shipped rules and report into the same results.
To turn the rules on in a repository that already has findings, name a [baseline](docs/configuration.md#baseline) file in `levenshtein.json` and record them with `verify main --write-baseline`. Recorded findings are reported but don't fail, new ones do, and fixing a recorded one means deleting its entry, so the file only shrinks.

## Quick start

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

`--format github` writes GitHub Actions annotations and `--format sarif` a file for GitHub code scanning; `--render report.json` turns a saved JSON report into any of them. Findings with a mechanical fix, such as `gofmt -w`, carry a hint. Levenshtein never changes your files. See [output formats](docs/configuration.md#output-formats).

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

Levenshtein runs inside your existing CI. On GitHub Actions it is one step, `uses: wangjohn/levenshtein@v0.1.0`, which runs `branch` on pushes, `pre-merge` on pull requests, and `main` on a nightly schedule, and annotates failing findings on the pull request. On other providers, your CI job checks out your repo and Levenshtein side by side, then runs `verify`. [docs/consumer-ci.md](docs/consumer-ci.md) has both.

Pin Levenshtein to a commit SHA or a release tag, not a branch. That way rule changes reach your repo only when you choose to update.

## Why a separate repo?

Lint config usually gets copied from repo to repo, and then the copies drift apart. One repo is on an old Staticcheck. Another turned off a rule years ago. A third never got the rule that would have caught last week's bug.

With Levenshtein, the rules and tool versions live in one place. Improve a rule once and every repo can pick it up. Developers, CI, and coding agents all get the same results.

If you have a single Go repo and you're happy with your `golangci-lint` setup, you probably don't need this.

## Status

Levenshtein is new and is being tried out on a few Go repos. The config format is versioned, and changes to it will be listed in the [changelog](CHANGELOG.md). Only Go has built-in checks. Other languages can use `command` checks.

## Documentation

- [Setup](docs/setup.md): install and run locally
- [Checks](docs/checks.md): every rule and why it's on or off
- [Community lint rules](docs/community-rules.md): publish rules as a Go module, or run someone else's
- [Configuration](docs/configuration.md): targets, runs, commands, and caching
- [Using it in CI](docs/consumer-ci.md): GitHub Actions and other providers
- [Coding agents](docs/agents.md): templates for Claude Code hooks, `AGENTS.md`, a workflow, and a starter config
- [Releases](docs/releases.md): prebuilt binaries
- [Semantic lint](docs/semantic-lint.md): an optional review by a language model
- [Mutation testing](docs/mutation.md): an optional check that your tests catch deliberate bugs in changed code
- [Architecture](docs/architecture.md): how `verify` works

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To report a security issue, see [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
