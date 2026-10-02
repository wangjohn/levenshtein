# CLI reference

```text
verify [RUN] [flags]
verify --render REPORT --format FORMAT
verify --version
```

`./verify` is a shell launcher in the Levenshtein checkout. It needs Go 1.21+ on `PATH` for automatic toolchain switching and builds the CLI in `cmd/levenshtein` with the Go version in `.go-version`, then runs it with `--shared` and `--source` set to the checkout it lives in, so pass `--source` to verify another repository. A [release archive](../releases.md) ships the same CLI prebuilt as `levenshtein`, which takes the same arguments; give it `--shared` yourself.

Flags may come before or after `RUN`. The [configuration reference](config.md) lists every field of `levenshtein.json`, which declares the runs.

## The run

`RUN` names one run from the source repository's `levenshtein.json`, or from the [defaults](../configuration.md#version-1) when it has none. It defaults to `branch`. At most one run is accepted. A run name only selects checks: it does not switch Git branches or fetch code.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--version` | off | Print CLI build identity and exit `0` before loading configuration or starting executors (upcoming source behavior; absent in released v0.2.0) |
| `--source DIR` | the current directory; the `./verify` launcher passes its own checkout | Repository to verify. Its `levenshtein.json` is read from here, and every path in the report is relative to it |
| `--shared DIR` | `$LEVENSHTEIN_SHARED_ROOT`; the launcher passes its own checkout | The pinned Levenshtein checkout or extracted archive whose checks run. Needed unless `--version`, `--dry-run`, or `--render` is given |
| `--cache-dir DIR` | `levenshtein/verification-v1` under the user cache directory (`~/Library/Caches` on macOS, `$XDG_CACHE_HOME` or `~/.cache` on Linux) | Where results, preparation records, the file stat memo, helper tools, and the Staticcheck cache live. It must be outside both the source and the shared checkout. See [local caching](../configuration.md#local-caching) |
| `--jobs N` | `0` | How many checks run at once. `0` keeps the built-in cap, the smaller of four and `GOMAXPROCS`; any other value replaces it |
| `--dry-run` | off | Print the plan as JSON and run nothing. Starts no executor, so it needs no container runtime. Takes no `--format` other than `json`, and no `--write-baseline` |
| `--format FORMAT` | `json` | `json`, `text`, `github`, or `sarif`. See [output formats](#output-formats) |
| `--path-prefix DIR` | none | A directory joined in front of every path in `text`, `github`, and `sarif` output, for a source that is a subdirectory of the checkout that annotations and SARIF locations are relative to. It must be a relative path inside the checkout, and it is an error with `json` |
| `--render REPORT` | none | Write a saved JSON report, from a file or `-` for standard input, in `--format`, and run nothing. See [rendering a saved report](#rendering-a-saved-report) |
| `--no-baseline` | off | Report every finding as if no [baseline](../configuration.md#baseline) were configured, without reading the file |
| `--write-baseline` | off | Run without the baseline, print the report, then rewrite the configured baseline file from it. See [baseline commands](../configuration.md#commands) |
| `-h`, `--help` | | Print usage and exit `0` |

Combinations that would mean nothing are errors (exit `2`): `--render` with a run, `--dry-run`, or a baseline flag; `--dry-run` with a `--format` other than `json` or with `--write-baseline`; `--write-baseline` with `--no-baseline`; and `--path-prefix` with `json`.

## Exit codes

The exit code is the same whichever `--format` is chosen.

| Code | Ordinary run | `--write-baseline` | `--render` |
| --- | --- | --- | --- |
| `0` | Every selected check passed, and no baseline entry is orphaned | The baseline file was written, whatever the run found | The report was written, whatever its status |
| `1` | A check failed, errored, was cancelled, or is incomplete, or the [baseline](../configuration.md#outcomes) has an entry no configured check can report | A check did not reach a verdict, so the file was left alone | |
| `2` | The command could not start: see below | As for an ordinary run, or the configuration names no `baseline` file | The input could not be read or is not a version 1 report |

Exit `2` means nothing was verified, or the result could not be written. The causes are a bad flag or run name, an unreadable or invalid `levenshtein.json` or baseline file, a plan that fails validation (a missing target directory, an unknown check, an input spelled with different case than on disk), no shared checkout, a `--cache-dir` inside the source or shared checkout, a pinned [rule module](../community-rules.md) the release has withdrawn, and a failure to write the plan, report, or baseline. The `./verify` launcher also exits `2` when it cannot obtain the pinned Go or build the CLI. [Troubleshooting](../troubleshooting.md#exit-code-2) has remedies.

## Output formats

| Format | Writes |
| --- | --- |
| `json` | The full versioned report: the plan, and a result for every selected check with its status, findings, cache status, and timings |
| `text` | One line per finding, `file:line:col: CODE message`, then one status line per check and a total |
| `github` | GitHub Actions workflow commands: an error annotation per failing finding, a warning per advisory finding, and one per check that did not pass without a finding to show |
| `sarif` | SARIF 2.1.0 for GitHub code scanning |

[Output formats](../configuration.md#output-formats) describes each in full, and [fix hints](../configuration.md#fix-hints) the one-line hints some findings carry.

## Rendering a saved report

`--render` turns a JSON report an earlier run saved into another format without planning or running anything, so a CI job can keep one JSON report and derive annotations and SARIF from it:

```sh
./levenshtein/verify pre-merge --source ./app > report.json   # exits 0, 1, or 2 as usual
./levenshtein/verify --render report.json --format github --path-prefix app
./levenshtein/verify --render report.json --format sarif > levenshtein.sarif
```

It fills in [fix hints](../configuration.md#fix-hints) the saved report lacks. The GitHub Action renders its annotations and SARIF file this way.

## Environment

| Variable | Used for |
| --- | --- |
| `LEVENSHTEIN_SHARED_ROOT` | Default for `--shared` |
| `TYPESAFE_API_KEY` | The API key a [`semantic-lint`](../semantic-lint.md) check reads from the host |
| `GITHUB_BASE_REF` | The base branch `go-mutation`, `go-apidiff`, and `semantic-lint` compare with when their configuration names none; otherwise `main` |
| `XDG_CACHE_HOME` | Where the `./verify` launcher keeps the CLI binary it builds (default `~/.cache`) |

Native `command` checks receive `LEVENSHTEIN_SOURCE`, `LEVENSHTEIN_WORKSPACE`, and `LEVENSHTEIN_RERUN_CHECKS`; see [native commands](../configuration.md#native-commands).

## Build and verification identity (upcoming)

These additions apply to the upcoming source revision; released `v0.2.0` pins do not provide `--version` or this metadata. The JSON report remains version `1`. All new objects are optional, so older saved reports still render.

`levenshtein --version` prints one line:

```text
levenshtein VERSION commit=COMMIT state=STATE go=GO_VERSION os=OS arch=ARCH
```

Release linker flags supply the version and full commit together. Linked builds retain Go's recorded VCS modification state, so a local snapshot built from edited sources reports `dirty`; paired linker identity defaults to `clean` when that state is absent. Ordinary builds report `development`, the Go build's VCS commit when available, and `clean`, `dirty`, or `unavailable` state. Builds without VCS metadata, including the source launcher (`-buildvcs=false`), report `commit=unavailable state=unavailable`. A partial release identity is treated as development. No flags or environment values are printed. `--version` needs no configuration, shared checkout, or Docker; the source launcher still needs Go to build the CLI.

| JSON location | Fields (all strings) | Meaning |
| --- | --- | --- |
| `build` | `version`, `commit`, `state`, `go_version`, `os`, `arch` | Identity of the reporting CLI; the same values as `--version` |
| `results[].implementation` | `digest` | SHA-256 of the exact shared source snapshot used by the verification fingerprint; covers each check's implementation inputs, not an executable digest |
| `results[].toolchain` | `digest`, `version`, `os`, `arch`, `settings_digest` | Effective native Go identity for shared Go checks; `digest` is the existing host toolchain fingerprint, and `settings_digest` hashes its result-changing Go settings |

Settings are hashed because Go flags can contain secrets. These hashes are identifiers, not secret sanitizers for low-entropy values. Dagger's pinned toolchain is covered by the implementation snapshot; there is no host toolchain object for Dagger or unrelated native kinds. An identity unavailable before execution is omitted. A cache hit retains the identities stored with the original verdict, including an absent identity in an older cache record. Rendering a saved report preserves its metadata. No helper executable digest is claimed unless the executable has actually been built.

For bug reports, include `--version` output (or the exact release/commit for `v0.2.0`) and the JSON report's identity objects. Review the rest of the report and configuration for private content before posting.
