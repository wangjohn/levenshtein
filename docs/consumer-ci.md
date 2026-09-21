# Use Levenshtein from your existing CI

Your CI checks out the application, chooses a run, and invokes a pinned Levenshtein version. Levenshtein prepares the check environment and returns results and an exit status. Application tests and CI schedules belong to the application repo.

**Available now:** shared Go lint, vet, HTTP/SQL cleanup checks, vulnerability scanning, workflow lint, native commands, and local result/setup/build caching. Start with one product target and a few useful checks; keep existing CI gates while proving equivalent behavior.

## The same command locally and in CI

Use the [runtime prerequisites](setup.md#prerequisites): Go for the source launcher and a Docker-compatible runtime for Go checks. The SDK provisions the pinned Dagger CLI. Keep the application and Levenshtein in separate directories:

```text
workspace/
  app/          # your application checkout
  levenshtein/  # shared checks at a reviewed commit
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
    "vulnerabilities": {"kind": "go-vuln", "target": "api", "environment": "go"}
  },
  "runs": {
    "branch": {"checks": ["lint"]},
    "pre-merge": {"checks": ["lint", "vet"]},
    "main": {"checks": ["lint", "vet", "vulnerabilities"], "rerun_checks": true}
  }
}
```

Adjust paths to your repo. For Dagger checks, `inputs` is the source allowlist **and** cache input scope: include every required workspace module, local dependency, manifest, fixture, and lockfile. Missing optional paths are allowed. Private directories stay outside the allowlist. Do not use `"."` in a mixed product/private repository. Symlinked inputs are rejected; declare real paths. See [source boundaries](configuration.md#source-boundaries).

Application tests stay in the application repo. Wrap existing test scripts with a native `command` check using the [native configuration](configuration.md#native-commands); the host must supply Go, Postgres, Xcode, or any other required tools/services. Native commands have host access and are not restricted by the Dagger allowlist. No shared `go-test` check is provided yet.

For an advisory model review of each pull request, add a native environment, a `semantic-lint` check in its own run, and supply `TYPESAFE_API_KEY` from a CI secret with `fetch-depth: 0` on checkout. The check reads the key and the pull request's base branch from the host environment itself. Findings never fail the run. See [semantic lint](semantic-lint.md).

## Example: an application using GitHub Actions

Add these steps to the application's existing workflow, or start with this small workflow. Both checkouts are siblings so the shared runner's files stay outside the application source passed to verification. The full SHA below pins the readiness implementation, including source boundaries. Adopt shared improvements by reviewing and updating that pin; do not use a moving branch.

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
      - name: Check out application
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          path: app
          persist-credentials: false
      - name: Check out shared checks
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          repository: wangjohn/levenshtein
          ref: 314238953567969b8092ff150ed502a709d1a3b5
          path: levenshtein
          persist-credentials: false
      - name: Set up Go for the source launcher
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: levenshtein/.go-version
          cache-dependency-path: levenshtein/go.sum
      - name: Verify application
        env:
          EVENT: ${{ github.event_name }}
          DRAFT: ${{ github.event.pull_request.draft }}
        run: |
          run=pre-merge
          if [[ "$EVENT" == schedule ]]; then
            run=main
          elif [[ "$EVENT" == push || "$DRAFT" == true ]]; then
            run=branch
          fi
          ./levenshtein/verify "$run" --source ./app
```

The job's normal shell failure handling propagates the launcher's nonzero exit status. Configure the application's required check in its existing repository settings. The schedule above belongs to this application; Levenshtein's own daily workflow checks its shared runner and fixtures.

## CircleCI and other providers

Use the same arrangement in an existing job: check out the application and pinned shared revision, provide Go for the source launcher and a Docker-compatible runtime, and invoke `verify` with the application source. Configure PR triggers, daily schedules, and required results through that provider. Provider-specific bootstrap configuration remains in the consuming repo; the shared checks receive a source directory and a run name.

## Pilot acceptance

Start with Family Books' Go API using explicit product inputs, then wrap a small Benchplan check on its existing macOS worker. Keep Postgres tests and simulator checks in their existing jobs until their wrappers are verified.

For each pilot, record cold, unchanged warm, source-edit, unrelated-file-edit, and fresh-audit timings. Introduce one deliberate lint/test failure to prove the existing CI gate receives a nonzero exit. Confirm a fresh run executes checks while keeping compatible build caches. Start native commands without result caching; enable it only with complete inputs, a provisioned environment identity, and an explicit `rerun_command`.

Local caches work today. Cross-worker cache transport and multi-job result aggregation are not implemented; keep required platform jobs individually required. Do not share writable result caches with untrusted PRs.
