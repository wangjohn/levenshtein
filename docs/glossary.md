# Glossary

The terms `levenshtein.json` and the docs use, in the order you usually meet them.

**Target.** A named directory to check, declared under `targets`: its working directory `dir`, its `workspace`, its `inputs`, and optionally `exclude` and `discovery`. A repository with several Go modules has one target per module. See [configuration](configuration.md#version-1).

**Workspace.** A target's `workspace`, the directory native `command` checks receive as `LEVENSHTEIN_WORKSPACE` and where preparation stages run. It defaults to `.`, the source root. It is not a Go workspace: native Go checks find a `go.work` from the target's `inputs` ([workspace selection](configuration.md#native-go-checks)).

**Input.** A literal repository-relative file or directory a target declares in `inputs`. Inputs are what a check's result is keyed on, and for Dagger checks they are also the only source copied into the container. See [source boundaries](configuration.md#source-boundaries).

**Discovery.** How the files under a target's inputs are listed for fingerprinting: `git` (the default) lists the files the work tree tracks or would add, and `filesystem` walks every path. See [input discovery](configuration.md#input-discovery).

**Environment.** A named place checks run, declared under `environments`. Its `executor` is `dagger` or `native`; a native environment can also declare `identity`, `env`, `pass_env`, and pinned `tools`.

**Executor.** What actually runs a check. The **Dagger** executor runs it in a pinned container through a Dagger engine, which needs a Docker-compatible runtime. The **native** executor runs it on the host: a `command` check's script, `semantic-lint`, or a shared Go check using the host's Go. See [native Go checks](configuration.md#native-go-checks).

**Check.** A named entry under `checks`: a `kind`, a `target` (or a `targets` list), an `environment`, and the option object its kind accepts. One check with `targets` plans one check per target, with the ID `<check>/<target>`.

**Kind.** What a check does, such as `go-lint`, `go-vet`, `secrets`, or `command`. [Check kinds](check-kinds.md) lists them all with their executors and caching.

**Run.** A named list of checks under `runs`, chosen on the command line: `./verify pre-merge`. `branch`, `pre-merge`, and `main` are conventions, not special names; the [defaults](configuration.md#version-1) for a repository without `levenshtein.json` define those three plus one run per shared kind.

**`rerun_checks`.** A run option that skips reused results, so every check in the run executes. Download and build caches are still used. Scheduled audits, usually `main`, set it.

**Baseline.** A checked-in file of findings a repository already had when it adopted the rules. Recorded findings are reported without failing; new ones fail; an entry that no longer matches fails until it is deleted, so the file only shrinks. See [baseline](configuration.md#baseline).

**Advisory.** A finding that is reported but never fails its check: a community rule a repository marks advisory, and every `semantic-lint` finding.

**Shared checkout.** The pinned Levenshtein revision whose rules and tools run, passed as `--shared`. The `./verify` launcher passes the checkout it lives in. It is separate from the source being verified, and the cache directory must be outside both.

**Rule module.** A Go module of `go/analysis` analyzers that exports the `lvrules` contract, pinned under `rule_modules` so its rules run beside the shipped ones in `go-lint`. See [community lint rules](community-rules.md).
