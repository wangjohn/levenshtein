# Configuration reference

Every field of `levenshtein.json`, the file `./verify` reads from the root of the source repository. [Configuration](../configuration.md) is the guide: what the pieces mean, worked examples, and how caching, the baseline, and native execution behave. The [CLI reference](cli.md) covers the flags that pick a run and write the report.

The file is one JSON object. Unknown fields anywhere are a configuration error (exit 2), and so is a field on an object whose kind does not take it. Paths are repository-relative: clean (no `..`, no trailing `/`, no backslash), and never absolute. Without the file, a repository gets the [defaults](../configuration.md#version-1).

Most errors are reported when a run is planned, before any check starts, and only for what that run selects: a mistake in a check no run selects goes unreported until one does. `rule_modules` and `baseline` are checked for every run.

## Top level

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `version` | number | required | Must be `1`. A file without it is rejected rather than guessed at |
| `targets` | object of [target](#targets) | required | Named directories to check, keyed by target name |
| `environments` | object of [environment](#environments) | required | Named ways to run a check, keyed by environment name |
| `checks` | object of [check](#checks) | required | Named checks, keyed by check ID |
| `runs` | object of [run](#runs) | required | Named groups of checks, keyed by run name; `./verify <run>` picks one |
| `preparations` | object of [stage](#preparations-and-builds) | none | Shared setup a `command` check can name in `command.preparation` |
| `builds` | object of [stage](#preparations-and-builds) | none | Build steps a `command` check can name in `command.build` |
| `rule_modules` | object of [rule module](#rule_modules) | none | Community lint rule modules every `go-lint` check runs, keyed by module path |
| `baseline` | string | none | Repository-relative path of the [baseline](#baseline) file |

## `targets`

A target is a directory and the files a check of it may read.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `dir` | string | required | Directory the check runs in, relative to the source root; `"."` is the root. Must exist. The kinds [check kinds](../check-kinds.md) marks as needing the repository root require `"."` |
| `workspace` | string | `"."` | Directory `command` checks see as `LEVENSHTEIN_WORKSPACE` and preparation and build stages run in. Must exist |
| `inputs` | array of strings | required, nonempty | Literal files and directories the target's checks read. They are the cache key for every executor, and on Dagger the only files imported. No glob or negation characters (`*?[]{}!`) on Dagger. A missing path is allowed and recorded; see [source boundaries](../configuration.md#source-boundaries) |
| `exclude` | array of strings | none | Literal paths to drop from `inputs`, from the key and from the Dagger import. No pattern characters, and not `"."`. See [excluding paths](../configuration.md#excluding-paths) |
| `discovery` | string | `"git"` | How files under `inputs` are enumerated: `git` lists the work tree's own files, as the repository's `.gitignore` files allow; `filesystem` walks every path. See [input discovery](../configuration.md#input-discovery) |

Every entry of `inputs` and `exclude` must be spelled the way the repository spells it, including its case.

## `environments`

An environment says which executor runs a check and, for a native one, what its processes see.

| Field | Type | Default | Applies to | Meaning |
| --- | --- | --- | --- | --- |
| `executor` | string | required | all | `dagger` or `native`. [Check kinds](../check-kinds.md) lists which executors run each kind |
| `identity` | string | none | `native` | Names a provisioned worker setup. Required for a cached `command` check, and for sharing preparation and build results; see [local caching](../configuration.md#local-caching) |
| `env` | object of strings | none | `native` | Fixed, nonsecret variables for the check's processes. Names cannot contain `=` or start with `LEVENSHTEIN_` |
| `pass_env` | array of strings | none | `native` | Names of host variables passed through to the check's processes, which otherwise inherit only `PATH`, `HOME`, and temporary-directory and system-root variables. Same naming rules as `env` |
| `tools` | array of [tool](#tools) | none | `native` | Tools whose version output must match before any check in the environment runs |

A Dagger environment with `identity`, `env`, `pass_env`, or `tools` is a configuration error: the container pins its own tools and sees no host variables. A `semantic-lint` check's environment may not declare `TYPESAFE_API_KEY` or `TYPESAFE_BASE_URL` in `env`, `TYPESAFE_API_KEY` in `pass_env`, or `PATH` or `GIT_*` in either; see [semantic lint](../semantic-lint.md#api-keys).

### Tools

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `command` | array of strings | required | Command that prints the tool's version, such as `["go", "version"]` |
| `version` | string | required | The exact standard output expected |

## `checks`

A check runs one kind over a target in an environment. Its ID is the key it is declared under.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `kind` | string | required | One of the kinds in [check kinds](../check-kinds.md) |
| `target` | string | one of `target` and `targets` | The target to check |
| `targets` | array of strings | one of `target` and `targets` | Several targets, planned as one check each with the ID `<check>/<target>`, in order. Nonempty, without repeats. See [one check, several targets](../configuration.md#one-check-several-targets) |
| `environment` | string | required | The environment to run in. Its executor must be one the kind supports |
| `command` | [command object](#command) | required on `command` | The command to run |
| `semantic` | [semantic object](#semantic) | none | Options for `semantic-lint` |
| `mutation` | [mutation object](#mutation) | none | Options for `go-mutation` |
| `lint` | [lint object](#lint) | none | Options for `go-lint` |
| `imports` | [imports object](#imports) | required on `go-imports` | The layering rules `go-imports` checks |
| `apidiff` | [apidiff object](#apidiff) | none | Options for `go-apidiff` |

A check carries only its own kind's object; any other is a configuration error. Every other kind takes no options.

### `command`

For `command` checks, which run only on a `native` environment. See [native commands](../configuration.md#native-commands).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `args` | array of strings | required, nonempty | The program and its arguments, run in the target's `dir` without a shell. Shell syntax needs an explicit shell, such as `["bash", "-c", "..."]` |
| `rerun_args` | array of strings | none | What a fresh run (`rerun_checks: true`) executes instead of `args`; it must bypass any verdict cache of its own, such as `go test -count=1`. Required for a check in a fresh run, and for `cache: true` |
| `env` | object of strings | none | Variables for this check only, added to the environment's `env`. Same naming rules |
| `timeout` | duration string | `"5m"` | A positive Go duration, such as `"90s"` or `"10m"`. The process group is stopped when it elapses, and the check is an error |
| `preparation` | string | none | Name of an entry in top-level `preparations` to run first |
| `build` | string | none | Name of an entry in top-level `builds` to run after the preparation |
| `artifacts` | array of strings | none | Repository-relative regular files the command must produce; a missing one is an error. Kept with a cached result |
| `cache` | boolean | `false` | Reuse a passing result while the inputs are unchanged. Needs the environment's `identity` and `rerun_args` |

### `semantic`

For `semantic-lint`, which runs only on a `native` environment. See [semantic lint](../semantic-lint.md).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `base` | string | `GITHUB_BASE_REF`, else `main` | Branch the change is measured against |
| `model` | string | `jev-1.13.0` | A pinned model release, `jev-X.Y.Z`; aliases such as `jev-latest` are rejected |
| `timeout` | duration string | `"5m"` | Limit for the whole check |
| `max_requests` | number | `60` | Most requests one run sends. Must be positive |
| `max_input_chars` | number | `1500000` | Most characters of state and questions one run sends. Must be positive |

### `mutation`

For `go-mutation`, which runs only on a `dagger` environment. See [mutation testing](../mutation.md#configure).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `base` | string | `GITHUB_BASE_REF`, else `main` | Branch whose merge base defines the changed files. Not allowed with `scope: "module"` |
| `scope` | string | `"changed"` | `changed` mutates the files the branch changed; `module` mutates every eligible file in the target |
| `accepted` | string | `".levenshtein/mutation-accepted.json"` | Repository-relative accepted-survivors file, inside the target's `inputs`. A missing file accepts nothing |
| `tags` | string | none | Build tags for gremlins and the tests, as one comma-separated list without spaces |
| `timeout` | duration string | `"20m"` | Limit for the whole check |

### `lint`

For `go-lint`, on either executor. See [lint selection](../configuration.md#lint-selection).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `checks` | array of strings | none | Patterns appended to the shipped rule selection, where the last match wins: an optional `-`, then `all`, `*`, a rule name such as `gocognit`, or a name ending in `*`. A pattern containing `_`, such as `errs_nopanic`, selects [community rules](../community-rules.md#selection). A pattern that matches no rule makes the check an error when it runs |
| `rule_modules` | boolean | `true` | `false` keeps the configuration's `rule_modules` out of this check |

A `lint` object needs a nonempty `checks`, a `rule_modules` setting, or both.

### `imports`

For `go-imports`, on either executor, where it is required. See [import boundaries](../check-kinds-guide.md#import-boundaries).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `rules` | array of [import rules](#import-rules) | required, nonempty | The layering rules the check enforces |

#### Import rules

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `packages` | array of strings | required, nonempty | Package patterns relative to the target directory: `.` or starting with `./`, where `...` matches any path. A pattern that matches no package makes the check an error when it runs |
| `deny` | array of strings | one of `deny` and `allow` | Import path patterns the packages may not import |
| `allow` | array of strings | one of `deny` and `allow` | The only import path patterns the packages may import; `deny` wins where both match |
| `tests` | string | `"include"` | `include` also judges `_test.go` files; `exclude` does not |
| `reason` | string | required | Why the boundary exists, repeated in every finding |

An import path pattern is a full import path with optional `...` wildcards, an entry starting with `./` resolved against the target directory's import path, or `std` for the standard library.

### `apidiff`

For `go-apidiff`, on either executor. See [API compatibility](../check-kinds-guide.md#api-compatibility).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `base` | string | `GITHUB_BASE_REF`, else `main` | Branch whose merge base the exported API is compared with |

## `runs`

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `checks` | array of strings | required, nonempty | Check IDs to run. An ID selects every target of a check; `<check>/<target>` selects one target of a check declared with `targets`. The same planned check twice is an error |
| `rerun_checks` | boolean | `false` | Execute every check fresh instead of reusing a cached result, while keeping dependency and build caches. Scripts see it as `LEVENSHTEIN_RERUN_CHECKS` |

Any name can be a run. With a configuration file, only the runs it declares exist, and `main` is not special.

## Preparations and builds

Entries of `preparations` and `builds` share one shape. A `command` check names at most one of each, and the stage runs in the target's `workspace` before the check. See [local caching](../configuration.md#local-caching).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `command` | array of strings | required, nonempty | The program and its arguments |
| `inputs` | array of strings | required, nonempty | Repository-relative paths whose content keys the stage |
| `outputs` | array of strings | required, nonempty | Repository-relative paths the stage produces; they are left out of source fingerprints and must not be symlinks |
| `env` | object of strings | none | Variables for the stage. Same naming rules as an environment's `env` |
| `timeout` | duration string | `"5m"` | Limit for the stage |

## `rule_modules`

Keyed by Go module path. See [community lint rules](../community-rules.md#configuration).

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `version` | string | required | An exact tag or pseudo-version, such as `v1.4.0`; branches and `latest` are errors |
| `namespace` | string | required | The module's `Namespace`: lowercase letters only. `lvrules`, `example`, `levenshtein`, and `lv` are reserved |
| `select` | array of strings | required, nonempty | Patterns for the rules every `go-lint` check runs from this module, in its own namespace only, such as `errs_*` or `-errs_wrapf` |
| `advisory` | array of strings | none | Patterns for selected rules that report without failing the check. A literal rule name no `go-lint` check selects is an error |
| `settings` | object of objects of strings | none | Flag values for each rule, keyed by rule code and then by the analyzer's flag name |

Rule modules run only on Dagger; a native `go-lint` check skips them with a warning. `go-http` and `go-sql` never run them.

## `baseline`

A repository-relative path, such as `".levenshtein/baseline.json"`, that is not `"."` and is not a private path such as `.env` or anything under `.git`. A missing file records nothing. See [baseline](../configuration.md#baseline) for the file format and how findings match.
