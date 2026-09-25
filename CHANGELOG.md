# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Consumers pin a release tag, or its commit SHA, as described in
[docs/releases.md](docs/releases.md).

## [Unreleased]

### Added

- Community lint rules: a top-level `rule_modules` object pins lint rules
  published as ordinary Go modules, which run beside the shipped rules in every
  Dagger `go-lint` check and report into the same results. Each rule reports as
  `<namespace>_<name>`, works with `//lint:ignore`, and is selected per module
  (`select`) and per check (`lint.checks`); `"lint": {"rule_modules": false}`
  opts one check out. Rules can be advisory, reported without failing the
  check, and take settings through their analyzer flags. The community linter
  is a separate binary on the same pinned Staticcheck, built for the exact
  pins with nothing allowed to move off them, and its lint step runs with no
  credentials and nothing shared writable. A rule that returns an error or
  panics makes the check an error while the core findings are still reported.
  Native `go-lint` checks run the shipped rules and warn that they skipped
  community rules. `runner/rule-modules.json` lists versions a release
  refuses or warns about. See [docs/community-rules.md](docs/community-rules.md).
- Lint findings carry `advisory` and, where the rule has one, a documentation
  `url`; community findings also name their `source` module. Results carry
  `warnings`, which are kept with cached results, and the GitHub Action's job
  summary lists advisory findings and warnings.
- `--format` writes the report as `json` (the default, unchanged), `text`
  (one `file:line:col: CODE message` line per failing finding, then a status
  line per check), `github` (Actions annotations, escaped as the runner
  expects), or `sarif` (SARIF 2.1.0 for code scanning, one run with a rule per
  code). `--render REPORT` writes a saved JSON report in any format without
  running anything, and `--path-prefix` places paths under a subdirectory.
  The exit status is the same in every format
  ([details](docs/configuration.md#output-formats)).
- Findings whose fix is mechanical carry a one-line `hint`, such as
  `gofmt -w <file>` for LV1005 or `go fix -minmax ./...`, shown in text
  output. Levenshtein never applies one
  ([table](docs/configuration.md#fix-hints)).
- A findings baseline: an optional top-level `baseline` file records existing
  `go-lint`, `go-http`, `go-sql`, `go-imports`, and `shell-lint` findings by
  kind, target directory, file, code, and normalized message, never by line. Recorded findings are reported
  as `baselined` and do not fail; new ones fail as before; an entry a check no
  longer matches fails as `baseline-stale` until it is deleted.
  `--write-baseline` records a run in which every check reached a verdict, as
  sorted one-entry-per-line JSON, and `--no-baseline` reports everything. The
  baseline applies to the finished report, never to a cached result; a check
  whose findings are all baselined still executes on every run
  ([details](docs/configuration.md#baseline)).
- The GitHub Action annotates failing findings by default (`annotations`),
  writes a SARIF file when the `sarif` input names one (the `sarif` output),
  and lists up to 50 failing findings in the job summary. It needs no new
  permission; uploading the SARIF file needs `security-events: write` in the
  calling job ([details](docs/consumer-ci.md#github-actions)).
- `templates/`: a starter `levenshtein.json`, a GitHub Actions workflow with
  annotations and a code scanning upload, Claude Code hooks that block
  finishing while the `branch` run fails and report unformatted Go files after
  each edit, and an `AGENTS.md` section, all tested in this repository
  ([details](docs/agents.md)).

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
  build or set up, a module with no tests or whose every test skipped, and a
  host without a C compiler are errors. It reads `go test -json` to tell them
  apart, leaves vet to `go-vet`, reuses its result like `go-vet` does, and is
  not in any default gate: add it to a run of your own, and keep tests that
  need services in a `command` check ([details](docs/checks.md#tests)).
- `go-imports`, a shared check on both executors that enforces a repository's
  layering rules. Each rule names packages relative to the target (`packages`,
  such as `./internal/store/...`) and what they may not import (`deny`) or the
  only things they may import (`allow`), with `...` wildcards, `./` entries
  resolved against the target's import path, and `std` for the standard
  library; `tests` says whether `_test.go` files count, and `reason` is
  repeated in every finding. Each forbidden direct import is a finding at the
  import's line; generated files are skipped. A package pattern that matches
  no package is an error, so a typo cannot leave a rule checking nothing. The
  check reads `go list -find` and the files' import declarations, so it needs
  no downloads or type checking, and its rules are part of the result key. It
  runs `levenshtein-gocheck`, a new program in `runner/lint` that both
  executors build, and is in no default gate: declare the rules in an
  `imports` object ([details](docs/checks.md#import-boundaries)).
- `go-generate`, a shared check on both executors that runs `go generate ./...`
  in a scratch copy of the target's declared inputs, never the working tree,
  and reports every file it adds, changes, or deletes as a finding at the first
  changed line with git's unified diff (capped at 200 lines). Files the copy's
  `.gitignore` ignores do not count. A `go generate` failure is an error; one
  caused by a tool the pinned image lacks, such as `protoc`, says to use a
  `command` check, while `go run pkg@version` directives work. A module with no
  `//go:generate` directive is an error rather than an empty pass. Its result is
  reused like `go-vet`'s, and it is in no default gate
  ([details](docs/checks.md#generated-code)).
- `go-apidiff`, a shared check on both executors that compares a library
  module's exported API at the merge base with a base branch (the `apidiff`
  object's `base`, else `GITHUB_BASE_REF`, else `main`) against the working
  tree, using `golang.org/x/exp/cmd/apidiff`, now pinned in `runner/tools`. Each
  incompatible change is a finding at the declaration it concerns; compatible
  changes pass and are listed in the report's summary. Internal and `main`
  packages are not compared, and a module that is new or has a new module path
  passes with a note. The CLI exports the base tree from git history on the
  host, so the result is never reused by the CLI; the checkout needs the base
  branch. It is in no default gate
  ([details](docs/checks.md#api-compatibility)).
- Levenshtein defines a `go-apidiff` run over `examples/rule-module`, which
  enforces that a rule module keeps the exports it has published.
- Levenshtein checks its own layering with `go-imports` in `branch`,
  `pre-merge`, `branch-dagger`, and `main`.
- `shell-lint`, a shared check on both executors that runs ShellCheck 0.11.0
  over `*.sh` and `*.bash` files and extensionless scripts with a sh, bash,
  dash, or ksh shebang, skipping `testdata`, `vendor`, and `node_modules`. Each
  warning or error is a finding with its `SC` code and location; `info` and
  `style` are not reported. It honors one root `.shellcheckrc` and otherwise
  reads none. The upstream release archive is pinned by SHA-256 per platform
  and verified before it runs ([details](docs/checks.md#shell-scripts)).
- `secrets`, a shared check on both executors that scans the target's files,
  not its history, with gitleaks 8.30.1's default rules. Each leak is a finding
  coded by its rule, and every value is redacted before gitleaks reports or
  logs it, so no secret reaches the report or the cache. It honors a root
  `.gitleaks.toml` and `.gitleaksignore` and `gitleaks:allow` comments; gitleaks
  is built from `runner/tools` ([details](docs/checks.md#secrets)).
- `deps-vuln`, a shared check on both executors that runs osv-scanner 2.6.0
  over non-Go dependency lockfiles (npm, pnpm, yarn, Python, Cargo, Gemfile,
  and more), skipping `testdata`, `vendor`, and `node_modules`, and reports
  each vulnerable package version, with its advisories, at its lockfile. Go
  modules stay with `go-vuln`. Like `go-vuln` it is never
  cached and needs the network, and a target without a lockfile is an error. It
  honors `osv-scanner.toml` for ignores, and the release binary is pinned by
  SHA-256 per platform ([details](docs/checks.md#dependency-vulnerabilities)).
  `shell-lint`, `secrets`, and `deps-vuln` require a repository-root target and
  are not in any default gate: add them to runs of your own.
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
- `go-lint` runs `scannererr`, which reports a `bufio.Scanner` loop that never
  checks `Err`, so a read error or an over-long line ends the input early with
  no error, and `testableexamples`, which reports an `Example` function without
  an `// Output:` comment that `go test` compiles but never runs. Measured over
  eight open-source Go codebases, `scannererr` found 18 real cases in four of
  them and `testableexamples` 5 in two
  ([evidence](docs/checks.md#measured-on-other-codebases)).
- `go-lint` runs seven more analyzers for known bug patterns:
  `reflectvaluecompare` (`reflect.Value`s compared with `==`), `httpmux`
  (Go 1.22 `ServeMux` patterns in a module on an older Go), `gochecksumtype`
  (a type switch over a `//sumtype:decl` interface that misses a variant; a
  `default` case does not count), and go-critic's `badSyncOnceFunc`,
  `evalOrder`, `rangeAppendAll`, and `returnAfterHttpError`. None fired on
  Levenshtein or the other codebases measured;
  [docs/checks.md](docs/checks.md#known-bug-patterns) explains why each is on.
  A consumer upgrading may see new findings from these and the two above.
- `levenshtein-lint` includes go-critic's `deferInLoop`, a `defer` inside a
  loop, off in the shipped selection like `gocognit`
  ([opt in](docs/checks.md#opt-in-resources-deferinloop)).

### Changed

- The Dagger CLI path calls `goLintReport`, which returns a passing check's
  advisory findings and warnings; `goLint` stays the Dagger check.
- Both linters register only the rules a check selects and guard each one:
  an analyzer that returns an error or panics now stops the run with an
  error instead of leaving a silently passing package (Staticcheck swallows
  analyzer errors and caches the pass). The run stops before Staticcheck
  caches the failed package, so no later run reuses its results, and a rule
  that is turned off never runs, so a broken rule can be switched off.
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
- Levenshtein's own `branch`, `pre-merge`, `branch-dagger`, and `main` runs
  include `shell-lint` over `scripts/` and `verify`, and `secrets` over the
  repository less its deliberately leaky fixture. Neither found a problem in
  Levenshtein's own files; its secrets-handling tests mark their made-up key
  with `gitleaks:allow`.
- Levenshtein's own `levenshtein.json` has a native `go-test` run over the
  repository and `runner/lint` for local use. It is not part of `branch`,
  `pre-merge`, or `main`, because CI's `tests` job already runs
  `go test -race` over the same modules.

### Fixed

- `musttag` no longer fails, unnoticed, on the test main `go test` generates
  for a package with tests; the new analyzer guard surfaced the swallowed
  error.
- Input discovery passes the run's context to the `git ls-files` it starts, so
  cancelling `verify` stops it too, and a cancelled or failed listing is no
  longer remembered for the rest of the run.
- `go-lint` in a package that imports `"C"` judges each file by its original
  source instead of cgo's generated rewrite of it. Upstream findings in
  hand-written cgo files, which were all silently dropped, are now reported,
  and LV1005 no longer reports cgo's build-cache output as unformatted.
- [docs/consumer-ci.md](docs/consumer-ci.md) no longer says there is no shared
  `go-test` check, and its list of what is available names `go-test`,
  `workflow-security`, and `go-mutation`.
- `//lint:ignore nilerr <reason>` suppresses the `nilerr` finding on its line.
  nilerr applied the directive itself and dropped the finding, so Staticcheck
  then reported the directive as matching nothing and the check failed either
  way. Upstream analyzers now leave `//lint:ignore` to Staticcheck.

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
