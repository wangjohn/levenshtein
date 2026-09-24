# Coding agents and templates

Coding agents fix what their tools report. Levenshtein reports; it never changes a file. These templates connect the two in a consumer repository: the agent runs the same checks CI runs, reads findings in a form made for it, and cannot finish a turn while the fast run fails.

The templates live in [`templates/`](../templates) in this repository. They are files to copy into your own repository, not files Levenshtein reads, which is why they sit apart from `examples/`, whose rule module this repository builds and tests.

| Template | Copy to | Purpose |
| --- | --- | --- |
| [`levenshtein.json`](../templates/levenshtein.json) | `levenshtein.json` | A starter configuration for one Go module at the repository root |
| [`agents-snippet.md`](../templates/agents-snippet.md) | Your `AGENTS.md` or `CLAUDE.md` | Tells an agent how to run the checks and read their findings |
| [`claude/settings.json`](../templates/claude/settings.json) | `.claude/settings.json` | Registers the two Claude Code hooks below |
| [`claude/hooks/levenshtein-stop.sh`](../templates/claude/hooks/levenshtein-stop.sh) | `.claude/hooks/levenshtein-stop.sh` | Blocks finishing while the fast run fails |
| [`claude/hooks/levenshtein-gofmt.sh`](../templates/claude/hooks/levenshtein-gofmt.sh) | `.claude/hooks/levenshtein-gofmt.sh` | Reports an unformatted Go file right after the edit |
| [`github/workflows/levenshtein.yml`](../templates/github/workflows/levenshtein.yml) | `.github/workflows/levenshtein.yml` | The GitHub Action with annotations and an optional code scanning upload |

The layout the templates assume is the one [consumer CI](consumer-ci.md) describes: your repository and a pinned Levenshtein checkout side by side.

```text
workspace/
  app/          # your repository, where the templates go
  levenshtein/  # Levenshtein at a pinned release
```

## The starter configuration

`levenshtein.json` declares one target, `app`, covering the whole repository, and runs every check on the host's Go, so neither an agent's machine nor CI needs a container runtime. `branch` runs `go-lint` and `go-vet`, `pre-merge` adds `go-mod`, and `main` adds `go-vuln` and runs fresh. Change `dir` and `inputs` for a module that is not at the root; see [configuration](configuration.md). Native checks need the Go version Levenshtein pins, as [native Go checks](configuration.md#native-go-checks) explains.

It also names `.levenshtein/baseline.json` as its [baseline](configuration.md#baseline). Until that file exists nothing is baselined and every finding fails. To adopt the rules in a repository that already has findings, write it once and commit it:

```sh
../levenshtein/verify main --source . --write-baseline
```

## Instructions for the agent

`agents-snippet.md` is a section to paste into the instructions file your agent reads (`AGENTS.md`, or `CLAUDE.md` for Claude Code). It tells the agent to run `verify branch --source . --format text` before finishing, how to read a finding and its hint, to suppress a finding only with `//lint:ignore CODE reason`, and never to add baseline entries. Change the `../levenshtein` path if your checkout is elsewhere.

`--format text` is the format for agents and people: one line per finding, `file:line:col: CODE message`, relative to the repository root, then one line per check with its status (see [output formats](configuration.md#output-formats)). A hint, when a code has a mechanical fix such as `gofmt -w`, is printed below its finding; the agent applies it, because Levenshtein does not.

## Claude Code hooks

Claude Code runs [hooks](https://code.claude.com/docs/en/hooks) at points in a session. Copy both scripts into `.claude/hooks/`, keep them executable, and merge `claude/settings.json` into `.claude/settings.json`. Both scripts need `bash`; they read the hook's input with `jq` and do less without it, as described below.

### Stop: keep working while the run fails

When the agent tries to end its turn, `levenshtein-stop.sh`:

1. Does nothing and lets the agent stop when `git status` shows no uncommitted change to a `.go` file, a `go.mod`, `go.sum`, or `go.work`, `levenshtein.json`, or `.levenshtein/`. A turn that did not touch Go costs nothing. Outside a git work tree it cannot tell, so it runs.
2. Otherwise runs `$LEVENSHTEIN/verify $LEVENSHTEIN_RUN --source <repository> --format text`.
3. Exits 0 when the run passes, so the agent stops.
4. Exits 2 when the run fails, with the text report on standard error. Claude Code's contract for a `Stop` hook is that exit 2 prevents stopping and hands stderr to the model as the reason, so the agent sees every finding and keeps working.
5. Exits 1 when `verify` itself cannot run (exit 2, such as a configuration error) or the launcher is missing. Claude Code shows the user a hook error and lets the agent stop: blocking on a broken setup would only loop.

| Variable | Default | Meaning |
| --- | --- | --- |
| `LEVENSHTEIN` | `$CLAUDE_PROJECT_DIR/../levenshtein` | The pinned Levenshtein checkout |
| `LEVENSHTEIN_RUN` | `branch` | The run to execute. Keep it fast and native |
| `LEVENSHTEIN_STOP_ONCE` | unset | `1` blocks only the first attempt to stop in a turn |

Set them in your shell, or in the `env` object of `.claude/settings.json`. The repository is the git top level of the working directory Claude Code reports in the hook's input, so a session in a git worktree checks that worktree, not the checkout it started from; without `jq` it is `$CLAUDE_PROJECT_DIR`.

The hook blocks for as long as the run fails, which is the point, but an agent that cannot fix a finding keeps trying until you interrupt it. `LEVENSHTEIN_STOP_ONCE=1` trades that for a single attempt: when Claude Code reports that the turn is already continuing because of a stop hook (`stop_hook_active`), the hook lets it stop, so the agent can end with an account of what is left. The hook checks uncommitted changes only; a change the agent already committed is left to CI.

Use a `branch` run of native checks, such as the starter's `go-lint` and `go-vet`. The result cache makes a repeat run over unchanged files fast, but a check with findings runs in full every time, and a Dagger check needs a container runtime and starts slower. The hook's `timeout` is 600 seconds, Claude Code's default for a command hook.

### PostToolUse: formatting after each edit

After an `Edit`, `MultiEdit`, or `Write` of a `.go` file, `levenshtein-gofmt.sh` runs `gofmt -l` on that file and exits 2 with `Run: gofmt -w <file>` when it is not formatted, or with the parse error when it does not parse. For `PostToolUse`, exit 2 cannot undo the edit; it shows the message to the agent at once instead of at the end of the turn. It never rewrites the file. It does nothing without `jq` or `gofmt` on `PATH`.

It stops at formatting because that is the one rule a single file answers quickly. The other rules need the module's packages loaded, so they are left to the Stop hook's run.

## The GitHub Actions workflow

`github/workflows/levenshtein.yml` is the workflow from [consumer CI](consumer-ci.md#github-actions) with a code scanning upload added. It runs `branch` on pushes, `pre-merge` on pull requests, and `main` on the daily schedule; annotates failing findings on the pull request; and writes `levenshtein.sarif`, which a same-repository event uploads with `github/codeql-action/upload-sarif`. The job asks for `security-events: write` for the upload only; delete that permission, the `sarif` input, and the upload step if you do not use code scanning. Annotations need no permission and also appear on pull requests from forks.

The `annotations` and `sarif` inputs are newer than release 0.1.0, which the template pins; use a release that has them.

## Validation

This repository tests the templates: the starter configuration must parse and plan every run with native checks only (`internal/verify/templates_test.go`), the settings must parse and name both scripts, each hook runs against a stand-in launcher and a scratch git repository, the workflow must pass actionlint (`scripts/test-tool-checks`), its Levenshtein pin must name the newest release (`scripts/test-doc-pins`), and CI runs shellcheck over the scripts.
