# Public roadmap

Levenshtein focuses on repeatable repository checks with shared, pinned policy.
The current priorities are reliability of native and Dagger results, clear
installation and failure diagnostics, and evidence from real consumer
repositories. This is a direction for contributions, not a release schedule.

## Priorities

- Make results trustworthy: regressions for parser boundaries, cache isolation,
  canceled checks, and native/container behavior differences.
- Make adoption easier: reproducible setup examples, actionable errors when a
  required tool or download is unavailable, and clear upgrade expectations.
- Measure useful policy: small, reproducible cases demonstrating bugs caught,
  false alarms, or unnecessary work on a consumer's second run. New default
  rules need the [rule selection evidence](rule-selection.md).
- Keep community rules usable: test the example module and document failures
  rule authors can reproduce without expanding the core rule set.

Windows support, new language integrations, hosted services, and additional
semantic-lint features are deferred while these priorities are addressed.

## Starter tasks

These are task shapes, not promises that an unclaimed issue exists. Check
[open issues](https://github.com/wangjohn/levenshtein/issues) and describe the
case you want to work on before a substantial change.

| Task | A useful finished contribution |
| --- | --- |
| Improve one setup/error example | Reproduce it in a temporary consumer, correct the relevant doc, and run the root documentation tests. |
| Add an existing rule regression | Add a failing and a similar passing fixture to `runner/lint/policy/testdata` or the appropriate analyzer test; run lint module tests. |
| Explain a community builder failure | Add a bounded regression in `runner/community/internal/build` and update community rule guidance; run community and example tests. |
| Measure a repeated verification | Record pinned revision, toolchain, consumer scope, cold/warm conditions and commands so another contributor can repeat the observation. Avoid general speed claims from one fixture. |

[Contributing](../CONTRIBUTING.md) provides the test tiers and PR expectations.
[@wangjohn](https://github.com/wangjohn) maintains and reviews the project;
review and issue responses are best effort, with no promised turnaround.
Use [private security reporting](../SECURITY.md) for vulnerabilities. Public
issues are appropriate for reproducible bugs, proposals and documentation gaps.
