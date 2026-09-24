## Checks

This repository's lint rules and checks run through [Levenshtein](https://github.com/wangjohn/levenshtein), pinned in `../levenshtein`. Adjust the path if your checkout differs.

- Before you finish a change, run `../levenshtein/verify branch --source . --format text` and fix everything it reports. Run `pre-merge` the same way before asking for review. Exit status 0 means every check passed, 1 means a check failed or did not finish, and 2 means the command or `levenshtein.json` is wrong.
- Each finding is one line, `file:line:col: CODE message`, with paths relative to the repository root. A check that wraps a whole tool, such as `go-vet`, prints `dir: CODE` and then the tool's own output, indented. The last lines give each check's status.
- An indented `hint:` line under a finding names a known mechanical fix, such as `gofmt -w file`. Apply it yourself: Levenshtein never changes files.
- Fix the code rather than silencing a rule. When a finding is a deliberate exception, put `//lint:ignore CODE reason` on the line above it, with a reason that says why. Do not turn rules off in `levenshtein.json`.
- `.levenshtein/baseline.json` lists findings that existed before this repository adopted the rules. Never add entries to it to make a run pass. When you fix a baselined finding, the run fails with a `baseline-stale` finding that points at its entry: delete the entry, or lower its `count`, in the same change.
- `../levenshtein/verify branch --source . --format text --no-baseline` lists every finding, baselined ones included, when you are asked to pay down that debt.
