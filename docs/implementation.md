# Implementation plan

**Implemented:** a standalone Go CLI with versioned configuration, tool-free planning, common results, and the preserved pinned Go lint through Dagger. Legacy configuration, rule fixtures, and CI for Levenshtein itself remain supported. [Setup](setup.md) and [consumer CI](consumer-ci.md) document the current interface.

**Next:** pilot Family Books Go product checks, then wrap Benchplan's existing native checks and connect consumer CI cache persistence. Rust/Python compatibility fixtures now validate the shared interface; see [fixture checks](language-fixtures.md). Native commands, pinned-tool validation, timeouts, artifacts, and shared preparation within a run are implemented. Local result caching, separate preparation/build keys, artifact restoration, and fresh execution are implemented. Cross-worker cache transport and consumer adoption below remain planned; the current CLI accepts both legacy Go module lists and version 1 targets/checks/environments/runs, with shared Go lint, vet, HTTP/SQL resource checks, vulnerability scanning, workflow lint, `self-test`, and native `command` checks. See [configuration](configuration.md).

## Core interface

A repo can contain several components and languages. Use four concepts:

| Concept | Responsibility | Example |
| --- | --- | --- |
| Target | A component's working directory, workspace context, and explicitly declared source inputs | A Go service, Swift application, Cargo package, or Python package |
| Check | A named verification operation, its options, input fingerprint, and results | Shared Go lint, Swift tests, or a repo-owned script |
| Environment | Execution requirements, tool versions, and resolved dependency setup | A pinned Linux container or a macOS/Swift toolchain |
| Run | A selection of checks and cache/freshness policy | `branch`, `pre-merge`, `main`, or a custom name |

Keep configuration, planning, cache lookup, and reporting in a small Go runner outside the Dagger module. Preserve the `./verify` entry point. Dagger executes container checks; a native executor launches checks on the supplied host. Keep language-specific workspace rules, toolchain details, dependency setup, and native diagnostic parsing inside check implementations. Planning should work without starting Docker or requiring Xcode; start an executor only when a selected check needs execution.

Shared, versioned check definitions own reusable tool setup, defaults, options, execution, and interpretation of results. Start with the existing Go lint check and a `command` check for application-owned scripts; add shared language test and lint definitions as consumers need them. An internal registry is enough initially. A new language should require a check implementation or configuration, with the same planner, run model, and result contract.

Targets distinguish the working directory from their workspace context and declare required inputs beyond that directory: root manifests/lockfiles, local dependencies, shared configuration, fixtures, generated inputs, or contracts. A repo may contain targets in several languages; a check may prepare multiple toolchains, as for a Python package with a Rust extension. Supply explicit product inputs for Family Books. Application tests and assertions stay beside their application code; shared rules and execution definitions live in Levenshtein. Consumers adopt shared improvements by updating a pinned revision.

Existing CI owns workers, triggers, schedules, credentials, and merge gates. It supplies the appropriate workers for checks requiring execution. When a run spans jobs or platforms, aggregate results against the complete selected check list; missing checks or unavailable environments without eligible cached results leave the run incomplete. Levenshtein's own workflow verifies its runner and fixtures.

## Rust and Python compatibility

The same interface must accommodate both languages. The [compatibility fixtures](language-fixtures.md) exercise this interface through native commands. Full shared language integrations remain consumer-driven:

| Concern | Rust | Python |
| --- | --- | --- |
| Target context | Cargo package plus workspace root and local dependencies | Package/service plus shared configuration, fixtures, and local dependencies |
| Checks | Formatting, Clippy, and tests | Ruff, type checking, and pytest |
| Execution variants | Toolchain, selected packages, features, build profile, and target platform | Interpreter, resolved dependencies, groups/extras, plugins, and test selection |
| Reusable work | Dependency downloads and compatible compiled artifacts | Dependency downloads, built packages, and compatible prepared environments |

Adapters include execution variants in result identity and report the scope verified. Keep these options inside check definitions; the planner needs no language-specific branches. Preserve repo-owned commands and dependency tools, then add shared recipes when repeated setup warrants them. [Design notes](design-notes.md#language-adapter-contract) cover language-specific cache constraints.

## Aggressive caching is a core requirement

Fast repeated verification and fast CI are primary acceptance criteria. Cache dependency downloads, tool installation, compilation, analysis, and completed check results by default wherever their inputs can be fingerprinted. This applies to container and native checks. Cache integration and persistence belong in the pilot.

| Layer | Ordinary runs | Fresh audits |
| --- | --- | --- |
| Tool installation and dependencies | Reuse pinned downloads and setup | Reuse |
| Compilation and build artifacts | Reuse compatible incremental artifacts | Reuse |
| Analysis and test results | Reuse successful results for matching inputs | Bypass verdict reuse and execute verification |
| Check setup | Share compatible preparation among selected checks | Share preparation while resetting mutable test/service state |

Give dependency preparation, compilation, and completed verification separate cache keys derived from their own inputs. A source or test edit should invalidate dependent results while retaining compatible setup and build artifacts.

A cacheable check supplies a fingerprint of its source and test inputs, relevant dependencies, invoked scripts and shared implementation, options, environment, and toolchain. Prefer fingerprints at a meaningful check/package scope so an unrelated edit does not invalidate everything. Include uncommitted changes and deletions. Record Git revisions as provenance; a new commit alone should not invalidate unchanged check inputs.

For a repo-owned command, begin with the complete declared target source plus shared inputs, script/helper contents, arguments, and environment requirements. Narrow inputs as evidence permits. Checks against live or otherwise unbounded external state declare fresh execution; they still reuse dependency and compiler caches. Unknown input relationships broaden the fingerprint. They must not produce an unjustified cache hit.

Persist useful caches across local runs, agents, and ephemeral CI jobs using supported existing cache storage or persistent execution workers. Wire persistence for both Dagger and native checks, and verify restoration in a new worker/process. Use exact fingerprints for completed results; dependency/build caches may use compatible restore fallbacks because the underlying tools revalidate their contents. Dedicated cache infrastructure is a later decision, not a prerequisite for shared cache reuse.

Avoid duplicate work within a run: resolve checks once and give reusable preparation an identity, declared inputs/outputs, and access rules. Compatible checks share tool/dependency/build preparation; coalesce identical work where the executor supports it. Run independent checks concurrently within explicit CPU, memory, and service limits. Isolate incompatible mutable environments, build directories, and test state; serialize conflicting writers.

Reports distinguish executed checks from reused results and record the original verification time, fingerprint, source/shared provenance, environment, cache lookup/restore cost, and execution time. Restore the diagnostics and required artifacts with the result. Missing, corrupt, or incompatible entries become misses. A cached success satisfies only the same required scope and environment. Incomplete execution, tool errors, and timeouts never become passing entries.

See [cache contracts](design-notes.md#cache-contracts-and-invalidation) for invalidation and sharing details.

## Runs and freshness

The existing command names remain familiar:

```sh
./verify
./verify pre-merge
./verify main
./verify <custom-run> --dry-run
```

Recommended run policy (supported by version 1 configuration):

| Run | Recommended scope | Cache policy |
| --- | --- | --- |
| `branch` | Fast lint and core/focused tests | Aggressive reuse of setup, artifacts, and eligible results |
| `pre-merge` | Critical regression and policy checks for the final candidate | The same aggressive reuse, keyed to the actual selected inputs and scope |
| `main` | Complete applicable suite, scheduled daily by the consumer's CI | Fresh verification with dependency/build reuse |
| Custom | Consumer-defined check selection | Explicit choice of normal reuse or fresh verification |

Set freshness as an explicit run property, `rerun_checks: true`, rather than a behavior available only to the name `main`. Fresh execution bypasses the shared result cache, Dagger's cached check execution, and native test/analysis verdict caches. It preserves compatible downloads and compilation. Each adapter implements and verifies that contract. A successful fresh run may populate results for later ordinary runs; reports retain when verification actually occurred.

Start with explicit core check selections and input scopes. Cache fingerprints determine whether selected work can be reused. More advanced change-based selection determines which checks are selected and remains separate. Required core checks, task acceptance, and new/modified tests remain covered; uncertain dependencies broaden verification. Add new checks to the full run explicitly during the pilot.

Each repo's CI maps events to runs. Recommended defaults are `branch` for editing/draft updates, `pre-merge` for ready PRs and merge candidates, and a daily fresh `main` audit. A branch result cannot stand in for an unfinished pre-merge selection. Result reuse must match the actual candidate's relevant inputs, including base state when the check depends on it.

## Pilot repositories

- **Family Books first:** runnable Go services and existing CI now make the API a useful first consumer. Start with shared Go lint/vet on explicit product paths, then wrap existing tests with their Postgres setup. Include required contracts and workspace files; its private `personal/` tree stays outside Dagger inputs. Preserve existing CI gates during adoption.
- **Benchplan next:** wrap selected Swift/native verification scripts on macOS and validate the required toolchain. Preserve existing assertions. Replace disposable build caches only after measuring them and confirming compatibility; the documented headless workaround does not replace SwiftPM, simulator, or native app gates.


Keep the working Go lint path as a regression case while adding native execution. The pilot should prove the interface across real setups and show one shared improvement transferring to a second applicable consumer.

## Build order

### 1. Preserve the working Go check

Retain the current lint behavior, native diagnostics, fixture coverage, consumer support, and nonzero failure propagation while extracting the neutral runner. Keep existing commands working until a documented configuration migration is available.

**Done when:** current good/bad/broken/empty/vendor/embed regressions still behave correctly through the extracted runner.

### 2. Introduce the interface and cache contract

Add versioned target/check/environment/run configuration, a planner, common results, and internal check/executor interfaces. Give each check an explicit input/cache contract, including workspace context, execution variants, separate cache layers, and shareable preparation. Carry forward the Go caches and expose cache-hit evidence; add native command execution and caching under the same result contract. Keep planning independent of execution tools.

Add two small compatibility fixtures while establishing the interface:

- A Rust workspace with two packages and a local dependency.
- A Python package with a locked dependency setup, shared pytest fixtures, and two checks sharing compatible preparation.

Use minimal check definitions or repo-owned commands to exercise the contract; complete Rust/Python integrations remain consumer-driven.

**Done when:** an unchanged eligible check reuses its result without starting its executor, input changes invalidate the right result, and an unsupported or incomplete check cannot report success. Both fixtures prove workspace/local-dependency and test-configuration invalidation, distinct result identities for execution variants, retained compatible preparation after source edits, and fresh test execution with dependency/build reuse. A new check implementation requires no language-specific branches in the planner.

### 3. Adopt real consumer checks

First adopt shared Go checks in Family Books with explicit product inputs. Then wrap selected Benchplan scripts. Record current commands, toolchain requirements, runtime, and source/input relationships. Wrap a small useful selection of repo-owned checks and validate native toolchain requirements. Preserve native output and useful artifacts. Persist compatible Swift build products and completed results. Add ordinary `swift test` and native app gates as their prerequisites are satisfied.

**Done when:** the same wrapper runs locally and on a macOS CI worker, a real assertion/lint failure fails the job, repeated unchanged checks hit caches, changed inputs rerun affected work, and a fresh audit executes its checks despite cached passing results.

### 4. Prove fast CI and correct invalidation

Connect consumer-owned PR, merge, manual, and daily events. Restore caches across CI jobs, including on a new worker with the same declared environment, and aggregate all required check results. Benchmark cold execution, unchanged warm execution, a small source edit, an unrelated target edit, and a fresh audit. Measure startup/planning, hashing, lookup/restore, setup, compilation, and actual check execution separately.

Set per-run latency budgets from the measured consumer baseline and track median/tail latency, cache hit rate, and work avoided. An unchanged warm run should approach lookup/restore/reporting cost. Cache overhead that exceeds the saved work must be reduced. Performance is part of adoption acceptance.

**Done when:** benchmarks demonstrate reuse across processes/jobs; source/test/fixture/configuration/script/dependency/toolchain changes invalidate dependent results; unrelated target changes retain eligible hits; corrupt/missing artifacts trigger recomputation; and fresh audits retain build reuse while rerunning verification. A deliberately failing change still blocks the correct gate.

### 5. Prove that an improvement transfers

Enroll a second applicable Go target/repo and retain the Swift pilot as evidence for native execution. Improve one shared rule or execution definition, validate bad and legitimate examples, measure runtime/cache effects, then update each consumer's pin. Both repos keep their native tests and existing CI provider.

**Done when:** both consumers benefit from one maintained definition, with a reviewed version/configuration update as the adoption step. Shared changes invalidate only the checks whose effective implementation or inputs changed.

### 6. Add one bounded improvement agent

Give one scheduled job a concrete failure, slow check, or recurring mistake, plus time/cost limits. Require a reproducer, before/after evidence, performance/cache measurements, and a proposed change for review. Foreground verification gets resource priority. Changes that weaken assertions, suppress findings, or reduce required coverage need separate review; a passing retry alone does not prove a flake was repaired.

**Done when:** an agent proposes one useful repair or shared check, validates correctness and speed, and the reviewed change follows the consumer adoption path.

## Pilot completion

- Existing Go checks and native Swift checks share the same core interface and result contract; small Rust/Python fixtures validate that contract beyond the pilot languages.
- Real consumer tests and lint run consistently locally and in existing CI.
- Aggressive reuse measurably reduces repeated work across local and CI runs, with verified invalidation.
- Daily audits execute the complete configured suite fresh while retaining compatible build/dependency caches.
- Missing or failed required work blocks the correct gate, including across platforms.
- One shared improvement benefits two applicable consumers, and one bounded agent job demonstrates an improvement without weakening acceptance.

## Later, when needed

Advanced static test impact analysis, distributed sharding, a dedicated cache service, automated fleet rollout, dashboards, formal agent evaluations, LLM grading, and public plugin protocols remain optional expansions. Cache integration/persistence and bounded parallel execution are part of the pilot. Deployment orchestration, artifact promotion, and rollback management remain with existing delivery workflows.

[Design notes](design-notes.md) retain the implementation considerations for these decisions.
