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
- `go-lint` runs three more upstream analyzers: `unparam` (unused parameters
  and results of unexported functions), `musttag` (untagged fields in structs
  passed to JSON, XML, YAML, and TOML encoders and decoders), and `recvcheck`
  (types whose hand-written methods mix pointer and value receivers).
  Consumers see their findings when they bump their Levenshtein pin.
- `go-lint` runs go-critic's likely-bug (`diagnostic`) checkers, minus the ones
  a rule already on repeats, plus `filepathJoin` and `badRegexp`. Each finding's
  code is the checker's name, such as `offBy1`, so one checker can be ignored or
  deselected on its own ([selection](docs/checks.md#the-go-critic-selection)).
- `go-lint` runs `contextcheck`, which reports a function that has a context but
  calls something that starts its own, so cancelling the caller does not stop
  the work.
- `go-lint` runs six analyzers for known bug patterns: `nilnesserr` (returning
  an error already known to be nil), `fatcontext` (a context that wraps itself
  in a loop), `bidichk` (Unicode bidirectional controls that make code display
  differently than it compiles), `gocheckcompilerdirectives` (misspelled
  `//go:` directives), `exptostd` (`golang.org/x/exp` calls the standard
  library replaces), and `usetesting` (tests that leave the environment,
  working directory, or temporary files changed). None fired on Levenshtein;
  [docs/checks.md](docs/checks.md#known-bug-patterns) explains why each is on
  anyway.
- `go-lint` runs two logging analyzers: `zerologlint` (a zerolog event never
  sent with `Msg` or `Send`) and `loggercheck` (a key without a value for logr,
  klog, zap's sugared logger, or go-kit log). A missing value in a `log/slog`
  call stays with go vet's `slog` check, so it is reported once. Neither fired
  on Levenshtein, which uses none of these loggers;
  [docs/checks.md](docs/checks.md#known-bug-patterns) explains why each is on.
  OpenTelemetry's `spancheck` was tried and left out, because it cannot tell
  an ended span from an open one under Staticcheck's loader, and `sloglint` was
  left out because its mixed-argument check is a consistency rule, not a bug
  check.
- [docs/checks.md](docs/checks.md#considered-and-off) lists the analyzers that
  were measured and left out, with the reason for each.
- `levenshtein-lint` includes `gocognit`, which reports a function whose
  cognitive complexity is over 30. It is off in the shipped selection, so
  `go-lint` does not report it and no consumer sees new findings; a repository
  turns it on with the `lint` option below
  ([opt in](docs/checks.md#opt-in-complexity-gocognit)).
- A `go-lint` check in `levenshtein.json` accepts an optional
  `"lint": {"checks": [...]}` object whose Staticcheck patterns are appended to
  the shipped selection, so a repository can turn an opt-in rule such as
  `gocognit` on, or a default rule off, without restating the rest. It works on
  both executors, is part of the check's result key, and rejects malformed
  entries at planning time and patterns that match no registered rule when the
  check runs ([lint selection](docs/configuration.md#lint-selection)). This is
  an additive field of configuration version 1; files without it are
  unchanged and keep their cached results.

### Changed

- **Repositories without a `levenshtein.json` now run `go-mod` in their
  default `branch`, `pre-merge`, and `main` runs**, next to `go-lint` and
  `go-vet`. An untidy root module, or one whose dependencies the container
  cannot download (such as private modules reached only through `vendor/`),
  starts failing those runs when the pin is bumped. Tidy the module, or add a
  `levenshtein.json` whose runs leave `go-mod` out.
- Levenshtein's own CI checks its module manifests through `go-mod` in the
  `lint` job's `branch` run instead of two separate workflow steps.

### Fixed

- Input discovery passes the run's context to the `git ls-files` it starts, so
  cancelling `verify` stops it too, and a cancelled or failed listing is no
  longer remembered for the rest of the run.

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
