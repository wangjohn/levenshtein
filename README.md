# Levenshtein

[![CI](https://github.com/wangjohn/levenshtein/actions/workflows/verify.yml/badge.svg?branch=main)](https://github.com/wangjohn/levenshtein/actions/workflows/verify.yml)
[![Latest release](https://img.shields.io/github/v/release/wangjohn/levenshtein)](https://github.com/wangjohn/levenshtein/releases/latest)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)

**A curated set of Go lint rules and verification checks, easy to hook up to any
Go repo.**

Coding agents are writing more and more of our code, and I want as much of
that code as possible to be verifiable. Good lint rules and automated checks
give agents feedback they can act on while they work, and give us more than
the agent's word that a change is ready.

I built Levenshtein while setting up a bunch of new Go repos. I wanted one
place to keep a high-quality set of rules I could rely on for all of my repos,
without researching analyzers and copying lint configs every time. Each repo
pins a Levenshtein revision and runs `./verify`, so that all my repos use the 
same rules and tool versions (and updates are easy to roll out everywhere).

You can easily add your own lint rules or commands alongside the built-in
checks in your repo’s configuration.

## What it checks

Levenshtein combines Staticcheck, established Go analyzers, and a few rules
of its own. The rules catch bugs and encourage readable code; most naming
and comment-style preferences are left out.

### Default checks

`./verify` and `pre-merge` run these checks. Go vulnerability scanning runs
only in `main`, which also reruns checks without using cached results.

| Check | Example finding |
| --- | --- |
| [Staticcheck](docs/rules.md#the-staticcheck-selection) | Close before error check: deferring `f.Close()` before checking whether `os.Open` succeeded |
| [Bug-finding analyzers](docs/rules.md#rules-on-by-default) | Unchecked error: calling `file.Write(data)` without checking whether the write failed |
| [Modernize](docs/rules.md#the-modernize-selection) | Manual map copy: copying entries in a loop when `maps.Copy` does the same job |
| [Levenshtein's rules](docs/rules.md#typed-choices-lv1001) | Untyped status: comparing a plain string to `"done"` and `"failed"` instead of typed constants |
| [`go vet`](docs/check-kinds.md) (`go-vet`) | Format mismatch: passing `"hello"` to `fmt.Printf("%d", ...)`, which expects an integer |
| [Module checks](docs/check-kinds-guide.md#module-manifests) (`go-mod`) | Missing dependency: importing a package whose module is missing from `go.mod` |
| [Go vulnerabilities](docs/check-kinds.md) (`go-vuln`, `main` only) | Vulnerable function: your code can reach a dependency function with a known security flaw |

### Optional checks

Add these checks to your repo's [configuration](docs/configuration.md).

| Check | Example finding |
| --- | --- |
| [Race detector](docs/check-kinds-guide.md#tests) (`go-test`) | Concurrent writes: two goroutines updating a shared map without synchronization |
| [HTTP resources](docs/check-kinds.md) (`go-http`) | Unclosed response body: returning from an HTTP request without closing `resp.Body` |
| [SQL resources](docs/check-kinds.md) (`go-sql`) | Unclosed query results: reading database rows without closing them afterward |
| [GitHub Actions lint](docs/check-kinds.md) (`workflow-lint`) | Unknown property: a workflow expression referencing a property that doesn't exist |
| [Workflow security](docs/check-kinds-guide.md#workflow-security) (`workflow-security`) | Shell injection: inserting a PR title directly into a workflow's shell command |
| [ShellCheck](docs/check-kinds-guide.md#shell-scripts) (`shell-lint`) | Unchecked directory change: running `cd "$dir"` without stopping or handling failure |
| [Secret scanning](docs/check-kinds-guide.md#secrets) (`secrets`) | Committed API key: a credential left in a tracked configuration file |
| [Other dependency vulnerabilities](docs/check-kinds-guide.md#dependency-vulnerabilities) (`deps-vuln`) | Vulnerable package: a version with a known security flaw pinned in `package-lock.json` |
| [Import boundaries](docs/check-kinds-guide.md#import-boundaries) (`go-imports`) | Forbidden import: a domain package importing a database package against your layering rules |
| [Generated files](docs/check-kinds-guide.md#generated-code) (`go-generate`) | Stale generated code: a committed file that changes when `go generate` runs |
| [API compatibility](docs/check-kinds-guide.md#api-compatibility) (`go-apidiff`) | Removed exported function: a public API change that breaks existing callers |
| [Mutation testing](docs/mutation.md) (`go-mutation`) | Missed boundary bug: changing `>` to `>=` without any test failing |
| [Semantic lint](docs/semantic-lint.md) (`semantic-lint`) | Vague error message: an added error gives the caller no clue how to fix the problem; advisory review through Jev |
| [Custom commands](docs/configuration.md#native-commands) (`command`) | Failed integration test: your repo's test script exits with an error |

`go-http` and `go-sql` run resource rules already included in `go-lint`,
for repos that want those checks alone. [`self-test`](docs/check-kinds.md)
tests Levenshtein's own fixtures when developing the shared checks.

The **[lint and CI/CD rules index](docs/checks.md)** links to all the rules,
checks, and CI policies, with explanations of why each rule is enabled or
left out. To understand a Go lint warning, start with
[Go lint rules](docs/rules.md).

## Quickstart

To try the Go lint rules, run this from your module's root with Go 1.21 or later:

```sh
go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@latest ./...
```

For the default checks, use Go and a Docker-compatible runtime on macOS or Linux:

```sh
git clone --depth 1 --branch v0.2.0 https://github.com/wangjohn/levenshtein
./levenshtein/verify --source ./myapp --format text
```

A single Go module needs no config. The first run downloads and builds the
tools; `verify` reports problems without changing your files. See
[setup](docs/setup.md) for prerequisites and named runs.

- [CI setup](docs/consumer-ci.md): GitHub Actions and other providers.
- [Coding agents](docs/agents.md): instructions and hooks for running checks while an agent works.
- [Configuration](docs/configuration.md): multiple modules, extra checks, and custom commands.
- [Baselines](docs/configuration.md#baseline) and [community rules](docs/community-rules.md): adopt checks gradually or add your own analyzers.

## Documentation

[Setup](docs/setup.md) ·
[Rules index](docs/checks.md) ·
[Configuration](docs/configuration.md) ·
[CLI reference](docs/reference/cli.md) ·
[Coding agents](docs/agents.md) ·
[Troubleshooting](docs/troubleshooting.md) ·
[All docs](docs/README.md)

See the [changelog](CHANGELOG.md) and [versioning policy](docs/versioning.md) before
updating the version your repo uses.

If you're happy with a single repo's `golangci-lint` setup,
you may not need this; the [FAQ](docs/faq.md#levenshtein-or-golangci-lint)
compares the two.

## Contributing and security

Maintained by [John Wang (@wangjohn)](https://github.com/wangjohn).
For questions or bugs, [open an issue](https://github.com/wangjohn/levenshtein/issues).

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
[SECURITY.md](SECURITY.md) describes. Released under the
[Apache License 2.0](LICENSE); see [NOTICE](NOTICE).
