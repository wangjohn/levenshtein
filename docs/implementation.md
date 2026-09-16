# Implementation plan

Status: Dagger path selected. The first slice implements pinned Go lint, configurable runs, rule fixtures, and CI for Levenshtein itself. See [setup and usage](setup.md) for the current behavior and [consumer CI](consumer-ci.md) for invoking it from another repo.

**Next milestone:** wrap a Go application's existing tests alongside shared lint, then run both locally and in that application's existing CI. Application test execution and enrollment of a real consumer are still to be implemented.

The [README](../README.md) is the product overview. This document defines the pilot scope, implementation sequence, and completion criteria.

## Starting paths

These are alternative execution approaches. Each can centralize shared checks and use the existing CI provider.

| Path | What we build | Main tradeoff | When to choose it |
| --- | --- | --- | --- |
| Shared workflows and native commands | Shared linter configuration, scripts, and a reusable CI workflow | Smallest setup; local tool versions and service setup still need explicit management | Proving reuse quickly across similar Go repos |
| Dagger module and a small launcher | Shared containerized checks, invoked through `./verify` locally and in CI | Consistent execution setup; requires Dagger and a container runtime, with startup cost to measure | Recommended for the intended mix of repos, agents, and eventual service tests |
| Standalone Go CLI | A distributed binary that selects checks and invokes native tools | Convenient entry point; we own tool installation, environment handling, and distribution | Container requirements are unacceptable and a wrapper has become insufficient |
| Hosted service | Repository integration, run history, rollout coordination, and agent jobs | Adds authentication, storage, operations, and tenancy work | Several users need coordination that existing CI and version updates cannot provide |

**Selected:** the Dagger path. Validate it with the defer/Close lint family before expanding it. Measure both a cold run and repeated local runs. Keep the repo-facing commands small; do not build multiple execution backends or a compiled CLI in the pilot. If container setup or latency makes this unsuitable, reconsider native commands with the same shared checks and run names.

GitHub [reusable workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows) support centralized workflow code and commit-pinned references. Dagger provides [shareable modules with pinned dependencies](https://docs.dagger.io/0.21/features/reusability/) and supports a [container runtime](https://docs.dagger.io/reference/container-runtimes/docker). These are implementation building blocks; Levenshtein supplies the shared verification policy and improvement process.

## Before writing the runner

1. Choose the first Go consumer repo and a second repo for proving reuse. Record current commands, Go versions, module layout, CI provider, services, and one recurring mistake. Until a consumer is selected, use a small fixture project inside Levenshtein.
2. For the recommended path, make a container runtime and a pinned Dagger CLI available locally and in CI. Select a compatible engine/SDK/toolchain combination using documentation for that release. The default [Dagger installation guide](https://docs.dagger.io/getting-started/install/) currently describes a beta; do not mix examples from different release lines or assume the host's Go version is suitable.
3. Choose one small check with a bad example and a legitimate example. A practical starting point is Staticcheck's [SA5001](https://staticcheck.dev/docs/checks/#SA5001), which detects deferring `Close` before checking an error. Existing checks also cover some other misplaced-defer patterns. Verify the specific fixture; they do not establish that every `defer` in a loop is wrong.

Implementation snapshot, September 15, 2026: the first slice uses Go 1.27.1, Dagger 0.21.9, and Staticcheck 2026.2.1. The local setup uses Colima and Docker. No external consumer repo has been enrolled.

## Pilot scope and ownership

Start with two Go repositories and their existing CI. Use Postgres only if a pilot needs it. The pilot has five parts:

- **Shared checks:** versioned folders of linter configuration, test commands, CI checks, and small fixtures. Use existing tools; add custom rules for demonstrated gaps.
- **A small runner:** a Dagger-based wrapper that uses the same pinned tools and check definitions locally and in CI. Keep test state isolated and start services only for checks that need them.
- **Repo configuration:** native commands, named runs, and a pinned shared revision. Application-specific tests stay beside the application code. CI workflows decide when runs execute.
- **Useful results:** native failure output, check names, timings, and rerun commands, plus a small machine-readable result file. Store these with existing CI logs and artifacts.
- **An improvement loop:** one bounded background job proposes a repair or shared check for human review. Update consuming repos manually at first.

The shared code lives in Levenshtein; each application repo invokes its pinned version. Existing CI supplies workers, triggers, schedules, credentials, and required status checks. Levenshtein selects checks, prepares their execution environment, and reports their results. Application tests stay in the application repo. Levenshtein's own workflow verifies the shared runner and its fixtures; each consumer owns its CI integration.

## Everyday use

Current interface (check scope will grow through the pilot):

```sh
./verify                # defaults to branch
./verify pre-merge      # required checks before merging
./verify go-lint        # shared Go checks, e.g. misplaced defers
```

A **check** verifies something. A **run** selects checks and can combine tests, lint rules, and CI/CD assertions. Names are configurable: use `./verify <name>` for any run.

Recommend these runs and invocation times; each repo's CI owns the event mapping:

| Run | When | What runs |
| --- | --- | --- |
| `branch` | During editing and ordinary PR updates | Main inexpensive lint and correctness checks, plus focused tests |
| `pre-merge` | Before merging | Critical regression and policy checks, plus important checks relevant to the change |
| `main` | Once a day on the configured default branch | The complete applicable suite, including newly registered checks |

`./verify pre-merge --dry-run` lists selected checks and modules without executing them. The flag works with any run. The first slice uses explicit module lists; reasons based on changed files come later.

Start with explicit core checks and simple path/package mappings. Include task-specific acceptance checks and new or modified tests in change verification. A Go package change runs its lint and focused tests; uncertain dependencies broaden the relevant suite. Check importance follows failure consequences and known defects.

The consuming repo's CI schedules `main` daily, including on days without changes. Invoking `main` executes the configured audit immediately. Build artifacts can be reused, but cached passing verdicts cannot replace the audit. Declare its supported environments; missing expected prerequisites make the run incomplete. Complete audits run on their cadence rather than every main-branch push.

Required checks must pass for the final proposed revision and relevant base state. Report the scope actually verified. Missing results, tool failures, and timeouts cannot become a passing gate, and a `branch` pass does not satisfy an unfinished `pre-merge` run.

## How improvements spread

1. Capture a bug, flaky check, slow run, or recurring review comment in an issue.
2. Reproduce the problem and propose the cheapest reliable check or repair.
3. Validate it against a bad example and a legitimate example; measure the added runtime.
4. Review the change and update one consuming repo's pinned revision.
5. Apply the same improvement to the second applicable repo.

For example, repeated cleanup mistakes could lead to a shared Go rule in the configured `go-lint` run. It flags `defer file.Close()` inside a loop when each iteration should close its file. The diagnostic explains that cleanup waits for the surrounding function to return and suggests moving each iteration's work into a helper with its own `defer`. Validate the problematic pattern and the correct helper before sharing the rule. Behavioral bugs still need regression tests; share those where repositories expose the same relevant behavior.

A scheduled agent job works on one candidate at a time with an enforced time/cost limit. It proposes changes for review and avoids repeating unchanged findings. If the `main` audit catches a regression missed before merge, consider adding a focused check to `pre-merge`.

Use ordinary issues and Markdown for acceptance criteria. For bug fixes, demonstrate failure on the bad revision and success on the fix. Agents may propose changes to checks, but weakening assertions, adding suppressions, or changing required coverage needs separate review. A passing retry alone does not prove a flake was repaired.

## Build order

### 1. Make one shared check work end to end

Create the shared Go check configuration, a small Dagger runner, a thin `verify` launcher, and good/bad fixtures. Pin the tool versions. Start with an existing analyzer; a new custom analyzer is needed only if the chosen defect is not covered adequately.

**Done when:** `./verify go-lint` passes the good fixture, fails the bad fixture for the intended diagnostic, prints the finding and rerun command, and preserves failure through CI. Record cold and warm durations. The first useful result is a shared check running correctly, not a general check framework.

Implemented code locations, alongside the existing docs:

```text
runner/                 # shared Go Dagger module and pinned toolchain.json
runner/testdata/        # good/bad/broken/empty Go modules
verify                  # thin launcher; --source accepts a consumer checkout
scripts/install-dagger # checksummed CLI installer
.github/workflows/      # verification of Levenshtein and its fixtures
```

Generated Dagger files follow the chosen release's conventions. Keep intentionally failing fixtures outside ordinary application test discovery and validate their expected outcomes explicitly.

### 2. Wrap the first consumer's existing tests (next)

Choose a Go application and record how its tests already run, including package scope, flags, and any required services. Add a shared `go-test` check that invokes those native Go tests through Dagger and composes with `go-lint` in named runs. `go-test` is planned; the current runner accepts only `go-lint` and `self-test`.

Keep test files and assertions in the application repo. The shared wrapper owns tool versions, environment setup, execution, and reporting. Preserve native failure output and nonzero exit status. A fresh `main` audit must bypass both Dagger's execution cache and Go's cached test results, while allowing dependency and compiler caches.

Pin Levenshtein in the consumer and add an invocation to its existing CI. Pass the consumer's source directory and run configuration to the shared module, following the [consumer guide](consumer-ci.md). Start with explicit core tests for `branch` and `pre-merge` and the complete applicable suite for `main`. Add change-based selection after this path works.

**Done when:** the real consumer runs its existing tests and shared lint through the same command locally and in its existing CI; a failing assertion and a lint violation each fail the CI job with useful output; custom runs work; and `main` reruns tests even without source changes. Add formatting and other checks after proving this path.

### 3. Connect the run policy to real CI events

Configure events and schedules in the consuming repo's CI. The runner receives the chosen run and checked-out source; Levenshtein's own workflow remains responsible for checking Levenshtein itself.

For GitHub Actions, use local/draft-PR feedback for `branch`. Run `pre-merge` when a PR is opened ready for review, becomes ready, or receives new commits while ready. Configure it as a required check and ensure the tested candidate includes the relevant base revision. Include `merge_group` if the repo uses a merge queue. Trigger `main` on a daily schedule and allow manual invocation. Existing CI providers can use their equivalent mechanisms. See GitHub's [PR, merge-queue, and schedule events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows).

Keep execution on one pinned Levenshtein revision; a reusable workflow reference and fetched check definitions must not silently use different versions. Shared-code changes in Levenshtein must run their own fixture and compatibility checks before release. App-repo updates remain explicit version changes.

**Done when:** a deliberately failing change is blocked, a development result cannot satisfy an incomplete pre-merge gate, and a scheduled audit executes the whole applicable suite fresh. Record its revision and completion time. Scheduling is not a guarantee of execution; make missed or incomplete audits visible in the CI summary.

### 4. Prove that an improvement transfers

Enroll the second Go repo with the same shared setup. Change one lint rule or check in Levenshtein, validate it against fixtures and the first consumer, then update the second repo's pin. Use a real historical mistake where possible.

**Done when:** both repos catch the intended defect from one maintained definition, with no duplicated rule implementation. Adoption consists of a small reviewed version/configuration change.

### 5. Add the smallest useful background agent job

Once results exist, schedule one agent job with access to recent failures or a supplied issue. Limit it to one candidate and an enforced time/cost budget. Require a reproducer, before/after evidence, and a proposed diff. Human review controls acceptance and rollout. Keep write credentials separate from untrusted verification jobs.

**Done when:** the agent proposes one useful repair or shared improvement, validates it without weakening acceptance, and the reviewed change can follow the same two-repo adoption path. Automatic deployment, broad comment mining, fleet analytics, and automatic rollout remain deferred.

## The pilot is done when

- Both repos run shared lint, tests, and CI checks consistently locally and in CI.
- Deliberate failures block the right gate; custom runs and daily discovery work.
- One shared improvement catches a demonstrated mistake in both repos.
- An agent uses the diagnostics to fix a real problem without weakening acceptance.
- Timings show everyday feedback stays fast, with the complete applicable suite executed daily.

## Later, when needed

Defer advanced test impact analysis, distributed sharding, shared-cache infrastructure, automated rollout, portfolio analytics, formal agent evaluations, LLM grading, general plugin interfaces, and dedicated failure-reproduction tooling.

Deployment orchestration, artifact promotion, and rollback management also wait; the pilot verifies CI configuration and invokes existing checks.

These are possible V1 expansions, driven by measured problems. They are not all requirements for V1. [Optional design notes](design-notes.md) retain implementation considerations without expanding the pilot.
