# Design notes

Status: updated for the language-independent interface and aggressive caching plan.

The [implementation plan](implementation.md) defines the pilot. The interface and cache contracts below support that plan. Sections describing later expansion remain conditional; they are not a checklist for V1.

## Keep the pilot mechanics simple

- A shared “pack” can be a folder of native tool configuration, commands, and fixtures pinned to a commit. No registry, plugin protocol, or release-management service is needed.
- Keep targets, checks, environments, and runs independent of language. Give each check a stable name and versioned implementation. A check may invoke a native suite; preserve its output and artifacts without requiring every test framework to use one diagnostic format.
- Keep named runs in repo configuration and triggers in CI workflows. Shared defaults need only enough extension to add a custom run; avoid designing a general workflow language.
- Preserve native output. A small result record needs the run, source revision, shared version, selected checks, outcomes, durations, and rerun commands. Mark local uncommitted changes explicitly; do not claim a commit alone reproduces them.
- Distinguish assertion failures from infrastructure errors, cancellation, and incomplete execution. Account for every selected check before accepting a run.
- Pin compatible tool versions, isolate mutable service/test state, and aggressively reuse tools, dependencies, build artifacts, and eligible check results. Correct cold execution and fast warm execution are both required. Cheap lint should not wait for database setup.
- Bind merge results to the final candidate and relevant inputs. Validate actual CI failure propagation with a deliberately failing pilot change; inspecting workflow configuration alone is insufficient.

Keep Dagger/native execution details behind internal executor interfaces and language-specific behavior inside shared check definitions. Version the repo configuration and result schema; preserve the current command entry point while migrating the Go-only configuration. Public plugin protocols and a general workflow language can wait.

## Cache contracts and invalidation

Default ordinary runs to aggressive reuse, including successful check results. A reusable check implementation must define its input fingerprint, reusable outputs, environment requirements, and fresh-execution behavior. Native command checks participate in the same contract as container checks.

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

## What would justify expansion?

| Area | Starting point | Revisit when |
| --- | --- | --- |
| Test impact analysis (TIA) | Explicit core checks and path/package mappings | Broad fallback checks consume a material part of the feedback cycle |
| Distributed execution | Bounded local parallelism, shared preparation, and aggressive caching | Measurements show execution remains slow after reuse and duplication are addressed |
| Dedicated cache service | Persist dependencies, build artifacts, and eligible results through existing backends | Measured storage, latency, or sharing limits justify operating another service |
| Automated rollout | Manual pinned-version updates | Adopting improvements across repos becomes repetitive or inconsistent |
| Portfolio reporting | CI logs and small result files | Questions about failures, adoption, or cost become difficult to answer |
| Additional language integrations | Existing Go checks and native commands for Benchplan through one core interface | Repeated setup warrants a shared language check; public plugins need actual external consumers |
| Formal requirements and evaluations | Issues, acceptance criteria, regression fixtures | Ordinary checks cannot answer a concrete quality question |
| LLM-graded checks | Human judgment for subjective requirements | A useful rubric can be calibrated against human decisions |
| Dedicated reproduction tooling | Rerun command, revision, and tool versions | Missing environments or retained inputs regularly prevent diagnosis |
| Deployment management | Existing delivery workflows and check commands | A pilot exposes a specific release-safety or coordination problem |

A hosted dashboard, custom worker scheduler, marketplace, and autonomous production deployment have no pilot requirement.

## If more precise selection becomes necessary

Start with native language/project information. For Go, package/import relationships must include test dependencies, build tags, and declared non-code inputs. Compare old and new graph information to account for deletions and changed edges. Migrations, schemas, fixtures, configuration, toolchains, and the harness itself can affect tests.

Static checks have different scopes. Formatting can be file-local, typed analysis may need packages and dependents, and architectural rules may need the full graph. Filtering diagnostics to changed lines does not establish complete analysis.

Preserve the declared core and task-specific acceptance checks. Unknown dependencies broaden execution to the relevant suite; repository-wide uncertainty can require a complete run. One resolved selection should own the final set so overlapping selectors cannot accidentally omit checks.

Before trusting a new selector, compare its proposed selection against complete execution at the same source/base pair. Use daily audits, known regression fixtures, and explicitly budgeted historical or PR samples. A later default-branch audit alone cannot establish whether an earlier PR selection was sound. Track misses; observed zero misses does not prove safety.

If sharding is justified, use timing history while respecting fixture affinity and CPU, memory, and database limits. Keep queue, setup, build, check execution, and reporting timings separate to identify the real bottleneck.

Keep selection and caching separate: select required work first, then reuse matching results or execute misses. Full audits, flake investigations, and live release smoke checks need fresh verification while retaining compatible setup/build reuse, as defined in the cache contract above. Verify persisted cache behavior in the actual CI environment.

## If maintenance and adoption need more automation

Start each proposed rule with a recurring defect or agreed convention, known-bad and legitimate fixtures, clear repair guidance, and measured cost. Uncertain rules can begin as advisory. Prefer existing analyzers before custom implementation.

Application-specific tests need explicit repository hooks before they can be generalized. A configuration convention can transfer directly; a behavioral contract transfers only when consumers expose the required operation and observations.

When rollout becomes a bottleneck, add upgrade PRs and adoption tracking around immutable versions. Test changes in one repo before expanding. Keep private repository inventory and application-specific examples separate from reusable code.

Keep every attempt when investigating flaky behavior. Retries are evidence, not repair. If quarantine becomes necessary, give it an owner, expiry, and continued observation. Existing lint baselines, if needed, must be explicitly reviewed rather than automatically regenerated to produce a pass.

A larger maintenance service may need deduplication, bounded concurrency, and proposal tracking. Foreground verification should have priority. Keep untrusted check execution separate from trusted write credentials; maintenance jobs do not need production deployment authority.

## If richer evidence or agent evaluations become useful

Preserve native diagnostics as the authoritative detail and normalize only fields consumers actually use. Add durable storage or an index when retention and query needs justify them.

Useful portfolio questions include: which shared version each repo uses, which failures recur, which daily audits are overdue, and which checks cost more than the value they provide. Do not build a dashboard before those questions become operationally useful.

Formal requirement IDs and linked evaluations can follow demonstrated needs. A future agent evaluation set should include representative mistakes, legitimate changes, and attempted weakening of acceptance checks. Measure defect detection, incorrect findings, accepted-change time, and substantive review corrections. Test counts and coverage alone do not establish quality.

Model-graded checks need a versioned rubric, recorded evaluator inputs, and calibration against human judgments. Keep them advisory until their reliability supports a stronger role.

## If release management becomes part of the product

Reuse the existing delivery workflow first. A later shared protocol could verify a candidate artifact, record its digest, and promote that exact artifact across environments. Keep provider-specific deploy, smoke, and rollback commands in the consuming repo.

Retain existing release approvals and environment-scoped credentials. Serialize changes to the same deployment target. Smoke checks need fresh observations of the artifact actually deployed.

Application rollback and database recovery are different operations. Plan compatibility explicitly; reverting an image does not reverse a destructive migration. Cross-service checks must use deployed or explicitly proposed versions.

## Implementation references

Consult current documentation and validate compatibility when an expansion is actually selected:

- [GitHub reusable workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations)
- [Dagger services](https://docs.dagger.io/using/services/) and [function caching](https://docs.dagger.io/extending/function-caching/)
- [Go analysis interfaces](https://pkg.go.dev/golang.org/x/tools/go/analysis) and [actionlint](https://github.com/rhysd/actionlint)
- [Renovate shared presets](https://docs.renovatebot.com/config-presets/)
