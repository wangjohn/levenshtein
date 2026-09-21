# Design notes

Status: describes the current language-independent interface and cache
contracts. See [architecture](architecture.md) for how the CLI implements
them, and [the roadmap](roadmap.md) for forward-looking expansion.

## Keep the pilot mechanics simple

- A shared “pack” can be a folder of native tool configuration, commands, and fixtures pinned to a commit. No registry, plugin protocol, or release-management service is needed.
- Keep targets, checks, environments, and runs independent of language. Give each check a stable name and versioned implementation. A check may invoke a native suite; preserve its output and artifacts without requiring every test framework to use one diagnostic format.
- Keep named runs in repo configuration and triggers in CI workflows. Shared defaults need only enough extension to add a custom run; avoid designing a general workflow language.
- Preserve native output. A small result record needs the run, source revision, shared version, selected checks, outcomes, durations, and rerun commands. Mark local uncommitted changes explicitly; do not claim a commit alone reproduces them.
- Distinguish assertion failures from infrastructure errors, cancellation, and incomplete execution. Account for every selected check before accepting a run.
- Pin compatible tool versions, isolate mutable service/test state, and aggressively reuse tools, dependencies, build artifacts, and eligible check results. Correct cold execution and fast warm execution are both required. Cheap lint should not wait for database setup.
- Bind merge results to the final candidate and relevant inputs. Validate actual CI failure propagation with a deliberately failing pilot change; inspecting workflow configuration alone is insufficient.

Keep Dagger/native execution details behind internal executor interfaces and language-specific behavior inside shared check definitions. Version the repo configuration and result schema; preserve the current command entry point while migrating the Go-only configuration. Public plugin protocols and a general workflow language can wait.

## Language adapter contract

An adapter is an internal check implementation using the common planner, executors, and results. Keep the target/check/environment/run model; do not assign a single language to an entire repo. Each adapter defines:

- **Workspace inputs:** distinguish the execution directory from the workspace root; include relevant root configuration, local dependencies, shared fixtures, generated inputs, and test configuration.
- **Execution variants:** include selected packages/tests, toolchains, dependency selection, and language-specific options in the check identity and reported scope. Record resolved versions, not just a requested version range.
- **Shared preparation:** declare preparation inputs, reusable outputs, compatibility, and read/write access. Lint, type checking, and tests can share compatible setup. Isolate environments with different dependency selections and serialize conflicting mutations; a generic command may keep setup embedded until it can declare a safe reusable boundary.
- **Results and freshness:** preserve native output, collect required artifacts, and define how fresh verification bypasses verdict reuse while retaining compatible setup/build artifacts.

### Rust and Python constraints

- **Rust workspaces:** Cargo shares a root lockfile and build directory. Include workspace configuration and relevant path dependencies even when checking one member. Features, profiles, selected packages, and target platforms identify different verification scopes. See [Cargo workspaces](https://doc.rust-lang.org/cargo/reference/workspaces.html) and [test options](https://doc.rust-lang.org/cargo/commands/cargo-test.html).
- **Rust build inputs:** account for `build.rs`, generated sources, environment inputs, native libraries, and required system tools. Declaring only Rust source files is insufficient; see [Cargo build scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html).
- **Python environments:** pin the interpreter and resolved dependencies; include groups/extras, test plugins, configuration, and `conftest.py`/shared fixtures. A uv recipe is one option, not a requirement for every consumer. Reuse compatible preparation across checks; see [uv synchronization](https://docs.astral.sh/uv/concepts/projects/sync/).
- **Python cache portability:** reuse compatible downloaded/built packages across workers. Reuse a complete virtual environment only when interpreter, platform, dependency selection, and filesystem layout match; otherwise recreate it from cached dependencies. Virtual environments contain absolute interpreter paths and are generally nonportable. Local package builds and dynamic metadata need their actual inputs reflected in underlying tool caches too. See [Python virtual environments](https://docs.python.org/3/library/venv.html) and [uv cache inputs](https://docs.astral.sh/uv/concepts/cache/).
- **Python fresh runs:** pytest's cache records state such as previous failures, not reusable passing-suite verdicts. A fresh full-suite audit must execute the configured suite; `--last-failed` alone cannot satisfy it. Dependency caches can remain warm. See [pytest cache behavior](https://docs.pytest.org/en/stable/how-to/cache.html).

Use the small Rust/Python fixtures described in [language fixtures](language-fixtures.md) to validate these boundaries before expanding shared language recipes.

## Cache contracts and invalidation

Default ordinary runs to aggressive reuse, including successful check results. A reusable check implementation must define its input fingerprint, reusable outputs, environment requirements, and fresh-execution behavior. Native command checks participate in the same contract as container checks.

### Separate cache layers

Each layer has its own key and compatibility rules. Do not key all preparation on the complete check fingerprint.

| Layer | Inputs that determine reuse |
| --- | --- |
| Tool/dependency preparation | Resolved toolchain, platform, manifests/lockfiles, dependency groups/extras, installer configuration, preparation implementation, and any local package/build inputs it consumes |
| Compilation/build artifacts | Relevant source and dependencies, compiler/system tools, platform, build flags/profile/features, build scripts, and generated inputs |
| Completed verification | Complete relevant source/test/configuration inputs, resolved environment, check implementation/options, and selected scope |

For example, editing a Python test should invalidate its verification result while retaining an unchanged dependency environment. Rust source edits invalidate dependent results while Cargo reuses compatible build artifacts. Include source in preparation keys when preparation consumes it; local package builds are not determined by lockfiles alone. A check may omit layers it does not need.

### What identifies reusable work

An exact result key includes the check/target identity and input scope, effective command and options, source and test content, fixtures and configuration, dependency manifests/lockfiles and relevant local dependencies, invoked scripts and helpers, effective shared check/executor implementation, and environment/toolchain identity. Include platform/architecture, container image digest or native SDK/compiler identity, build flags, relevant declared environment values, and any base revision/diff that the check actually consumes. Never put raw credentials in cache metadata; credential-dependent results require an appropriate version/state identity or fresh execution.

Use content fingerprints so unchanged work can be reused across commits. Preserve the source and pinned shared revisions as provenance. Hash relevant shared implementations and their dependencies rather than invalidating every check for an unrelated documentation or recipe change. Input discovery must account for untracked inputs, deletions, generated sources, workspace replacements, and shared contracts.

For generic repo-owned commands, start with the full declared source scope, scripts, arguments, and environment. Broaden uncertain dependency scopes, then narrow them with evidence. A tool that reads live services or other unbounded state declares fresh execution unless that state has a dependable identity. It still benefits from aggressive tool, dependency, and compilation reuse.

### Reuse across processes and workers

- Restore only exact completed-result keys. Prefix/fallback restoration is for dependency/build caches whose tools validate compatibility before reuse.
- Preserve result output and required artifacts together. A missing or invalid artifact turns the lookup into a miss; use atomic publication after successful completion. Record original execution time and cache provenance rather than presenting a reused result as freshly executed.
- Persist caches across agents and CI jobs using supported existing storage or persistent workers. Scope mutable outputs by environment/check/worktree and serialize conflicting writers; share immutable artifacts where compatible. Reuse preparation and coalesce identical work where supported.
- Keep trusted result publication separate from untrusted pull-request writes. Shared dependency/artifact reuse must respect repository and access boundaries. Cache backend failure should fall back to recomputation when execution is available.
- Start executors lazily. A complete result hit should avoid container startup, tool installation, and check execution. Missing required work without a suitable environment leaves the run incomplete.

### Freshness and evidence

A fresh run bypasses completed-result reuse at every verification layer, including Dagger execution and native test/analysis caches. Compatible tool installations, downloads, compiled libraries, and test binaries remain reusable. Each check adapter must prove that its fresh mode actually executes verification; a new outer cache key alone is insufficient. Freshly executed successes may populate later ordinary-run caches.

Validate both hits and misses: unchanged work hits; relevant source, test, fixture, dependency, script, configuration, toolchain, and shared implementation changes miss; unrelated target changes retain valid hits. Exercise persisted caches from another process/worker, incomplete/corrupt entries, and a fresh audit after a cached success.

Measure cold, warm, small-edit, and fresh-audit runs. Separate hashing/lookup/restore, environment startup, setup, compilation, and check execution; record cache hit rate, work avoided, and median/tail latency. If restoring an artifact costs more than recomputing it, adjust cache granularity or retention. Aggressive caching must reduce end-to-end latency.
