# Troubleshooting

## Exit code 2

Exit `2` means `verify` stopped before verifying anything; the message on standard error says why. The [CLI reference](reference/cli.md#exit-codes) lists every cause. The common ones:

| Message or cause | What to do |
| --- | --- |
| `Go 1.27.2 could not be obtained` or `Building the Levenshtein CLI failed` (from the `./verify` launcher) | See [the pinned Go cannot be downloaded](#the-pinned-go-cannot-be-downloaded) |
| `configuration needs "version": 1` (v0.2.0 and earlier) or `configuration needs "version": 1 or 2` (v0.3.0 onward) | Add a supported version to `levenshtein.json`; use `1` for v0.2.0 and earlier pins. Version 2 is available starting with v0.3.0 ([migration](configuration.md#community-rule-modules)) |
| `configuration version 2 requires community rules to run on Dagger` | Use a Dagger environment for the selected check, or explicitly set `"lint": {"rule_modules": false}` for an intentional core-only native check ([community rules](configuration.md#community-rule-modules)) |
| An unknown field, or a hint that a field moved | The file was written for another release. Check the field against [configuration](configuration.md) for the revision you pin; an older release rejects fields added after it |
| A target's `dir` or `workspace` does not exist, or reaches outside the source through a symlink | Paths are relative to `--source`. Declare real paths, not symlinks ([source boundaries](configuration.md#source-boundaries)) |
| `run "<name>" is missing or empty` | The run is not in `levenshtein.json`. A file replaces the defaults, so `branch`, `pre-merge`, and `main` exist only if it declares them |
| `set --shared to the pinned Levenshtein checkout` | Use the `./verify` launcher, which passes it, or pass `--shared` to a prebuilt binary |
| `cache directory must be outside source and shared checkouts` | See [the cache directory](#the-cache-directory) |
| `--write-baseline needs "baseline" in levenshtein.json` | Add a top-level `"baseline"` path first ([baseline](configuration.md#baseline)) |
| A flag combination error | `--render`, `--dry-run`, and the baseline flags exclude one another; see [flags](reference/cli.md#flags) |

A check that ends in `error` is exit `1`, not `2`: the run started, and that check could not reach a verdict. Its message is in the report, and in the `text` format's status lines.

## Docker or Colima is not running

Checks bound to a Dagger environment, which is every check of a repository with no `levenshtein.json`, start a Dagger engine in a container. Without a running Docker-compatible runtime they end in `error` with a message from the Dagger SDK about reaching the engine or the Docker daemon.

- Start the runtime: `colima start levenshtein` for the Colima profile in [setup](setup.md#prerequisites), or open Docker Desktop. `docker info` should succeed before you run `verify`.
- Or bind the checks to a [native environment](configuration.md#native-go-checks), which needs no container runtime. `go-http`, `go-sql`, and `go-mutation` stay Dagger-only.
- `--dry-run` never starts Dagger, so it works either way.

## The pinned Go cannot be downloaded

The `./verify` launcher builds the CLI with the Go version in `.go-version`, setting `GOTOOLCHAIN` so a host Go 1.21+ can download that version through the module proxy. Older Go versions do not support this automatic switching; upgrade the bootstrap Go or install the pinned version on `PATH`. With `GOPROXY=off`, or with no route to the proxy, the download fails and the launcher exits `2`. Install that Go version so it is the `go` on `PATH`, or let the toolchain download reach an approved `GOPROXY`. An offline CLI build also needs its module dependencies cached. A [prebuilt archive](releases.md#running-an-archive) avoids that build, but check tools and Dagger images still need to be provisioned.

Native Go checks do not download a toolchain: they run with `GOTOOLCHAIN=local` on the host's `go`, which should be the version in `.go-version`.

## Private modules and go-mod

`go-mod` runs `go mod tidy -diff` and `go mod verify`, which resolve modules through the module proxy even when the module vendors its dependencies. A private module the check cannot fetch makes it an `error`, never a pass.

- In Dagger, the container has no credentials for private modules. Vendor the dependencies for `go-lint` and `go-vet`, and bind `go-mod` to a native environment.
- On a native environment, pass the host's settings through `pass_env`, such as `GOPRIVATE`, `GONOSUMDB`, `GOPROXY`, and `GOFLAGS`, with the credentials themselves in the host's own Git or `.netrc` configuration, never in `levenshtein.json`.
- A repository that must check offline leaves `go-mod` out of its runs. With no `levenshtein.json`, `go-mod` is in `branch`, `pre-merge`, and `main`, so add a file whose runs leave it out.

See [module manifests](check-kinds-guide.md#module-manifests).

## Shallow clones

`go-apidiff`, `go-mutation`, and `semantic-lint` compare the working tree with the merge base of a base branch: the check's own `base` option, else `GITHUB_BASE_REF`, else `main`. They use `origin/<base>`, or the local branch when it is the same commit or ahead of it. In a shallow clone the base branch can be missing, or present without the history that joins it to `HEAD`; either way the check is an error that names the shallow checkout.

- On GitHub Actions, check out with `fetch-depth: 0`.
- Elsewhere, fetch the base branch with enough history to reach the merge base before running, for example `git fetch --unshallow origin main`.

## The cache directory

Results, the file stat memo, preparation records, helper tools, and the Staticcheck analysis cache live in one directory: by default `levenshtein/verification-v1` under the user cache directory (`~/Library/Caches` on macOS, `$XDG_CACHE_HOME` or `~/.cache` on Linux). `--cache-dir` moves it. It must be outside both the source checkout and the shared checkout; `verify` exits `2` otherwise.

Every cached result is checked against a fresh fingerprint before it is reused, and a missing or corrupt entry is only a miss, so deleting the directory costs a slower next run and nothing else. Do not share a writable cache directory between trusted jobs and untrusted pull requests ([local caching](configuration.md#local-caching)).

## A pattern matches no rule

A `go-lint` check ends in `error` with `go-lint check "<pattern>" matches no rule levenshtein-lint registers` when a pattern in its [`lint.checks`](configuration.md#lint-selection) names no rule the pinned linter has.

- Check the spelling against [the rules](rules.md). Case does not matter; a typo such as `gocogint` does.
- The rule may be newer than your pin. Bump the pin, or drop the pattern.
- A pattern containing `_` selects [community rules](community-rules.md#selection) instead, and must match a rule of a pinned rule module.

## Levenshtein's own checks do not build

In the Levenshtein repository itself, `./verify` checks the `runner` module, which compiles against a generated Dagger SDK that is not committed. Run `dagger develop --compat=skip` once from the repository root ([setup](maintainers/development.md#develop-the-shared-checks)).
