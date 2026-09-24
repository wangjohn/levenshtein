# Use Levenshtein from your existing CI

Your CI checks out the application, chooses a run, and invokes a pinned Levenshtein version. Levenshtein prepares the check environment and returns results and an exit status. Application tests and CI schedules belong to the application repo.

**Available now:** shared Go lint, vet, module manifest checks, HTTP/SQL cleanup checks, vulnerability scanning, tests (`go-test`), workflow lint and workflow security, mutation testing (`go-mutation`), native commands, local result/setup/build caching, text/GitHub/SARIF output, and a findings [baseline](configuration.md#baseline) for adopting the rules with existing findings. Start with one product target and a few useful checks; keep existing CI gates while proving equivalent behavior.

## The same command locally and in CI

Use the [runtime prerequisites](setup.md#prerequisites): any Go on `PATH` for the source launcher, which provisions the pinned toolchain itself, and a Docker-compatible runtime for checks bound to a Dagger environment. The SDK provisions the pinned Dagger CLI. Keep the application and Levenshtein in separate directories:

```text
workspace/
  app/          # your application checkout
  levenshtein/  # shared checks at a pinned release
```

From `workspace/`, run:

```sh
./levenshtein/verify pre-merge --source ./app
./levenshtein/verify main --source ./app
```

The first command runs the consumer's `pre-merge` checks. The second runs the configured audit; set `rerun_checks: true` on `main` in version 1 configuration to make it fresh. Your CI decides which command to invoke on a PR, push, or schedule.

A single Go module at the application root works without configuration. For adoption, prefer explicit version 1 configuration in the **application** repo:

```json
{
  "version": 1,
  "targets": {
    "api": {"dir": "services/api", "inputs": ["services/api", "contracts", "go.work", "go.work.sum"]}
  },
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "lint": {"kind": "go-lint", "target": "api", "environment": "go"},
    "vet": {"kind": "go-vet", "target": "api", "environment": "go"},
    "modules": {"kind": "go-mod", "target": "api", "environment": "go"},
    "vulnerabilities": {"kind": "go-vuln", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["lint"]},
    "pre-merge": {"checks": ["lint", "vet", "modules"]},
    "main": {"checks": ["lint", "vet", "modules", "vulnerabilities"], "rerun_checks": true}
  }
}
```

Adjust paths to your repo. For Dagger checks, `inputs` is the source allowlist **and** cache input scope: include every required workspace module, local dependency, manifest, fixture, and lockfile. Missing optional paths are allowed. Private directories stay outside the allowlist. Do not use `"."` in a mixed product/private repository. Symlinked inputs are rejected; declare real paths. See [source boundaries](configuration.md#source-boundaries).

Application tests stay in the application repo. Wrap existing test scripts with a native `command` check using the [native configuration](configuration.md#native-commands); the host must supply Go, Postgres, Xcode, or any other required tools/services. Native commands have host access and are not restricted by the Dagger allowlist. Self-contained unit tests can instead run as the shared [`go-test`](checks.md#tests) check, which runs `go test -race ./...` on either executor; tests that need a service stay in a `command` check.

For an advisory model review of each pull request, add a native environment, a `semantic-lint` check in its own run, and supply `TYPESAFE_API_KEY` from a CI secret with `fetch-depth: 0` on checkout. The check reads the key and the pull request's base branch from the host environment itself. Findings never fail the run. See [semantic lint](semantic-lint.md).

## GitHub Actions

Call the action at the repository root. The ref you pin is the revision of the shared checks: GitHub downloads Levenshtein at that ref outside your workspace, so the shared runner's files never enter the verified source. This is a complete workflow:

```yaml
name: Application verification

on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]
  merge_group:
  push:
    branches: [main]
  schedule:
    - cron: '23 7 * * *' # This application's daily audit.
  workflow_dispatch:

permissions:
  contents: read

jobs:
  verify:
    runs-on: ubuntu-24.04
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: wangjohn/levenshtein@v0.1.0
```

With no `run` input, the action picks one from the event: a schedule runs `main`, a push or draft pull request runs `branch`, and a ready pull request, merge queue, or manual dispatch runs `pre-merge`. Pass `run:` to choose explicitly, for example one job per run. If a run includes [`go-mutation`](mutation.md) or `semantic-lint`, check out with `fetch-depth: 0`: both diff against the base branch.

| Input | Default | Meaning |
| --- | --- | --- |
| `run` | chosen from the event | Run to execute |
| `source` | `.` | Directory to verify, relative to the workspace |
| `setup-go` | `true` | Install the Go version this revision pins. Set `false` when the job already provides Go |
| `cache` | `true` | Restore and save completed results and the Staticcheck analysis cache with `actions/cache` |
| `annotations` | `true` | Annotate each failing finding, and each check that did not reach a verdict, on the run and on the pull request's files |
| `sarif` | empty | Path, relative to the workspace, to write a SARIF file to for code scanning. Empty writes none |

The action's outputs are `run`, the run it executed, `report`, the path to the JSON report, and `sarif`, the path of the SARIF file when the `sarif` input asked for one. The job summary has a status table and lists up to 50 failing findings; findings the [baseline](configuration.md#baseline) accepts are counted there, not listed. A failed check fails the step. Make the job a required status check in the application's branch protection or ruleset.

The annotations and the SARIF file are rendered from the saved JSON report with `verify --render` after the Verify step, so they never run the checks again or change the step's result, and a repository whose `source` is a subdirectory gets paths relative to the workspace. Neither needs a permission beyond `contents: read`: annotations are workflow commands in the step's log. [Output formats](configuration.md#output-formats) describes both.

**Code scanning.** To see findings in the repository's Security tab and on pull request diffs, pass `sarif:` and upload the file in a later step of the same job. The upload, not this action, needs `security-events: write`; a private repository's upload also needs `actions: read`. A pull request from a fork never gets `security-events: write`, so upload only from same-repository events and keep annotations for forks:

```yaml
jobs:
  verify:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      security-events: write
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - id: levenshtein
        uses: wangjohn/levenshtein@v0.1.0
        with:
          sarif: levenshtein.sarif
      - if: >-
          ${{ !cancelled() && steps.levenshtein.outputs.sarif != '' &&
          (github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository) }}
        uses: github/codeql-action/upload-sarif@1c5b675653bb5c22dbe9b12b556ec555138e09fd # v4.38.1
        with:
          sarif_file: ${{ steps.levenshtein.outputs.sarif }}
          category: levenshtein
```

`!cancelled()` uploads after a failing Verify step too, which is when there is something to see. The `annotations` and `sarif` inputs are new since 0.1.0 (see the [changelog](../CHANGELOG.md)); a pin to 0.1.0 does not have them. [`templates/github/workflows/levenshtein.yml`](../templates/github/workflows/levenshtein.yml) is this workflow ready to copy, and [coding agents](agents.md) describes the other templates.

**Adopting the rules with existing findings.** Name a `baseline` file in `levenshtein.json`, run `verify main --source . --write-baseline` once over the whole repository, and commit the file. From then on a new finding fails the job, a baselined one is reported without failing it, and fixing a baselined finding fails until its entry is deleted in the same change, so the file only shrinks. `verify` never adds entries on its own; only `--write-baseline` does, so an entry that grows the file shows up in review. Consider a `CODEOWNERS` entry for the file. See [baseline](configuration.md#baseline).

**Pin a release.** `@v0.1.0` names a published [release](releases.md). To pin immutably, use that tag's commit SHA with the version as a comment, as this repository does for every action it calls. Dependabot's `github-actions` ecosystem proposes new Levenshtein releases like any other action, including the SHA and comment. Do not pin a branch.

**Caching and trust.** The action keeps two caches. Completed results are small records, saved per commit from every event and keyed by runner OS, architecture, and job; a pull request's entries live in that pull request's own cache scope, which the default branch never reads, so an untrusted pull request cannot seed `main`'s results. Every result is re-keyed by a content fingerprint before reuse, so a restored directory can only skip work, never change a verdict. The Staticcheck analysis cache, used by native `go-lint`, is saved only from pushes to the default branch and scheduled runs, keyed by a hash of the pinned linter; pull requests restore it and never write it. Helper binaries are not cached, because they rebuild from Go's build cache in seconds.

## A native lint job without Docker

`go-lint`, `go-vet`, `go-mod`, `go-test`, `go-imports`, `go-generate`, `go-apidiff`, `workflow-lint`, `workflow-security`, `shell-lint`, and `go-vuln` also run on a [native environment](configuration.md#native-go-checks), using the host's Go instead of a container. Declare it in the application's `levenshtein.json`:

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "inputs": ["go.mod", "go.sum", "cmd", "internal"]}},
  "environments": {"host": {"executor": "native"}},
  "checks": {
    "lint": {"kind": "go-lint", "target": "app", "environment": "host"},
    "vet": {"kind": "go-vet", "target": "app", "environment": "host"},
    "vulnerabilities": {"kind": "go-vuln", "target": "app", "environment": "host"}
  },
  "runs": {
    "branch": {"checks": ["lint"]},
    "pre-merge": {"checks": ["lint", "vet"]},
    "main": {"checks": ["lint", "vet", "vulnerabilities"], "rerun_checks": true}
  }
}
```

The workflow is unchanged. The job needs no container runtime, and the action's caches restore the Staticcheck analysis cache and completed results across workers, which Dagger's in-engine cache volumes cannot do on ephemeral runners; setup-go's cache keeps the helper builds fast. A native check's result key includes the host's Go version, operating system, and architecture, so a job that changes runner image or Go version re-verifies rather than reusing another host's verdict.

To audit the workflows too, add a [`workflow-security`](checks.md#workflow-security) check on a repository-root target whose `inputs` include `.github` (and `action.yml` for an action repository), in its own run or beside `lint`. It runs zizmor's offline audits only, so keep [zizmor's GitHub Action](https://github.com/zizmorcore/zizmor-action) with the workflow token, ideally on a weekly schedule, if you also want the audits that query GitHub, such as `impostor-commit` and `known-vulnerable-actions`. The native check downloads the pinned zizmor archive into `--cache-dir`'s `tools/` directory, so restoring that directory saves the download.

The same repository-root target can carry [`shell-lint`](checks.md#shell-scripts) for its shell scripts, beside `lint`: it is reused from the cache like `lint`, and the native check keeps the ShellCheck download under `--cache-dir`'s `tools/` directory.

To run the unit tests through Levenshtein as well, add a [`go-test`](checks.md#tests) check, which runs `go test -race ./...` and needs a C compiler on the worker (the GitHub-hosted Ubuntu and macOS images have one). Its result is reused like `lint`'s, so give it a target whose `inputs` cover everything the tests read, and put it in a run of its own or beside `lint` only if the workflow does not already run `go test -race` over the same modules. Tests that need a database or another service stay in a [`command` check](configuration.md#native-commands) that starts it, or in the job's existing test step.

A library can fail pull requests that break its exported API with a [`go-apidiff`](checks.md#api-compatibility) check. It compares with the merge base of the pull request's base branch, which GitHub names in `GITHUB_BASE_REF`, so check out with `fetch-depth: 0`; a shallow clone is an error that says so.

## CircleCI and other providers

Use the same arrangement in an existing job: check out the application, fetch the pinned Levenshtein release beside it, provide any Go for the source launcher and a Docker-compatible runtime for Dagger checks, and pick a run from the provider's trigger:

```sh
git clone --depth 1 --branch v0.1.0 https://github.com/wangjohn/levenshtein ../levenshtein
../levenshtein/verify pre-merge --source .
```

Keep the Levenshtein checkout outside the application directory. Alternatively, download a platform archive from the [release](releases.md), which needs no Go compiler. Configure PR triggers, daily schedules, and required results through the provider. Restore and save a `--cache-dir` outside both checkouts with the provider's cache feature to reuse results across workers. Cache its `results/`, `stages/`, and `stat/` directories per commit; its `staticcheck/` directory is shared analysis data that only trusted default-branch jobs should write, and its `tools/` directory rebuilds cheaply.

Cross-worker cache transport beyond that directory and multi-job result aggregation are not implemented; keep required platform jobs individually required. Do not share writable result caches with untrusted PRs. See [the roadmap](roadmap.md#consumer-pilot-acceptance) for planned pilot adoption steps.
