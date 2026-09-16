# Optional design notes

Status: reference ideas retained from the broader proposal, September 15, 2026.

The [implementation plan](implementation.md) defines the pilot. These notes preserve useful implementation details and possible expansions. Future features are conditional, not a roadmap or a checklist for V1.

## Keep the pilot mechanics simple

- A shared “pack” can be a folder of native tool configuration, commands, and fixtures pinned to a commit. No registry, plugin protocol, or release-management service is needed.
- Give each runnable check a stable name. A check may invoke a native suite; normalizing every individual test or diagnostic is unnecessary.
- Keep named runs in repo configuration and triggers in CI workflows. Shared defaults need only enough extension to add a custom run; avoid designing a general workflow language.
- Preserve native output. A small result record needs the run, source revision, shared version, selected checks, outcomes, durations, and rerun commands. Mark local uncommitted changes explicitly; do not claim a commit alone reproduces them.
- Distinguish assertion failures from infrastructure errors, cancellation, and incomplete execution. Account for every selected check before accepting a run.
- Pin compatible tool versions, isolate service state, and keep execution correct with an empty cache. Cheap lint should not wait for database setup.
- Bind merge results to the final candidate and relevant inputs. Validate actual CI failure propagation with a deliberately failing pilot change; inspecting workflow configuration alone is insufficient.

Exact schemas, directory structures, and adapter interfaces can follow the first working implementation.

## What would justify expansion?

| Area | Starting point | Revisit when |
| --- | --- | --- |
| Test impact analysis (TIA) | Explicit core checks and path/package mappings | Broad fallback checks consume a material part of the feedback cycle |
| Distributed execution | Native parallelism and ordinary build caching | Measurements show execution remains slow after setup and duplication are fixed |
| Shared cache | Existing runner caches | Cold setup/build costs justify operating another service |
| Automated rollout | Manual pinned-version updates | Adopting improvements across repos becomes repetitive or inconsistent |
| Portfolio reporting | CI logs and small result files | Questions about failures, adoption, or cost become difficult to answer |
| More languages and plugins | One language plus native commands | A real second consumer needs deeper integration |
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

Distinguish build reuse from successful-verdict reuse. Full audits, flake investigations, and release smoke checks need fresh execution through every relevant cache layer, including the native test runner. Verify shared-cache behavior in the actual CI environment before relying on it.

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
