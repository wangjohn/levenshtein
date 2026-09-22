# Mutation testing

`go-mutation` makes small, deliberate bugs in the Go code a branch changed and runs the tests against each one. A test that still passes when the code is wrong is not testing that behavior. The check fails for each such surviving mutant on a line the branch changed, naming the file, line, and kind of change.

It runs pinned [gremlins](https://github.com/go-gremlins/gremlins) v0.6.0 inside Dagger. Levenshtein chooses the files, reads gremlins' JSON report, and decides the outcome itself; gremlins' own thresholds and `--diff` mode are not used (see [Known gremlins defects](#known-gremlins-defects)).

## What a run does

1. **Choose files on the host.** The CLI finds the merge base with the base branch and lists files the working tree adds or modifies, plus untracked files git does not ignore. For each modified file it also records which lines the change wrote, from a zero-context diff. It keeps `.go` files in the target's module that the target's `inputs` cover. It drops test files, `testdata` and `vendor` directories, files matched by the target's `exclude`, files in a nested module, and files with a generated-code header. With nothing left, the check passes with "no Go files to mutate" and no Dagger session starts.
2. **Mutate in Dagger.** The runner builds gremlins from `runner/tools`, then runs it once from the module root with two workers and a timeout coefficient of 10. Every Go file outside the selection is excluded with one pattern per directory. Gremlins changes one operator at a time, such as `<` to `<=`, `<` to `>=`, `+` to `-`, `++` to `--`, or `-n` to `n`, and runs that package's tests against each change.
3. **Decide.** The runner reads the report and applies the rules below.

Only tests in the mutated file's own package count as coverage. Code exercised only by another package's tests is reported as not covered.

## Outcomes

| Condition | Outcome |
| --- | --- |
| Gremlins exits nonzero, logs `ERROR:`, or writes no report without saying it found nothing to mutate | **error** |
| The report is invalid, names a file that was not selected, or uses a status the runner does not know | **error** |
| Some mutants timed out and none were killed | **incomplete**, with a `go-mutation-timeout` finding per timed-out mutant |
| A mutant survived on a changed line and no accepted entry matches it | **failed**, with a `go-mutation` finding |
| An accepted entry for a mutated file matches no mutant | **failed**, with a `go-mutation-stale` finding at the entry |
| Otherwise | **passed** |

Gremlins mutates whole files, so it also finds survivors on lines the branch did not touch. Those are gaps the branch inherited: they are listed under `unchanged` in `details.summary`, counted as `unchanged_survivors`, and do not fail the check. In a trial on ten of this repository's pull requests, 26 of 283 survivors were on changed lines. A survivor on a changed line is judged even when the rest of the file is old. An untracked file, and every file in `scope: "module"`, counts all of its lines as changed. A modified file whose change only deletes lines has no changed lines. The tradeoff: a change can cause a survivor elsewhere, for example by deleting the only assertion that covered an old function, and that survivor is listed rather than failed.

A timed-out mutant counts as caught, as it does in PIT and Stryker: a mutation that makes the code hang, such as a worker count of zero, is one the tests noticed. Timed-out mutants are listed under `timed_out_mutants`. When mutants time out and none are killed, the machine is the likelier cause than the code, so that run is incomplete rather than passed.

Code no test reaches is listed under `uncovered` and does not fail the check. Not viable and skipped mutants are counted only. The run's summary, with every count and list, is in `details.summary` whether the check passed or not.

## Configure

```json
{
  "version": 1,
  "targets": {"app": {"dir": ".", "inputs": ["."]}},
  "environments": {"go": {"executor": "dagger"}},
  "checks": {
    "mutation": {"kind": "go-mutation", "target": "app", "environment": "go"}
  },
  "runs": {
    "pre-merge": {"checks": ["mutation"]},
    "mutation": {"checks": ["mutation"]}
  }
}
```

The optional `mutation` object accepts:

| Field | Default | Meaning |
| --- | --- | --- |
| `base` | `GITHUB_BASE_REF`, else `main` | Branch whose merge base defines "changed". It is resolved locally, then as `origin/<base>`. |
| `scope` | `changed` | `module` mutates every eligible file in the target instead, and cannot be combined with `base`. |
| `accepted` | `.levenshtein/mutation-accepted.json` | Repository-relative accepted-survivors file. A missing file accepts nothing. It must be inside the target's `inputs`, because the check reads it from the Dagger source. |
| `tags` | none | Build tags for gremlins and the tests, as one comma-separated list. |
| `timeout` | `20m` | Limit for the whole check. Reaching it is an error. |

`go-mutation` runs only in a Dagger environment. The CI checkout needs the base branch, so use `fetch-depth: 0` in GitHub Actions; a shallow clone fails with a message that says so.

Each mutant runs its package's tests again, so the check costs roughly the package test time for every covered mutant, split across two workers. Put it in `pre-merge` or a run of its own rather than `branch`.

## Accepted survivors

Some mutants no test can kill. In `if n < lo { return lo }`, changing `<` to `<=` still returns `lo` when `n == lo`. List these in the accepted-survivors file with a reason:

```json
{
  "version": 1,
  "accepted": [
    {
      "file": "clamp.go",
      "mutator": "CONDITIONALS_BOUNDARY",
      "line": "if n < lo {",
      "reason": "n == lo returns lo either way"
    }
  ]
}
```

`file` is relative to the target's module directory. `mutator` is the gremlins mutator name shown in the finding. `line` is the mutated source line with surrounding whitespace trimmed; entries match on this text, not the line number, so edits elsewhere in the file do not break them. `reason` is required. Unknown fields and any version other than 1 are rejected.

An entry becomes stale when its file was mutated but no mutant of that kind is on a line with that text any more. The check reports stale entries so the file does not keep exceptions that no longer apply. Entries for files a run did not mutate are not judged.

## Caching

The CLI does not reuse a `go-mutation` result, because which files are mutated depends on where the base branch points, and the input fingerprint does not see that. Dagger still caches the function call, so a rerun with the same source, file list, and changed lines returns immediately. A run with `rerun_checks` passes a fresh nonce and executes again.

## Known gremlins defects

These were reproduced with gremlins v0.6.0 on Go 1.27.1, and the design avoids each of them:

- `--threshold-efficacy` and `--threshold-mcover` are ignored as flags. As configuration they work, but compare with `<=`, so a 100% threshold fails even when every mutant is killed.
- Per-mutant time limits scale from the coverage run's duration. On a warm build cache, the default coefficient of 3 timed out every mutant in four of five runs. A coefficient of 10 kept repeated runs identical.
- `--diff` needs `.git`, which Dagger does not receive. With a path argument it skips the changed files, and an empty diff mutates the whole module.
- The GitHub `v0.6.0` tag was moved after release. The module proxy's `v0.6.0` is what `runner/tools/go.sum` locks.

Cross-package coverage (`--coverpkg`) and the optional mutators, such as `invert-logical`, are off; revisit them with data from real pull requests.
