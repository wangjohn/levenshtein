# Use Levenshtein from your existing CI

Your CI checks out the application, chooses a run, and invokes a pinned Levenshtein version. Levenshtein prepares the check environment and returns results and an exit status. Application tests and CI schedules belong to the application repo.

**Available now:** shared Go lint. The [next implementation phase](implementation.md#core-interface) introduces language-independent checks, native macOS execution, and aggressive caching, then [wraps Benchplan's existing checks](implementation.md#3-wrap-benchplans-existing-checks). The example below uses the current Go interface.

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

The first command runs the consumer's `pre-merge` checks. The second immediately runs its fresh audit. Your CI decides which command to invoke on a PR, push, or schedule.

A single Go module at the application root needs no configuration for the current lint checks. For multiple modules or custom runs, put [`levenshtein.json`](setup.md#configure-a-repo) in `app/`. The runner reads that application's configuration. Its test files stay in `app/` as test execution is added.

## Example: an application using GitHub Actions

Add these steps to the application's existing workflow, or start with this small workflow. Both checkouts are siblings so the shared runner's files stay outside the application source passed to verification. The full commit below pins the current lint implementation; adopt shared improvements by reviewing and updating that pin.

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
          ref: ba9acb3d59ef5b25fffe852e4d86028f69b723cb
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
