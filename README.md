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
| Staticcheck | **Close before error check:** deferring `f.Close()` before checking whether `os.Open` succeeded |
| Bug-finding analyzers | **Unchecked error:** `file.Write(data)` |
| Modernize | **Manual map copy:** a loop replaceable with `maps.Copy` |
| Levenshtein's own rules | **Untyped status:** comparing a string to `"done"` and `"failed"` |
| `go vet` | **Format mismatch:** `fmt.Printf("%d", "hello")` |
| Module checks | **Missing dependency** in `go.mod` |
| Go vulnerabilities (`main` only) | **Vulnerable function** called by your code |

### Optional checks

Add these checks to your repo's [configuration](docs/configuration.md).

| Check | Example finding |
| --- | --- |
| Race detector (`go-test`) | **Concurrent writes** to a shared map |
| GitHub Actions lint (`workflow-lint`) | **Unknown property** in a workflow expression |
| Workflow security (`workflow-security`) | **Shell injection** through a PR title |
| ShellCheck (`shell-lint`) | **Unchecked directory change:** `cd "$dir"` without handling failure |
| Secret scanning (`secrets`) | **Committed API key** |
| Other dependency vulnerabilities (`deps-vuln`) | **Vulnerable package** in `package-lock.json` |
| Import boundaries (`go-imports`) | **Forbidden import:** domain → database |
| Generated files (`go-generate`) | **Stale generated code** |
| API compatibility (`go-apidiff`) | **Removed exported function** |
| Mutation testing (`go-mutation`) | **Missed boundary bug:** `>` changed to `>=` |

The **[lint and CI/CD rules index](docs/checks.md)** links to all the rules,
checks, and CI policies, with explanations of why each rule is enabled or
left out. To understand a Go lint warning, start with
[Go lint rules](docs/rules.md).

## Use it in your repo

After cloning Levenshtein as shown in the [Quickstart](#quickstart), choose
which checks to run by passing a run name. These commands check the code in
`./myapp`; the names do not switch Git branches:

```sh
./levenshtein/verify pre-merge --source ./myapp --format text
./levenshtein/verify main --source ./myapp --format text
./levenshtein/verify go-lint --source ./myapp --format text
```

Tell your coding agent to run the verification command and fix any failures
before handing back a change. The [coding-agent setup guide](docs/agents.md)
has instructions you can copy into `AGENTS.md` or `CLAUDE.md`, plus hooks
that run checks automatically.

Add a [`levenshtein.json`](docs/configuration.md) if your repo has multiple
Go modules, or you want extra checks or your own commands. If there's already
a lot to fix, a [baseline](docs/configuration.md#baseline) records existing
lint warnings so you can start by blocking new ones. You can also add rules
published as Go modules through [community rules](docs/community-rules.md).

In GitHub Actions, use the shared action:

```yaml
- uses: wangjohn/levenshtein@3d47ab4c589fdf3a30107b6dd3f0816c1f346c85 # v0.2.0
```

The action chooses a run based on what triggered the workflow, such as a
push, pull request, or schedule, and shows warnings on the affected lines.
The [CI setup guide](docs/consumer-ci.md) has a complete workflow and examples
for other providers. Use a specific release or commit so you control when
your repo picks up rule changes.

We use these checks here too, alongside tests of example repos and release packages.
The [rules index](docs/checks.md#this-repositorys-ci-and-release-policies)
links to our CI and release policies and the files that enforce them.

## Quickstart

To try the Go lint rules in a module, run this from its root. You need Go 1.21
or later; it downloads the Go version the linter needs:

```sh
go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@latest ./...
```

That runs the linter alone. For the default checks described above, you need
Go and Docker Desktop, Colima, or another Docker-compatible runtime. On macOS
or Linux:

```sh
git clone --depth 1 --branch v0.2.0 https://github.com/wangjohn/levenshtein
./levenshtein/verify --source ./myapp --format text
```

A single Go module needs no config. The first run downloads and builds the
tools; later runs reuse saved results where possible if the code and tools
haven't changed. `verify` reports problems without changing your files. It
exits `0` when checks pass, `1` when a check fails, and `2` for a command or
configuration error.

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
