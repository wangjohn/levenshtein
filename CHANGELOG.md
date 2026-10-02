# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Consumers pin a release tag, or its commit SHA, as described in
[docs/releases.md](docs/releases.md). [docs/versioning.md](docs/versioning.md)
says which interfaces are versioned and what to expect when you bump your pin.

## [Unreleased]

### Added

- Release archives include the changelog, contribution and security policies,
  code of conduct, agent conventions, and consumer templates. Archive smoke
  checks now reject missing metadata and broken extracted documentation links;
  `scripts/test-release --metadata-only` runs these checks without Docker.
- The CLI supports `--version` before verification setup and adds optional build,
  implementation snapshot, and effective native Go identity to version 1 JSON
  reports. Cached results retain the identity of their original verification;
  toolchain settings are hashed rather than exposed.
- Release tag protection proposals now cover both `v*` and `runner/lint/v*`,
  with policy and release-guard regressions. Maintainer documentation separates
  proposed settings, verified live enforcement, main ancestry, and PR review.
- Bounded parser fuzz smoke covers configuration, baselines, tool diagnostics,
  and release archive entries; maintainers can run longer sessions with
  `scripts/test-fuzz`.
- Configuration version 2 requires selected native `go-lint` checks that
  participate in community rule modules to use Dagger or explicitly opt out
  with `lint.rule_modules=false`. Version 1 remains accepted and preserves
  skip-with-warning behavior, with migration guidance on fresh results.
  Report and baseline file versions remain 1; released consumer templates
  continue using configuration version 1.
- A contributor check entry point (`scripts/test-contributor`) with explicit test
  tiers, and a public roadmap with priorities and starter contribution tasks.

- The repository and its example rule module now use Apache License 2.0, with
  John Wang named in `NOTICE`. Release archives include both the license and
  notice. Earlier published releases retain their original MIT license.
- Tagged release builds now prepare a draft GitHub release so the maintainer
  can inspect its archives, checksums, SBOMs, attestations, and notes before
  publishing it.
- [docs/check-kinds.md](docs/check-kinds.md) lists every check kind with
  what it checks, which executors run it, whether its results are cached,
  whether a baseline can hold its findings, whether it needs a
  repository-root target, and which default runs include it. The table is
  generated from the verifier's own kind descriptors, and a test fails when
  it falls out of date.
- Documentation: a [docs index](docs/README.md), a
  [CLI reference](docs/reference/cli.md), [troubleshooting](docs/troubleshooting.md),
  a [versioning policy](docs/versioning.md), a [glossary](docs/glossary.md),
  and a [comparison with golangci-lint](docs/faq.md#levenshtein-or-golangci-lint).
  Tests in the root module fail when a relative link or `#anchor` in any
  Markdown file, or a documentation link in code, configuration, or scripts,
  does not resolve, and when the docs index misses a page.
- The release workflow publishes only a `vX.Y.Z` tag whose commit `main`
  contains and whose version `CHANGELOG.md` releases (`scripts/release-on-main`).
  [docs/maintainers/releases.md](docs/maintainers/releases.md#protecting-release-tags)
  has the tag rulesets and immutable-release setting an admin can apply.

### Fixed

- Gocheck reports reject trailing JSON or malformed output instead of accepting
  a clean prefix, and lint reports reject diagnostics missing a rule code.
  Gocheck and ShellCheck reject reports without their diagnostic arrays.
  Release archive traversal also bounds decompressed skipped
  entries to the existing 128 MiB release limit.

### Changed

- Installation examples pin the existing v0.2.0 release and state Go 1.21+
  for source-launcher toolchain switching, separately from native analysis
  pins. Setup distinguishes direct lint, source, and prebuilt execution,
  with guidance for denied downloads and missing Docker.
- Native checks retain the first 1 MiB of each subprocess output stream while
  draining the remainder, report truncation warnings, and reject incomplete
  helper diagnostics as check errors. Result warnings, including skipped
  community rules and deprecation guidance, now appear in text, GitHub, and
  SARIF output as well as JSON.

- **A baseline entry no configured check could report fails every run.**
  An entry whose rule module was removed, whose rule was dropped from a
  module's `select` or turned off in every `go-lint` check, or whose check
  was removed from `levenshtein.json` used to be kept forever, because only
  a check that could report it judged it. Such an entry is now
  [orphaned](docs/configuration.md#orphaned-entries): every run, a partial
  one included, fails with a `baseline-stale` finding for it in the report's
  new `baseline.orphaned` list, and `--write-baseline` drops it whatever the
  run. Entries a configured check the run left out could report are still
  kept as they were.
- **Each pinned Go tool builds from a module of its own.** `actionlint`,
  `apidiff`, `gitleaks`, `govulncheck`, and `gremlins` moved from one shared
  `runner/tools` module to `runner/tools/<tool>`, each with a single `tool`
  directive, so a Dependabot update of one tool needs no manual pin edit and
  cannot move a version another is built with. Every tool still links exactly
  the module versions it did before, and Dependabot proposes each tool's
  updates in a pull request of its own.
- **The documentation moved.** `docs/checks.md` is now a short landing page:
  the rules `go-lint` enforces are in [docs/rules.md](docs/rules.md), the
  evidence and the analyzers left off in
  [docs/rule-selection.md](docs/rule-selection.md), and how each check kind
  behaves in [docs/check-kinds-guide.md](docs/check-kinds-guide.md).
  [docs/reference/config.md](docs/reference/config.md) lists every
  `levenshtein.json` field. The community rules design, and the maintainers'
  CI, development, and release notes, moved under `docs/design/` and
  `docs/maintainers/`. Every old heading stays as a pointer, so links from
  earlier releases' findings still land, and finding URLs and hints now link
  to `docs/rules.md`.
- The Claude Code Stop hook template blocks only the first attempt to stop in a
  turn by default, so an agent that cannot fix a finding, or meets one that
  predates its change, ends with a report instead of looping.
  `LEVENSHTEIN_STOP_ONCE=0` keeps blocking every attempt while the run fails
  ([docs/agents.md](docs/agents.md#stop-keep-working-while-the-run-fails)).
- The workflow template and the consumer examples pin the action by the
  release's commit SHA with the version as a comment
  (`wangjohn/levenshtein@<sha> # vX.Y.Z`), and `scripts/test-doc-pins` checks
  that the SHA is the one the tag names. Releases now update the examples in a
  pull request after the tag, checked with `scripts/test-doc-pins --latest`
  ([docs/maintainers/releases.md](docs/maintainers/releases.md#publishing-a-release)).
- Levenshtein's own CI: concurrency groups are per commit outside pull
  requests, so GitHub no longer cancels queued `main` runs; `dependency-review`
  reports in a merge queue; every `integration`-tagged Go test runs, natively
  unless `scripts/test-integration` lists it as needing Dagger; a non-required
  `macos` job runs the root module's unit tests; the `tests` job's result
  cache saves only result records; the Dagger CLI cache holds the release
  archive, verified against its checksum on every run, instead of the binary;
  `TYPESAFE_API_KEY` is visible only to the semantic-lint step;
  `go-vuln` also scans `examples/rule-module` and `scripts/test-sdk-security`
  scans the built `runner/tools` binaries; Dependabot updates
  `examples/rule-module` and bumps Staticcheck in `runner/lint` and
  `runner/community` together; and `scripts/test-workflows` keeps the
  template's and docs' action pins equal to the workflows'. Dependabot now
  updates the workflow template too, in the same single grouped pull request
  as the workflows, and `scripts/sync-action-pins` copies the pins into the
  docs.
- A test fails when any version recorded twice (the Go version and image,
  Staticcheck, the Dagger engine) disagrees with its copy. zizmor is pinned
  in the same shape as ShellCheck and osv-scanner.

### Fixed

- Native checks publish helper executables at immutable paths identified by
  their compiled bytes. Concurrent checks using different shared revisions or
  build settings can no longer replace a helper another check is about to run.
- The generated Dagger SDK now uses OpenTelemetry logging 0.21.0 and
  core/trace exporters 1.45.0, fixing GO-2026-6508 and GO-2026-6505 without
  suppressions. The generator and runtime share a compatibility patch for
  Dagger's pinned telemetry library; HTTP exporter default paths are preserved.

- [docs/rules.md](docs/rules.md) listed `gocognit` and `deferInLoop`, which
  are off by default, in its table of rules on by default. They are now in
  an [opt-in rules](docs/rules.md#opt-in-rules) table, and a test fails when
  either table disagrees with the selection `runner/toolchain.json` ships.
- A baseline no longer judges a `go-lint` entry by a check that could not
  have reported it. A native check, which skips community rules, reported
  every community-rule entry as stale, and `--write-baseline` deleted them;
  a check whose `lint.checks` turned a rule off (such as `-unparam`) did the
  same to that rule's entries. Such entries are now neither stale nor removed.
- A community rule that sets a diagnostic `Category` now reports under its
  code. Staticcheck dropped every such finding, while the report still listed
  the rule as selected. Community rules also skip generated files, as core
  rules do.
- A rule module can no longer hide the core linter's unused-directive
  findings. Rule code runs in the community linter's process and could print
  an `lvrules_mixed` finding at any line; the runner now drops a core
  unused-directive finding only when the source at that position holds a
  directive that really mixes core and community codes.
- [docs/community-rules.md](docs/community-rules.md) no longer describes the
  `lvrules-template` repository and the `lvrules-check` action as available;
  both are still planned, so rule authors start from `examples/rule-module`.
- Native checks in one run execute in parallel again, up to `--jobs`. Each
  check took the workspace file lock through its own handle, which also
  blocks the same process, so every native check ran alone, even read-only
  ones and cache hits. Read-only kinds (the shared Go kinds and
  `semantic-lint`) now overlap, `command` checks still run alone, a cache hit
  with no artifacts to restore takes no workspace lock, and another process
  on the same source still waits. A check cancelled while it waits for the
  workspace is reported as `cancelled` instead of `error`.
- Git input discovery lists the work tree again after every check and every
  preparation or build stage, not only after a passing check. A failed check,
  or a preparation that generates files into its build's inputs, could leave
  untracked files that a later key in the same run did not see, so a stale
  result or build could be reused.
- A native shared Go check's cache key covers the Go settings that change
  what it reports, such as `GOFLAGS`, `GOEXPERIMENT`, `GOFIPS140`, `GODEBUG`,
  `CGO_ENABLED`, `CC`, and the architecture levels, including values set with `go env -w`. Before, only
  the Go version, OS, and architecture were, so `go env -w
  GOFLAGS=-tags=integration` reused results computed without the tag. Module
  download settings such as `GOPROXY` and `GOPRIVATE` still do not affect the
  key. Existing native shared Go results are recomputed once.
- A native `command` check or stage that exits 0 but leaves a background
  process holding its output, as `sh -c 'server & echo ok'` or a daemonizing
  build tool can, passes with a `detached-output` warning instead of failing
  with `exec: WaitDelay expired before I/O complete`. The leftover process is
  still killed with the command's process group, which happens after every
  run, not only on timeout as docs/architecture.md said; daemons that start
  their own session are unaffected.
- The file stat memo keeps working in repositories above roughly 280,000
  files. Its single record outgrew the 64 MiB read limit, so it was written on
  every run and never read back; it is now split across as many records as it
  needs. A cache record over the limit is refused when written, and a result
  too large to cache says so in its cache `reason`.
- Tests that build throwaway git repositories no longer write to the
  repository `go test` was started from. They inherited `GIT_DIR`,
  `GIT_INDEX_FILE`, and `GIT_WORK_TREE`, which git exports to hooks, so the
  lefthook `pre-push` run of `go test ./...` committed, switched branches, and
  staged files in the pushing checkout. Every test now runs git through
  `internal/testgit`, which drops every inherited `GIT_` variable, and a guard
  test fails on a test file that runs git directly.
- `go-mutation`, `go-apidiff`, and `semantic-lint` measure the change from
  `origin/<base>` when it exists, and use the local branch only when it is the
  same commit or ahead of it. A local `main` left behind after rebasing onto
  `origin/main` used to set an old merge base, so the check judged, and
  `semantic-lint` paid for, other people's commits.
- When the base and `HEAD` share no commit, these checks now say so instead of
  `git merge-base: exit status 1:`. In a shallow checkout, such as
  `actions/checkout`'s default depth of one plus a shallow fetch of the base,
  the message advises `fetch-depth: 0`.
- `semantic-lint` reviews changed files whose names contain a space, a quote,
  a backslash, or escaped non-ASCII bytes. Their diff headers kept git's
  trailing tab or C quoting, so the files were classified as neither Go nor
  Markdown and silently skipped, and commit summaries listed the raw header.
- `semantic-lint` gives each API request 90 seconds and retries one that
  stalls, instead of letting a hung connection use up the check's whole
  timeout. HTTP 500 is retried like 502, 503, 504, and 429, and the client no
  longer waits out a retry delay after its last attempt.
- `semantic-lint` stays advisory when answers are missing. A question the API
  omitted made the check `incomplete`, and one rejected request made it
  `error`, both exiting 1; now unanswered questions and failed requests are
  listed in the output (`details.missing`, `details.errors`) and the check
  passes, unless requests were sent and not one question was answered. When
  the check's timeout elapses mid-run, the answers already received are kept
  instead of discarded. Unanswered questions are named as
  `path:line symbol question`, once each, instead of by wire ids such as
  `comment_explains_why#0` that repeat in every request.
- `semantic-lint` bounds what one run sends: `max_requests` (default 60) and
  `max_input_chars` (default 1,500,000) in the check's `semantic` object.
  States beyond either budget are not sent and are named in a note and
  `details.skipped`, and the change-level state is sent first. A large
  refactor used to send hundreds of requests, run into the timeout, and
  report nothing ([details](docs/semantic-lint.md#how-it-works)).
- A second Ctrl-C or SIGTERM ends `verify` while it writes the report,
  flushes the cache, or closes the Dagger session. The first signal cancels
  the run as before; the second used to be swallowed until the process
  exited, so a hung teardown needed SIGKILL.
- `go-mutation` no longer passes weak tests on a warm build cache. Gremlins
  timed its coverage run, which `go test` could answer from its cache in
  milliseconds, and gave every mutant ten times that; a package whose tests
  took seconds then timed out every mutant, and timeouts counted as caught.
  Gremlins now runs with `GOFLAGS=-count=1`, a timeout counts as caught only
  under a limit of at least 10 seconds, a run that timed out a mutant under a
  shorter limit is repeated with a higher coefficient, and if that still
  falls short the check is incomplete. The summary warns about any package in
  which at least half of the covered mutants, and at least two, timed out
  ([details](docs/mutation.md#timeouts)).
- **`go-mutation` judges accepted survivors more strictly**, so an entry can
  no longer keep a weak test hidden. An entry whose mutants a test now kills,
  or that time out, is stale and fails the check until it is removed. An
  entry whose text matches survivors on more than one line accepts none of
  them and fails with `go-mutation-ambiguous`; the new optional `function`
  and `occurrence` fields say which line it means
  ([details](docs/mutation.md#accepted-survivors)).
- **`go-mutation` runs only the mutants on changed lines**, so its run time
  follows the size of the change rather than of the files it touches. On a
  pull request that touched a large file, about 80% of the covered mutants
  were on unchanged lines, each costing a package test run that could never
  fail the check. The runner hands gremlins a diff of the lines to run, and
  gremlins skips every other mutant without testing it. Lines an accepted
  entry names always run, so an entry is still stale once a test catches its
  mutant, even on a line the branch did not touch. `unchanged` and
  `unchanged_survivors` now list only survivors on such lines, mutants on
  other unchanged lines are counted under `skipped`, and `uncovered` lists
  only lines that ran. `scope: "module"` and untracked files still run every
  line ([details](docs/mutation.md#which-lines-run)).
- LV1002 checks structs built inside `switch`, type switch, and `select`
  cases. A case holds its statements without a block of its own, so
  `var s S; s.A = 1` inside one went unreported.
- LV1006 checks a test whose parameter names `testing.T` through an alias, as
  in `type T = testing.T; func TestX(t *T)`, which go test runs, and no longer
  counts `t.Failed()` as a way to fail: it only reads the test's state.
- LV1001 asks for typed constants only for string types declared in the
  module being linted. Converting a literal to a library's open-ended type,
  such as `corev1.ResourceName("nvidia.com/gpu")`, was reported because the
  library declares a few constants of it.
- LV1005 checks the Go files the default build leaves out, such as
  `foo_windows.go` and files behind `//go:build integration` or
  `//go:build ignore`, which `gofmt -l` checks and LV1005 skipped. The
  linter checks them after Staticcheck's run, outside its cache, so formatting
  one clears its finding on the next run and editing one re-lints no package.
  Their findings appear in `text` and `json` output only
  ([details](docs/rules.md#formatted-files-lv1005)).
- `//lint:ignore recvcheck`, `//lint:ignore unparam`, and
  `//lint:ignore gochecksumtype` suppress a finding that exists in only one of
  a package's builds, with or without its tests, instead of being reported as
  matching nothing by the other build. One build now decides each rule's
  findings on non-test files: the build with tests for `recvcheck`, and the
  build without them for `unparam` and `gochecksumtype`, so a test's calls no
  longer change what `unparam` reports and a test's fake variant no longer
  makes a sum type's switches incomplete. A directive that matches nothing is
  still reported. The `//lint:file-ignore unparam` workaround is no longer
  needed.
- The `verify` launcher exits 2, the setup-error status, for every failure
  before the CLI runs: an unset `HOME` with no `XDG_CACHE_HOME`, a cache
  directory it cannot create, or an unreadable or empty `.go-version` used to
  exit 1, which means a check failed and made the Claude Stop hook block the
  agent.
- The `verify` launcher builds the CLI for the host whatever the caller's Go
  settings: `GOOS`, `GOARCH`, `GOEXPERIMENT` and the like are dropped and
  `GOFLAGS` is replaced for the build only, so a cross-compiling shell no
  longer gets a binary it cannot run and `GOFLAGS=-mod=vendor` no longer breaks
  the build. The CLI itself now receives the caller's environment unchanged,
  without the launcher's `GOWORK` and `GOTOOLCHAIN`.
- Concurrent `verify` launchers no longer rebuild the CLI in place: each builds
  to a temporary file and renames it over the binary, so none can run a
  half-written one.
- Dagger runs no longer resolve the patched SDK adapter's Python dependencies
  from PyPI each time: `sdk/patched-go/uv.lock` locks them with hashes,
  `dagger-io` is pinned to the engine version and the `uv_build` backend
  exactly, and release archives include the lock. Runs still download the
  wheels, but always the same, hash-checked ones. A `deps-vuln` check scans the
  lock in the `main` run.
- A reused result now covers every file its check read. Under `git` discovery
  the cache key left gitignored files out while the checks still read them:
  editing a gitignored generated `*.pb.go` or `vendor/` file replayed a cached
  `go-vet` or `go-test` pass, and adding a secret to a gitignored file replayed
  a cached `secrets` pass. The key, the Dagger import, and the files the
  native scanners read now come from one enumeration. Under `git` discovery
  the Dagger import leaves out the paths the repository's `.gitignore` files
  ignore and `secrets`, `shell-lint`, and `deps-vuln` read only the listed
  files. The Go kinds (`go-lint`, `go-vet`, `go-mod`, `go-test`, `go-http`,
  `go-sql`, `go-vuln`, `go-imports`, `go-generate`, `go-apidiff`,
  `go-mutation`) add the ignored paths the Go toolchain can load: ignored Go
  and cgo sources, module files and `vendor/modules.txt`, whatever a
  `//go:embed` directive in the directory or above could name, `testdata`, and
  symlinks to directories in the source, through which an import path
  resolves. An ignored directory with no `.go` file in it and nothing
  embedding it, such as `node_modules` or a build output, is left out of the
  key and the Dagger import, and a symlink in the ignored content that is kept
  is hashed by its link text rather than disabling result reuse. The Go
  kinds' keys change once, and the native `go-generate` copy holds the same
  files.
- A declared input that git discovery could not see no longer contributes
  nothing to the key. An input spelled with different case than the
  repository (`Src` for `src/` on a case-insensitive filesystem), or an
  exclude spelled that way, is a configuration error when the run is planned.
  An input reached through a symlinked directory follows the source symlink
  rule on either discovery and disables result reuse, as the Dagger path
  already refused it. An input inside a submodule or an untracked nested
  repository is fingerprinted from disk.
- Git discovery finds each input's paths by binary search in the sorted
  listing instead of scanning the whole listing once per input.

### Security

- The Dagger `go-test`, `go-generate`, and `go-mutation` steps, which run the
  repository's own code as root, mount Go module and build cache volumes of
  their own. Before, they shared the volumes that build `levenshtein-lint`,
  `levenshtein-gocheck`, and the pinned tools, so a malicious test could edit a
  linter's module source in the cache and change later linter builds on a
  persistent engine. The tool-build volumes are renamed too, so an engine that
  ran older checks starts them clean. The tools those steps need, gremlins and
  `levenshtein-gocheck`, are built from the tool-build volumes and copied in.
- Those untrusted volumes are now kept per clone: the CLI passes the three
  Dagger functions a `cacheKey`, the SHA-256 of the checkout's git common
  directory (or of its resolved path outside a work tree), and they mount
  `levenshtein-go-{mod,build}-untrusted-<go>-<key prefix>`. Every worktree of
  one clone shares them; separate clones, and repositories, never do. Before,
  every repository on a persistent engine shared them, so one repository's
  tests could change the module sources or build cache another's `go-test`,
  `go-generate`, or `go-mutation` compiled. A direct Dagger call without a key
  uses `unkeyed` volumes. The key is not part of the result fingerprint.
- Before each of those steps runs anything, `go mod verify` checks the module
  cache's copies of the module's dependencies against the hashes recorded when
  they were downloaded, which Go ties to `go.sum`; a copy changed since is a
  check error rather than compiled. The build cache is protected only by the
  per-clone volumes, so shared self-hosted runners that check untrusted
  repositories should still use ephemeral engines
  ([SECURITY.md](SECURITY.md#not-a-sandbox)).

## [0.2.0] - 2026-09-25

### Upgrading from 0.1.0

- **Configuration.** A `levenshtein.json` written for 0.1.0 is accepted
  unchanged: every field it could use still exists, and the new ones (`lint`,
  `rule_modules`, `baseline`, and the `imports` and `apidiff` objects) are
  optional. `version` stays `1`.
- **New default check.** A repository without a `levenshtein.json` now runs
  `go-mod` in `branch`, `pre-merge`, and `main`. An untidy root module, or one
  whose dependencies cannot be downloaded without credentials, fails where it
  passed before. Tidy it, or add a `levenshtein.json` whose runs leave `go-mod`
  out.
- **New findings.** `go-lint` runs more than twenty analyzers and go-critic
  checks 0.1.0 did not (listed under Added), now reports upstream findings in
  cgo files it used to drop, and stops with an error on an analyzer failure
  that used to pass silently. Expect new findings on the first run.
- **go-mutation verdicts.** It now fails only on surviving mutants on lines
  the branch changed, and counts timed-out mutants as caught (see Changed). A
  pull request that failed on inherited survivors or on timeouts can pass.
- **Staging the new findings.** To move the pin before fixing everything, add
  a top-level `"baseline"` path and run `verify main --write-baseline` once
  ([baseline](docs/configuration.md#baseline)), or turn a new rule off for now
  with a `lint.checks` pattern such as `"-unparam"`
  ([lint selection](docs/configuration.md#lint-selection)).
- **GitHub Action.** Annotations are on by default and need no new permission;
  set `annotations: false` to turn them off.
- **JSON report.** Additive only: findings gain `advisory`, `url`, `hint`, and
  `baselined`, results gain `warnings`, and the report gains a `baseline`
  summary when one applies. The report `version` stays `1`.

### Added

- The linter runs with nothing but `go`:
  `go run github.com/wangjohn/levenshtein/runner/lint/cmd/levenshtein-lint@latest ./...`
  from a module's root reports the shipped `go-lint` rules with no checkout,
  container, or config. Release tags gain a `runner/lint/vX.Y.Z` companion so
  the same command can pin a release
  ([details](docs/checks.md#running-the-linter-directly)).

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

- `levenshtein-lint` run without `-checks` selects the shipped rules from
  `runner/toolchain.json` instead of Staticcheck's default, which also turned
  on the opt-in `gocognit` and `deferInLoop`. `-checks=inherit` still defers
  to `staticcheck.conf`. `verify` always passes `-checks`, so its results are
  unchanged.
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
- **`go-mutation` fails only on changed lines and counts timeouts as caught
  (#44), which changes CI verdicts.** The CLI records the lines each modified
  Go file changed, from a zero-context diff, and a surviving mutant fails the
  check only on one of those lines; survivors elsewhere in a mutated file are
  listed under `unchanged` in the summary and counted as `unchanged_survivors`.
  An untracked file, and every file in `scope: "module"`, counts all its lines
  as changed. A timed-out mutant now counts as caught and is listed under
  `timed_out_mutants`. A run is `incomplete` only when three or more covered
  mutants all timed out; before, any timeout made it `incomplete`
  ([details](docs/mutation.md)).
- Levenshtein's own CI runs `branch` and `pre-merge` on the native executor,
  and keeps `main` and `self-test` in Dagger (#38). Its pull requests also run
  `go-mutation` in a `mutation` job beside `semantic-lint` (#51).

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
- A run with a `go-mutation` check no longer breaks the Dagger checks after
  it (#42). When `go-mutation` was the first check to start the shared Dagger
  session, its timeout closed the session for every later check in the run,
  which then failed with "connection reset by peer".
- The file stat memo no longer trusts, within one process, a hash taken in the
  same timestamp tick as the file's last write (#40). On a filesystem with
  coarse timestamps, a same-size rewrite in that tick kept the old content's
  fingerprint, so a cached result could be reused for different inputs. The
  persisted memo already had this guard.

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

[Unreleased]: https://github.com/wangjohn/levenshtein/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/wangjohn/levenshtein/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/wangjohn/levenshtein/releases/tag/v0.1.0
