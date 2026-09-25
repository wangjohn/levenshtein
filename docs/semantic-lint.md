# Semantic lint

`semantic-lint` asks TypeSafe's Jev model bounded questions about the change on your branch and reports the answers as advisory findings. Jev is a System One model: given a state and typed questions, it returns probabilities in one pass. It does not write text or code, so it cannot rewrite anything. Code selects what to judge, phrases each question, and owns every threshold.

**Status:** advisory pilot. Findings never change the check outcome. The result record keeps every raw judgment so thresholds can be calibrated against real pull requests before any question gates a merge.

## What it judges

Code preselects each item from the diff. The model answers one question per item, in parallel, and the answer is a probability. A question fires when the probability crosses its threshold in the listed direction. Questions about quality are phrased in the positive and fire on a low probability, which avoids the model's weakness with negation.

| Question | Scope | Preselected by code | Fires |
| --- | --- | --- | --- |
| `comment_explains_why` | Go hunk | Each added statement comment; doc comments and directives are excluded | probability ≤ 0.4 |
| `error_message_actionable` | Go hunk | Each added `fmt.Errorf` or `errors.New` call | ≤ 0.4 |
| `duplicates_package_helper` | Go hunk | Declarations with three or more added lines, compared against package function signatures | ≥ 0.7 |
| `evaluative_wording` | Markdown hunk | Hunks that add prose outside code fences and tables | ≥ 0.8 |
| `unscoped_guarantee` | Markdown hunk | Each added sentence containing always, never, cannot, guaranteed, complete, or immutable | ≥ 0.75 |
| `scope_creep` | Change | Changes with two or more files or commits; a three-level rubric | score ≥ 1.5 with confidence ≥ 0.6 |
| `commit_subject_matches` | Change | Each non-merge commit since the base | ≤ 0.4 |
| `behavior_change_undocumented` | Change | Changes touching source, tests, configuration, or workflows | ≥ 0.7 |
| `describes_unimplemented` | Change | Changes touching Markdown | ≥ 0.7 |

Severity orders the report: `important`, then `minor`, then `nit`. The exact wording, criteria, and thresholds live in `internal/semantic/catalog.go` under a catalog version. Change the version when editing them so recorded judgments stay comparable. Version 0.1 also asked about test block structure, function responsibilities, and boolean parameters; a first live run showed those were style opinions with a fuzzy middle, so 0.2 dropped them.

Mechanical conventions belong in analyzers, not here. Spacing, struct field layout, typed choices, and struct construction are enforced by [the shared Go lint rules](checks.md#go-lint-rules).

## How it works

1. **Diff.** The check resolves the configured base branch (as `origin/<base>`, unless the local branch is the same commit or ahead of it, or no `origin/<base>` exists), takes the merge base with `HEAD`, and diffs the working tree against it with zero context. Untracked Go and Markdown files count as fully added. Deleted files contribute only to the change summary.
2. **State.** For Go, the current file is parsed and hunks are grouped by the top-level declaration they touch. The declaration text, the lines of the diff inside that declaration, the file's test flag, the preselected items, and, for functions with three or more added lines, package-level function signatures form one state. For Markdown, the state is the hunk, its enclosing section, and the preselected sentences. The change state holds commit subjects with their files and hunk headers, a per-file summary, the Markdown diff, and a digest of added Go symbols and configuration keys.
3. **Ask.** Each state goes to the pinned model with all of its questions in one request, a few requests at a time. Oversized states drop package signatures, then truncate the declaration text, to stay inside the model's context.
4. **Compose.** Answers are matched back to their question and location. Every judgment is recorded. Findings are the ones that crossed their threshold.

The check passes whenever every question received an answer. It is `incomplete` when the response omits a question, and `error` when the API key is missing, the base branch cannot be found (including on a shallow clone), the API is unreachable or rejects the request, or the timeout elapses. Rate limiting and overload are retried a few times. Results are never cached: the model is pinned but not bitwise deterministic, and the cost of a rerun is a fraction of a cent.

## Configure

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "inputs": ["."]}},
  "environments": {
    "host": {"executor": "native"}
  },
  "checks": {
    "review": {"kind": "semantic-lint", "target": "app", "environment": "host", "semantic": {"base": "main", "model": "jev-1.13.0"}}
  },
  "runs": {"review": {"checks": ["review"]}}
}
```

`semantic-lint` is a native check without a command. Its optional `semantic` object accepts `base`, `model` (a pinned release; aliases such as `jev-latest` are rejected), and `timeout` (default five minutes); the environment still supplies `env`, `pass_env`, and `tools`, except for the entries named under [API keys](#api-keys). The base branch is `base` when set, otherwise `GITHUB_BASE_REF` when GitHub Actions provides it for a pull request, otherwise `main`. A `semantic-lint` check cannot carry a `command` object, so args, rerun args, artifacts, preparation, build, and caching are not expressible for it. A run with `rerun_checks: true` needs no rerun args, because the check always executes.

The target's `dir` and `inputs` limit which changed files are judged. Paths under a `testdata` directory and private `.env` files are skipped. Keep the check in its own run while piloting, so ordinary runs stay offline and deterministic.

Run it locally against the current branch:

```sh
TYPESAFE_API_KEY=... ./verify semantic-lint
```

## API keys

The check reads `TYPESAFE_API_KEY` from the host process environment only. Unlike `command` checks, it needs no `pass_env` entry: the kind itself defines which variables it consumes, so a consumer only exports the variable locally or adds a CI secret.

Keeping the key out of `levenshtein.json` is enforced, not just advised. Configuration is committed, so a pull request that could declare these variables would choose where the branch's CI secret is sent. Validation rejects a check `env` or environment `env` that declares `TYPESAFE_API_KEY` or `TYPESAFE_BASE_URL`, and a `pass_env` entry naming `TYPESAFE_API_KEY`. Neither variable is ever read from configuration at run time. A `pass_env` entry for `TYPESAFE_BASE_URL` is accepted but only re-exports the host value to subprocesses; the check itself still reads the host. Committed `PATH` and `GIT_*` entries are rejected as well, because they would decide which `git` produces the diff.

In GitHub Actions, supply it from a repository secret and skip the step when the secret is absent, which is the case for pull requests from forks:

```yaml
    env:
      TYPESAFE_API_KEY: ${{ secrets.TYPESAFE_API_KEY }}
    steps:
      - uses: actions/checkout@... # pinned
        with:
          fetch-depth: 0
      - name: Advisory semantic lint
        if: env.TYPESAFE_API_KEY != ''
        run: ./levenshtein/verify review --source ./app
```

`fetch-depth: 0` makes the base branch available for the merge base; on a shallow checkout the check fails with a message that says so. Levenshtein's own workflow does this in its `semantic-lint` job and writes the summary to the job's step summary.

`TYPESAFE_BASE_URL` overrides the API origin for a proxy or a recording server and is read from the host process the same way. It must use `https`, so the bearer token is not sent in the clear; plain `http` is accepted only for the loopback hosts `127.0.0.1`, `::1`, and `localhost`, which keeps local test servers and recorders usable.

What leaves the machine: the diff hunks, the enclosing declarations or sections, package function signatures, commit subjects, and file paths. Treat this as sending source to a third party and review TypeSafe's data terms before enabling it on a private repository.

## Limits

- Only Go and Markdown files produce hunk questions. Renames appear as a deletion plus an addition.
- A state is capped near the model's 32k-token budget. Very large declarations are truncated and package signatures are dropped first.
- The model reads questions literally, degrades when the state is padded with irrelevant text, and cannot count or compare dates. Text inside the diff can influence answers, so do not gate on findings for changes from untrusted authors.
- Calibration is a population property. A single probability is not a verdict, and TypeSafe publishes no accuracy figures for code review. The vendor's own workflow evaluation lands around 68 percent agreement with frontier-model labels.
- Repeated runs return similar, not identical, probabilities. Findings near a threshold can flip between runs.

## Calibrating thresholds

The result's `details.judgments` array holds every question, location, probability, confidence, and whether it fired. Collect these across a few dozen pull requests, label the fired ones as useful or noise, and move thresholds per question. Keep the model pinned while doing so, and log the `served_model` field from each result. When wording or thresholds change, bump the catalog version so old judgments are not mixed with new ones.

## Extensions

Candidate extensions, in rough priority order. None are scheduled; each becomes work once real pull requests show the need.

- **Cross-package duplicate detection.** `duplicates_package_helper` compares new code only with signatures from the same directory. The extension follows the retrieve-then-judge pattern: one `go/ast` pass indexes every non-test, non-generated function in the source with its signature, doc line, body, and identifiers; code ranks candidates for each new function by shared identifiers, parameter and return types, and length; the top five are sent with bodies capped near 1.5k characters; and one Noul per candidate asks whether the new function performs the same operation. Findings then name the existing function to call. Near-identical clones stay with a deterministic clone detector; the model earns its place on semantic duplicates after code has narrowed the field.
- **Trim the change-level state for very large pull requests.** `fit` drops package signatures, declaration text, and the Markdown diff, but not the `files` and `commits` lists. A change touching several hundred files could exceed the state cap and fail as an infrastructure error. Keep hunk headers for the first files and replace the rest with a count.
- **A review band around each threshold.** Report judgments within 0.1 of a Noul threshold under an `uncertain` heading rather than as findings or silence, matching the confidence floor Score questions already have. Run-to-run drift on this repository averaged 0.02 with a worst case near 0.2, so findings at the threshold can flip between runs.
- **Per-question calibration summary in check output.** Print judged, fired, uncertain, and the probability range for each question so every run produces its own calibration record without opening the JSON.
- **A labels file.** Check in hand labels of fired and near-miss judgments, keyed by catalog version, question, and location, so thresholds move on evidence and questions with stable precision can be promoted to analyzers.
- **Record and replay.** Store request and response pairs on disk so fixtures and calibration runs need no network and no key, and so a catalog change can be replayed against recorded states before it is asked live.
- **Per-question gating.** A `gate` flag that lets one question fail the check once its precision is known, while the rest stay advisory.
- **Defaults for unconfigured repositories.** The no-configuration defaults are Dagger-only, so a repository without `levenshtein.json` cannot run the check today.
- **A first-party skip when the key is absent.** Today the CI conditional is the consumer's job; a documented skip outcome would let the check sit in a shared run without failing forks.

## Future: separate module

Everything else in the CLI is offline and deterministic; this check is neither. It reaches a third-party API over the network, its answers vary between runs, and it carries a vendor's wire format into a repository that otherwise depends on nothing. Moving `internal/semantic` into its own module would let the vendor coupling version separately and keep the core free of it.

The adapter surface is already small: the `native.go` dispatch on the check kind, `validateSemanticLint`, and the `base`, `model`, and `timeout` options on `Check`. The split is deferred until calibration settles, because the catalog, the thresholds, and the state shape are still moving and a module boundary would make each change a two-repository edit.
