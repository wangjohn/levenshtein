# Setup and usage

Levenshtein's shared checks run pinned lint, vet, workflow, and vulnerability tools, either in Dagger containers or natively on the host. The runner accepts a source checkout and a named run; your existing CI supplies workers and decides when to invoke it. See [use from an application repo](consumer-ci.md) for local and CI examples, and [architecture](architecture.md) for how the runner works.

## Prerequisites

The source launcher needs Go **1.21 or later** on `PATH`, which supports automatic toolchain switching: it sets `GOTOOLCHAIN` to the version in `.go-version` (**1.27.1**), so Go downloads and caches that toolchain itself when the host differs. That download needs a reachable module proxy. With `GOPROXY=off`, install Go 1.27.1 on `PATH` or provision its toolchain cache first; the CLI's module dependencies must also already be cached. The launcher sets `GOTOOLCHAIN` for its own build even if the caller sets it to `local`. This does not select the Go executable used by native checks.

What else you need depends on the executor of the environments your checks use:

- **Dagger**, the default for a repository without `levenshtein.json`: a Docker-compatible container runtime. The Go SDK then downloads and checksum-verifies Dagger **0.21.9** automatically, and the checks run on the Go and tools pinned in the container.
- **Native**: the Go version in `.go-version` on `PATH`. The [shared Go checks on a native environment](configuration.md#native-go-checks) use the host's `go` as it is, without switching toolchains, and Levenshtein builds or downloads their other pinned tools itself. `go-test` also needs a C compiler, and native `command` checks need whatever their configuration declares. Planning and native checks do not start Dagger and need no container runtime.

On macOS, one option for the container runtime is Colima:

```sh
brew install colima docker
colima start levenshtein --runtime docker --vm-type vz --cpu 2 --memory 4 --disk 60
```

Docker Desktop or an existing Docker engine also works. Dagger downloads its pinned engine and builds the module on the first invocation; allow extra time for the initial run.

## Choose an installation

- **Direct linter trial:** from a Go module, run `go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@v0.2.0 ./...`. Go 1.21+ obtains the released linter's toolchain and dependencies. This runs only lint rules; see [the direct linter](rules.md#running-the-linter-directly).
- **Source launcher:** clone an existing release with `git clone --branch v0.2.0 --depth 1 https://github.com/wangjohn/levenshtein.git`, then run `./levenshtein/verify --source /absolute/path/to/app`. Run that command from the directory containing the clone; `--source` names your application, not the Levenshtein checkout.
- **Prebuilt CLI:** extract a [release archive](releases.md#running-an-archive), keep its files together, and pass both `--shared /absolute/path/to/archive` and `--source /absolute/path/to/app` to its `levenshtein` binary. The prebuilt CLI needs no host Go; native Go checks still need the archive's pinned Go version on `PATH`.

These examples pin the existing **v0.2.0** release. The tool versions on this page describe this documentation's checkout; when using a release, read its `.go-version` and documentation for its pins. Select the executor through the application's configuration: choosing a source checkout or prebuilt CLI does not change the default Dagger execution.

If downloads are denied, configure an approved `GOPROXY` or provision the pinned toolchain, modules, and check tools in advance. A prebuilt archive removes the CLI build step; it does not remove check-tool downloads or container image requirements. See [download troubleshooting](troubleshooting.md#the-pinned-go-cannot-be-downloaded).

If Docker is absent or stopped, start a runtime and confirm `docker info` succeeds before running the default checks. To inspect the configuration without Docker, use `--dry-run`; to execute without Docker, explicitly configure [native environments](configuration.md#native-go-checks) and install their required tools.

## Run checks

From the Levenshtein checkout, point `--source` at the repository to verify. For a repository with no `levenshtein.json`, the runs are the zero-config defaults:

```sh
./verify --source /path/to/app              # branch: go-lint, go-vet, go-mod
./verify pre-merge --source /path/to/app    # the same three checks
./verify main --source /path/to/app         # adds go-vuln, and skips the result cache
./verify go-lint --source /path/to/app      # one check
./verify pre-merge --source /path/to/app --dry-run   # print the plan without running it
```

A repository with a `levenshtein.json` gets the runs it declares instead. Without `--source`, the launcher verifies the Levenshtein checkout itself, with its own `levenshtein.json`; see [develop the shared checks](#develop-the-shared-checks).

A run name selects checks; it does not switch Git branches or fetch code. The source directory is what gets verified, so CI supplies the pull request, merge candidate, or default-branch checkout.

`verify` prints a JSON report and exits `0` when every check passes, `1` when one fails, and `2` when the command or configuration is wrong. Tool errors, invalid configuration, and modules with no Go packages fail rather than reporting an empty pass. The [CLI reference](reference/cli.md) lists every flag, output format, and exit code.

Dependency resolution follows Go's defaults: use `vendor/` when enabled by the module or workspace, otherwise use read-only module resolution. Source filtering excludes `.git`, `.env`, and `.env.*` at every level, with an explicit exception for public `.env.example` templates so Go can embed them. Keep those templates free of secrets; other `.env.*` names remain excluded.

[Go lint rules](rules.md) lists every rule `go-lint` enforces and why.

### What the rules catch

Moved to [Go lint rules](rules.md#rules-on-by-default).

## Configure a repo

A single Go module at the source root works without configuration. For multiple modules or custom runs, add a version 1 `levenshtein.json` to that repo; [configuration](configuration.md) is the guide, and the [configuration reference](reference/config.md) lists every field.

Version 2 is available starting with v0.3.0; released consumer templates remain at version 1. See [community-rule migration](configuration.md#community-rule-modules) for its explicit completeness check.

Use the [version 1 consumer example](consumer-ci.md#the-same-command-locally-and-in-ci) for explicit product targets, checks, and run selections. `inputs` restricts Dagger's imported source as well as its cache scope; include required manifests, local dependencies, and fixtures. Native command inputs only describe cache scope and do not restrict host access. See [source boundaries](configuration.md#source-boundaries).

A configuration file replaces defaults. Paths are relative to the source root. Every selected check runs or reuses an eligible result; change-based selection is not implemented. [Check kinds](check-kinds.md) lists every kind you can use. Every configuration file declares a supported `version`; use `1` with v0.2.0 and earlier pins, or `1` or `2` starting with v0.3.0.

Version 1 runs use explicit `rerun_checks: true` for fresh audits, regardless of their name. Levenshtein's own `main` is configured that way. Audits bypass passing-verdict reuse while retaining compatible downloads and compiler caches. Vulnerability scans always execute against current advisory data. Add new checks explicitly to your configured full run during this pilot.

## Develop the shared checks

To work on Levenshtein itself, see [developing Levenshtein](maintainers/development.md#develop-the-shared-checks): the pinned Dagger CLI, the generated SDK, and the test commands CI runs.

### Initial measurements (original Dagger-only launcher)

Moved to [developing Levenshtein](maintainers/development.md#initial-measurements-original-dagger-only-launcher).

## Pinned dependencies

Moved to [developing Levenshtein](maintainers/development.md#pinned-dependencies).

## Standalone planning and configuration

Moved to [configuration](configuration.md) and the [CLI reference](reference/cli.md).

## Dagger integration

Moved to [developing Levenshtein](maintainers/development.md#dagger-integration).

## Levenshtein's own CI

The workflows that verify this repository are described in [Levenshtein's own CI](maintainers/ci.md), for maintainers. Each heading below keeps an old link working.

### Jobs

Moved to [Levenshtein's own CI](maintainers/ci.md#jobs).

### Required checks (branch protection)

Moved to [Levenshtein's own CI](maintainers/ci.md#required-checks-branch-protection).

### Result-cache trust

Moved to [Levenshtein's own CI](maintainers/ci.md#result-cache-trust).

### Caches and self-config notes

Moved to [Levenshtein's own CI](maintainers/ci.md#caches-and-self-config-notes).

### Success criteria and gaps

Moved to [Levenshtein's own CI](maintainers/ci.md#success-criteria-and-gaps).

### Out of scope here

Moved to [Levenshtein's own CI](maintainers/ci.md#out-of-scope-here).
