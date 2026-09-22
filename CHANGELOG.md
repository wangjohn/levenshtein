# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Consumers pin a release tag, or its commit SHA, as described in
[docs/releases.md](docs/releases.md).

## [Unreleased]

### Added

- `go-mod`, a shared check on both executors that runs `go mod tidy -diff` and
  `go mod verify` in the target module. Untidy manifests fail with tidy's diff,
  and a download that no longer matches its recorded hash or `go.sum` fails too;
  an unreachable module proxy is an error, never a pass. It runs with
  `GOWORK=off`, needs the module proxy even for a vendored module, and its
  result is never cached; like `go-vuln`, a direct Dagger `sharedCheck` call
  must pass a unique `nonce` ([details](docs/checks.md#module-manifests)).
- `workflow-security`, a shared check on both executors that runs zizmor 1.30.1's
  offline audits over a repository's workflows, composite actions, and
  Dependabot configuration and fails on findings of medium severity and above,
  keeping zizmor's report. It honors one root zizmor configuration file,
  requires a repository-root target, and is not in any default gate: add it to
  a run of your own. The zizmor release archive is pinned by SHA-256 per
  platform and verified before it runs; audits that query GitHub stay with
  zizmor's own action ([details](docs/checks.md#workflow-security)).
- `go-test`, a shared check on both executors that runs `go test -race ./...`
  on the pinned toolchain with cgo on. A failing test, a panic, a test that
  hits the ten-minute per-package timeout, or a data race the race detector
  reports is a finding with `go test`'s own output; a package that does not
  build or set up, a module with no tests, and a host without a C compiler are
  errors. It reads `go test -json` to tell them apart, leaves vet to `go-vet`,
  reuses its result like `go-vet` does, and is not in any default gate: add it
  to a run of your own, and keep tests that need services in a `command` check
  ([details](docs/checks.md#tests)).

### Changed

- **Repositories without a `levenshtein.json` now run `go-mod` in their
  default `branch`, `pre-merge`, and `main` runs**, next to `go-lint` and
  `go-vet`. An untidy root module, or one whose dependencies the container
  cannot download (such as private modules reached only through `vendor/`),
  starts failing those runs when the pin is bumped. Tidy the module, or add a
  `levenshtein.json` whose runs leave `go-mod` out.
- Levenshtein's own CI checks its module manifests through `go-mod` in the
  `lint` job's `branch` run instead of two separate workflow steps.
- Levenshtein's own `branch`, `pre-merge`, `branch-dagger`, and `main` runs
  include `workflow-security`. `security.yml` keeps zizmor's GitHub Action for
  the online audits and now names the same inputs as the shared check.
- Levenshtein's own `levenshtein.json` has a native `go-test` run over the
  repository and `runner/lint` for local use. It is not part of `branch`,
  `pre-merge`, or `main`, because CI's `tests` job already runs
  `go test -race` over the same modules.

## [0.1.0] - 2026-09-22

The first release: everything merged into `main` so far, grouped by what it
gave a consumer.

### Added

- Shared Go checks that run pinned tools in Dagger: `go-lint` (Staticcheck
  `SA*`, errcheck, exhaustive), `go-vet`, `go-http`, `go-sql`, `go-vuln`,
  `workflow-lint`, and Levenshtein's own `self-test` (#1, #8).
- House lint rules LV1001 (typed choices for enum-like strings) and LV1002
  (construct value records together), including enum-like usage detection and
  removal of the opt-in record marker (#7, #9).
- A standalone Go CLI and the `./verify` launcher, with version 1
  target/environment/check/run configuration, named runs, `--dry-run` planning
  that starts no executor, and a versioned JSON report (#2, #3).
- Native `command` checks: pinned tool validation, minimal inherited
  environment, timeouts that kill the process group, artifacts, and shared
  preparation and build stages (#4).
- Local caching of completed results, preparation, and native builds, with
  content fingerprints, atomic publication, per-key locking, and fresh runs
  through `rerun_checks` (#5).
- Rust and Python fixture consumers that exercise the workspace, preparation,
  invalidation, and freshness contracts without language-specific planner code
  (#6).
- Consumer adoption support: declared Dagger source boundaries, complete
  GoReleaser archives, and an extracted-archive smoke test (#10).
- An advisory `semantic-lint` check that asks a pinned TypeSafe Jev model
  bounded questions about a change and records every judgment (#12).
- Repository baseline: license, contributing guide, security policy, and
  Dependabot (#13).
- Multi-target checks: one declaration with `targets` expands to a planned
  check per target (#27).
- Native execution of `go-lint`, `go-vet`, `workflow-lint`, and `go-vuln` on
  the host Go, with no container runtime (#31).
- A GitHub Action at the repository root, `uses: wangjohn/levenshtein@v0.1.0`,
  that picks a run from the event, sets up the pinned Go, and caches helper
  builds, analysis, and results across workers.
- Tagged releases: a `vX.Y.Z` tag publishes platform archives with checksums,
  SBOMs, and build provenance (#30).
- House rule LV1006 reports tests that cannot fail, including tests skipped
  unconditionally (#35).
- `go-mutation`: diff-scoped mutation testing with pinned gremlins, failing when
  a covered mutant survives, with an accepted-survivors file for known
  exceptions (#37).

### Changed

- `go-lint` enforces the whole pinned Staticcheck release minus six naming and
  documentation style rules, nineteen curated upstream analyzers, five
  `modernize` analyzers, and house rules LV1003 through LV1005 (#29, #33).
- The `./verify` launcher accepts any host Go and provisions the pinned
  toolchain itself (#27).
- Fingerprinting hashes each file once per process, persists a stat cache
  between runs, and discovers inputs from the Git work tree by default (#28).
- Split `Check` into per-kind option objects and routed native kinds through
  one registry, so a check carries only the options its kind accepts (#17,
  #19, #25).
- Simplified the verification core: legacy configuration dropped, `Result`
  helpers collapsed, the shared fingerprint memoized, one stage store (#14).
- Separated product documentation from the pilot roadmap (#15).
- Split CI into `lint` and `tests` jobs with per-job result caches, prefix
  restore keys, composite actions for the Dagger CLI and generated SDK, and
  single-sourced pins (#11, #18, #26).
- Added formatting, module tidiness and checksum, Python lint, and coverage
  gates to CI, and an Actions security scan workflow (#30).

### Security

- Hardened `semantic-lint` credentials: the API key and API origin are read
  from the host only and rejected in committed configuration, diff prefixes
  are pinned, and request state is fitted to the model's budget (#16).
- Pinned the Dagger wrapper's logging dependencies through a patched SDK
  generator so GO-2026-4985 stays fixed across regeneration (#8).

[Unreleased]: https://github.com/wangjohn/levenshtein/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/wangjohn/levenshtein/releases/tag/v0.1.0
