# Roadmap

This is the internal pilot plan: what is planned next, and what would justify
larger investments later. It is not user-facing documentation — see
[the README](../README.md) and [docs/architecture.md](architecture.md) for
how Levenshtein works today.

## Next

Native commands, pinned-tool validation, timeouts, artifacts, and shared
preparation within a run are implemented. Local result caching, separate
preparation/build keys, artifact restoration, and fresh execution are
implemented. Cross-worker cache transport and consumer adoption below remain
planned. Rust/Python compatibility fixtures validate the shared interface;
see [fixture checks](language-fixtures.md).

The near-term plan: pilot explicit Go product checks in a Go services repo,
then wrap an existing native check in a Swift application repo, and measure
cache reuse before adding cross-worker persistence.

## Pilot repositories

- **A Go services repo first:** runnable Go services and existing CI make its
  API a useful first consumer. Start with shared Go lint/vet on explicit
  product paths, then wrap existing tests with their database setup. Include
  required contracts and workspace files; private, non-product trees stay
  outside Dagger inputs. Preserve existing CI gates during adoption.
- **A Swift application repo next:** wrap selected Swift/native verification
  scripts on macOS and validate the required toolchain. Preserve existing
  assertions. Replace disposable build caches only after measuring them and
  confirming compatibility; the documented headless workaround does not
  replace SwiftPM, simulator, or native app gates.

Keep the working Go lint path as a regression case while adding native
execution. The pilot should prove the interface across real setups and show
one shared improvement transferring to a second applicable consumer.

## Build order

### 1. Preserve the working Go check

Retain the current lint behavior, native diagnostics, fixture coverage,
consumer support, and nonzero failure propagation while extracting the
neutral runner. Keep existing commands working until a documented
configuration migration is available.

**Done when:** current good/bad/broken/empty/vendor/embed regressions still
behave correctly through the extracted runner.

### 2. Introduce the interface and cache contract

Add versioned target/check/environment/run configuration, a planner, common
results, and internal check/executor interfaces. Give each check an explicit
input/cache contract, including workspace context, execution variants,
separate cache layers, and shareable preparation. Carry forward the Go caches
and expose cache-hit evidence; add native command execution and caching under
the same result contract. Keep planning independent of execution tools.

Add two small compatibility fixtures while establishing the interface:

- A Rust workspace with two packages and a local dependency.
- A Python package with a locked dependency setup, shared pytest fixtures,
  and two checks sharing compatible preparation.

Use minimal check definitions or repo-owned commands to exercise the
contract; complete Rust/Python integrations remain consumer-driven.

**Done when:** an unchanged eligible check reuses its result without starting
its executor, input changes invalidate the right result, and an unsupported
or incomplete check cannot report success. Both fixtures prove
workspace/local-dependency and test-configuration invalidation, distinct
result identities for execution variants, retained compatible preparation
after source edits, and fresh test execution with dependency/build reuse. A
new check implementation requires no language-specific branches in the
planner.

### 3. Adopt real consumer checks

First adopt shared Go checks in a Go services repo with explicit product
inputs. Then wrap selected checks from a Swift application repo. Record
current commands, toolchain requirements, runtime, and source/input
relationships. Wrap a small useful selection of repo-owned checks and
validate native toolchain requirements. Preserve native output and useful
artifacts. Persist compatible Swift build products and completed results.
Add ordinary `swift test` and native app gates as their prerequisites are
satisfied.

**Done when:** the same wrapper runs locally and on a macOS CI worker, a real
assertion/lint failure fails the job, repeated unchanged checks hit caches,
changed inputs rerun affected work, and a fresh audit executes its checks
despite cached passing results.

### 4. Prove fast CI and correct invalidation

Connect consumer-owned PR, merge, manual, and daily events. Restore caches
across CI jobs, including on a new worker with the same declared environment,
and aggregate all required check results. Benchmark cold execution, unchanged
warm execution, a small source edit, an unrelated target edit, and a fresh
audit. Measure startup/planning, hashing, lookup/restore, setup, compilation,
and actual check execution separately.

Set per-run latency budgets from the measured consumer baseline and track
median/tail latency, cache hit rate, and work avoided. An unchanged warm run
should approach lookup/restore/reporting cost. Cache overhead that exceeds
the saved work must be reduced. Performance is part of adoption acceptance.

**Done when:** benchmarks demonstrate reuse across processes/jobs;
source/test/fixture/configuration/script/dependency/toolchain changes
invalidate dependent results; unrelated target changes retain eligible hits;
corrupt/missing artifacts trigger recomputation; and fresh audits retain
build reuse while rerunning verification. A deliberately failing change
still blocks the correct gate.

### 5. Prove that an improvement transfers

Enroll a second applicable Go target/repo and retain the Swift pilot as
evidence for native execution. Improve one shared rule or execution
definition, validate bad and legitimate examples, measure runtime/cache
effects, then update each consumer's pin. Both repos keep their native tests
and existing CI provider.

**Done when:** both consumers benefit from one maintained definition, with a
reviewed version/configuration update as the adoption step. Shared changes
invalidate only the checks whose effective implementation or inputs changed.

### 6. Add one bounded improvement agent

Give one scheduled job a concrete failure, slow check, or recurring mistake,
plus time/cost limits. Require a reproducer, before/after evidence,
performance/cache measurements, and a proposed change for review. Foreground
verification gets resource priority. Changes that weaken assertions,
suppress findings, or reduce required coverage need separate review; a
passing retry alone does not prove a flake was repaired.

**Done when:** an agent proposes one useful repair or shared check, validates
correctness and speed, and the reviewed change follows the consumer adoption
path.

## Pilot completion

- Existing Go checks and native Swift checks share the same core interface
  and result contract; small Rust/Python fixtures validate that contract
  beyond the pilot languages.
- Real consumer tests and lint run consistently locally and in existing CI.
- Aggressive reuse measurably reduces repeated work across local and CI runs,
  with verified invalidation.
- Daily audits execute the complete configured suite fresh while retaining
  compatible build/dependency caches.
- Missing or failed required work blocks the correct gate, including across
  platforms.
- One shared improvement benefits two applicable consumers, and one bounded
  agent job demonstrates an improvement without weakening acceptance.

## Consumer pilot acceptance

Start with a Go services repo's API using explicit product inputs, then wrap
a small check from a Swift application repo on its existing macOS worker.
Keep database tests and simulator checks in their existing jobs until their
wrappers are verified.

For each pilot, record cold, unchanged warm, source-edit, unrelated-file-edit,
and fresh-audit timings. Introduce one deliberate lint/test failure to prove
the existing CI gate receives a nonzero exit. Confirm a fresh run executes
checks while keeping compatible build caches. Start native commands without
result caching; enable it only with complete inputs, a provisioned
environment identity, and an explicit `rerun_command`.

Local caches work today. Cross-worker cache transport and multi-job result
aggregation are not implemented; keep required platform jobs individually
required. Do not share writable result caches with untrusted PRs.

## Later, when needed

Advanced static test impact analysis, distributed sharding, a dedicated cache
service, automated fleet rollout, dashboards, formal agent evaluations, LLM
grading, and public plugin protocols remain optional expansions. Cache
integration/persistence and bounded parallel execution are part of the pilot.
Deployment orchestration, artifact promotion, and rollback management remain
with existing delivery workflows.

## What would justify expansion?

| Area | Starting point | Revisit when |
| --- | --- | --- |
| Test impact analysis (TIA) | Explicit core checks and path/package mappings | Broad fallback checks consume a material part of the feedback cycle |
| Distributed execution | Bounded local parallelism, shared preparation, and aggressive caching | Measurements show execution remains slow after reuse and duplication are addressed |
| Dedicated cache service | Persist dependencies, build artifacts, and eligible results through existing backends | Measured storage, latency, or sharing limits justify operating another service |
| Automated rollout | Manual pinned-version updates | Adopting improvements across repos becomes repetitive or inconsistent |
| Portfolio reporting | CI logs and small result files | Questions about failures, adoption, or cost become difficult to answer |
| Additional language integrations | Existing Go checks and native commands for a Swift application repo through one core interface | Repeated setup warrants a shared language check; public plugins need actual external consumers |
| Formal requirements and evaluations | Issues, acceptance criteria, regression fixtures | Ordinary checks cannot answer a concrete quality question |
| LLM-graded checks | Human judgment for subjective requirements | A useful rubric can be calibrated against human decisions |
| Dedicated reproduction tooling | Rerun command, revision, and tool versions | Missing environments or retained inputs regularly prevent diagnosis |
| Deployment management | Existing delivery workflows and check commands | A pilot exposes a specific release-safety or coordination problem |

A hosted dashboard, custom worker scheduler, marketplace, and autonomous
production deployment have no pilot requirement.

## If more precise selection becomes necessary

Start with native language/project information. For Go, package/import
relationships must include test dependencies, build tags, and declared
non-code inputs. Compare old and new graph information to account for
deletions and changed edges. Migrations, schemas, fixtures, configuration,
toolchains, and the harness itself can affect tests.

Static checks have different scopes. Formatting can be file-local, typed
analysis may need packages and dependents, and architectural rules may need
the full graph. Filtering diagnostics to changed lines does not establish
complete analysis.

Preserve the declared core and task-specific acceptance checks. Unknown
dependencies broaden execution to the relevant suite; repository-wide
uncertainty can require a complete run. One resolved selection should own the
final set so overlapping selectors cannot accidentally omit checks.

Before trusting a new selector, compare its proposed selection against
complete execution at the same source/base pair. Use daily audits, known
regression fixtures, and explicitly budgeted historical or PR samples. A
later default-branch audit alone cannot establish whether an earlier PR
selection was sound. Track misses; observed zero misses does not prove
safety.

If sharding is justified, use timing history while respecting fixture
affinity and CPU, memory, and database limits. Keep queue, setup, build,
check execution, and reporting timings separate to identify the real
bottleneck.

Keep selection and caching separate: select required work first, then reuse
matching results or execute misses. Full audits, flake investigations, and
live release smoke checks need fresh verification while retaining compatible
setup/build reuse, as defined in the [cache contracts](design-notes.md#cache-contracts-and-invalidation).
Verify persisted cache behavior in the actual CI environment.

## If maintenance and adoption need more automation

Start each proposed rule with a recurring defect or agreed convention,
known-bad and legitimate fixtures, clear repair guidance, and measured cost.
Uncertain rules can begin as advisory. Prefer existing analyzers before
custom implementation.

Application-specific tests need explicit repository hooks before they can be
generalized. A configuration convention can transfer directly; a behavioral
contract transfers only when consumers expose the required operation and
observations.

When rollout becomes a bottleneck, add upgrade PRs and adoption tracking
around immutable versions. Test changes in one repo before expanding. Keep
private repository inventory and application-specific examples separate from
reusable code.

Keep every attempt when investigating flaky behavior. Retries are evidence,
not repair. If quarantine becomes necessary, give it an owner, expiry, and
continued observation. Existing lint baselines, if needed, must be explicitly
reviewed rather than automatically regenerated to produce a pass.

A larger maintenance service may need deduplication, bounded concurrency, and
proposal tracking. Foreground verification should have priority. Keep
untrusted check execution separate from trusted write credentials;
maintenance jobs do not need production deployment authority.

## If richer evidence or agent evaluations become useful

Preserve native diagnostics as the authoritative detail and normalize only
fields consumers actually use. Add durable storage or an index when
retention and query needs justify them.

Useful portfolio questions include: which shared version each repo uses,
which failures recur, which daily audits are overdue, and which checks cost
more than the value they provide. Do not build a dashboard before those
questions become operationally useful.

Formal requirement IDs and linked evaluations can follow demonstrated needs.
A future agent evaluation set should include representative mistakes,
legitimate changes, and attempted weakening of acceptance checks. Measure
defect detection, incorrect findings, accepted-change time, and substantive
review corrections. Test counts and coverage alone do not establish quality.

Model-graded checks need a versioned rubric, recorded evaluator inputs, and
calibration against human judgments. Keep them advisory until their
reliability supports a stronger role.

## If release management becomes part of the product

Reuse the existing delivery workflow first. A later shared protocol could
verify a candidate artifact, record its digest, and promote that exact
artifact across environments. Keep provider-specific deploy, smoke, and
rollback commands in the consuming repo.

Retain existing release approvals and environment-scoped credentials.
Serialize changes to the same deployment target. Smoke checks need fresh
observations of the artifact actually deployed.

Application rollback and database recovery are different operations. Plan
compatibility explicitly; reverting an image does not reverse a destructive
migration. Cross-service checks must use deployed or explicitly proposed
versions.

## Implementation references

Consult current documentation and validate compatibility when an expansion
is actually selected:

- [GitHub reusable workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/reusing-workflow-configurations)
- [Dagger services](https://docs.dagger.io/using/services/) and [function caching](https://docs.dagger.io/extending/function-caching/)
- [Go analysis interfaces](https://pkg.go.dev/golang.org/x/tools/go/analysis) and [actionlint](https://github.com/rhysd/actionlint)
- [Renovate shared presets](https://docs.renovatebot.com/config-presets/)
