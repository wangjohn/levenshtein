# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Nothing has been released yet: consumers pin a reviewed commit or build an
archive from one, as described in [docs/releases.md](docs/releases.md).

## [Unreleased]

Everything merged into `main` so far, grouped by what it gave a consumer.

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

### Changed

- Split `Check` into per-kind option objects and routed native kinds through
  one registry, so a check carries only the options its kind accepts (#17,
  #19, #25).
- Simplified the verification core: legacy configuration dropped, `Result`
  helpers collapsed, the shared fingerprint memoized, one stage store (#14).
- Separated product documentation from the pilot roadmap (#15).
- Split CI into `lint` and `tests` jobs with per-job result caches, prefix
  restore keys, composite actions for the Dagger CLI and generated SDK, and
  single-sourced pins (#11, #18, #26).

### Security

- Hardened `semantic-lint` credentials: the API key and API origin are read
  from the host only and rejected in committed configuration, diff prefixes
  are pinned, and request state is fitted to the model's budget (#16).
- Pinned the Dagger wrapper's logging dependencies through a patched SDK
  generator so GO-2026-4985 stays fixed across regeneration (#8).

[Unreleased]: https://github.com/wangjohn/levenshtein/commits/main
