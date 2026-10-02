# Developing Levenshtein

This page is for working on Levenshtein itself: the pinned dependencies, the local development loop, and how the CLI drives Dagger. [CONTRIBUTING.md](../../CONTRIBUTING.md) has the short version of the build and test commands, and [Levenshtein's own CI](ci.md) explains what the workflows run.

## Tools

Use Go 1.27.1, the version in `.go-version`. To develop the shared Dagger module or use `dagger check` directly, install the pinned Dagger CLI with the checked-in archive checksums:

```sh
./scripts/install-dagger
export PATH="$HOME/.local/bin:$PATH"
dagger version
```

The installer supports macOS Intel/Apple Silicon and Linux amd64/arm64. Checks bound to a Dagger environment also need a Docker-compatible container runtime; [setup](../setup.md#prerequisites) shows one way to get one.

Generate the ignored SDK files once before verifying Levenshtein itself:

```sh
dagger develop --compat=skip
```

## Pinned dependencies

Stable versions checked on September 15, 2026:

| Dependency | Version | Pin |
| --- | --- | --- |
| Go for lint and local development | 1.27.1 | `.go-version`, root and `runner` `go.mod`, fixture modules, `runner/toolchain.json` |
| Go container | 1.27.1 on Debian Trixie | Tag and immutable image digest in `runner/toolchain.json` |
| Dagger CLI / engine / SDK | 0.21.9 | `.dagger-version`, `dagger.json`, root `go.mod`, generated module dependencies |
| Staticcheck | 2026.2.1 (`honnef.co/go/tools` v0.8.1) | `runner/toolchain.json` |
| actionlint, apidiff, gitleaks, govulncheck, gremlins | Per tool | One module each under `runner/tools`, whose `go.mod` is the only pin; Dependabot proposes each tool's updates in a pull request of its own |
| Actions checkout / setup-go / cache | 7.0.1 / 7.0.0 / 6.1.0 | Full commit hashes in the workflows and composite actions |

The pinned Go version builds the CLI (the launcher provisions it when the host Go differs) and is what runner development and unit tests expect. The actual lint runs on Linux with default build tags, using the pinned container toolchain with automatic Go toolchain switching disabled. A temporary SDK adapter fixes Dagger 0.21.9’s forced logging dependency overrides for both generation and execution; see [dependency security](../dependencies.md#dagger-wrapper-dependency-security). The wrapper’s Go language version must stay at or below the Go version of the codegen container (`goImage`), which Dagger’s module generator refuses to exceed; it does not restrict the Go version of repositories being checked. `go.sum` records checksums. Upgrade pins together and validate the fixtures before adoption.

## Develop the shared checks

Use `./scripts/test-contributor fast` for the initial unit-test loop, `native`
for race and host integration checks, `integration` for container regressions,
and `full-ci` for the local ready-PR checks. [Contributor test tiers](../../CONTRIBUTING.md#start-here)
list prerequisites and the hosted checks that remain. The commands below are
focused development tools; the entry point reuses them.

Use Go 1.27.1 and the pinned Dagger CLI:

```sh
dagger develop --compat=skip
GOTOOLCHAIN=local go test -race ./...
(cd runner && GOTOOLCHAIN=local dagger run go test ./...)
./verify pre-merge
./scripts/test-consumers
```

In this repository `./verify branch` and the per-kind runs of its static checks run natively and need no container runtime, only Go 1.27.1 and the generated SDK from `dagger develop`, since the `runner` module compiles against it. `pre-merge` adds `self-test`, which runs in Dagger. `./verify mutation` runs mutation testing of the branch's changed Go files in Dagger, outside `pre-merge`. `./verify branch-dagger` runs the same static checks in Dagger, and `./verify main` is the full hermetic audit. `levenshtein.json` names every run.

The generated Go SDK needs a Dagger session, including during unit tests. The deliberately broken Go module lives under `runner/testdata`, outside ordinary test discovery. The self-test requires good code, vendored dependencies, and embedded templates to pass, bad code to emit each intended rule, broken/empty modules to fail verification, the pinned zizmor to pass `workflow-secure` and report `workflow-insecure`'s template injection, `go test -race` to pass `test-pass`, report `test-fail` and `test-race` as findings, and refuse `test-build` as an error, the pinned ShellCheck to pass `shell-good` and report each of `shell-bad`'s diagnostics, gitleaks to pass `secrets-clean` and report `secrets-leaky`'s made-up key without its value, and osv-scanner to pass `deps-clean` and report `deps-vulnerable`'s two npm packages but not its Go module. A compiler failure cannot substitute for an expected lint finding.

`scripts/test-consumers` checks module and workspace vendoring through the launcher. It also adds synthetic private env files next to root and nested `.env.example` templates; each embed must match exactly one file, proving the templates survive filtering and the private files do not. A final case verifies undeclared dependencies still fail without vendoring.

### Initial measurements (original Dagger-only launcher)

On an Intel Mac with a two-CPU, 4 GiB Colima VM, the first `pre-merge` run after SDK setup took **136 seconds**, including the Go image download and Staticcheck compilation. A repeat took **2.5 seconds**; a fresh `main` audit took **14 seconds**. A single fixture check took **1.8 seconds**. These are small-pilot measurements, not guarantees for application repos; initial CLI/VM installation and SDK setup are excluded.

## Dagger integration

The wrapper uses the official Go SDK and one engine session per run. It loads the pinned shared module once and passes each consumer directory, module path, and freshness token as function arguments. Dagger owns container execution, dependency downloads, compilation caches, and execution caching. A fresh audit reruns analysis while retaining download and compiler caches.

`GoLint` and `SelfTest` are also native Dagger checks: `dagger check -l` lists them and `dagger check` runs them against this checkout. The wrapper calls the same functions with explicit consumer inputs; named-run selection lives only in the standalone CLI. The former Dagger `verify` and `check` functions have been removed. Use `./verify` for Levenshtein runs.

Shared module identity must remain separate from consumer inputs. Loading a synthetic module directory containing consumer files would change Dagger's cache namespace on every source edit or fresh audit. See [dependency choices](../dependencies.md).
