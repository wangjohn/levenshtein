# FAQ

## Levenshtein or golangci-lint?

Both run many Go analyzers in one pass, and several of Levenshtein's rules are the same upstream analyzers golangci-lint ships. They differ in who chooses the rules, how the tools are pinned, and what else runs beside lint.

| | Levenshtein | golangci-lint |
| --- | --- | --- |
| **Who picks the rules** | Levenshtein does. Nearly all of Staticcheck is on, plus a curated set of upstream analyzers and go-critic checks, and [rule selection](rule-selection.md) gives the evidence for turning it on or leaving it off. A repository can [add or remove patterns](configuration.md#lint-selection) but starts from the same set as every other repository | You do. A small set of linters is on by default, and each repository enables more of its catalog in `.golangci.yml` |
| **How the tools are pinned** | One Levenshtein revision pins Go, the container image, Staticcheck, every analyzer, and every other tool. Checks run in a pinned container through Dagger, or natively on the host, whose Go should be the pinned version | You pin the golangci-lint version; it runs on whatever Go and host it is given |
| **Sharing across repositories** | Every repository pins the same shared checkout, so a rule change is made once and picked up when each repository bumps its pin | Each repository keeps its own configuration file |
| **Adopting on existing code** | A checked-in [baseline](configuration.md#baseline) records existing findings; new ones fail and the file only shrinks | Reports only issues new since a revision or in a patch (`--new-from-rev`, `--new-from-patch`) |
| **Output** | JSON, text, GitHub annotations, and SARIF ([formats](configuration.md#output-formats)) | Many formats, SARIF among them |
| **Caching** | Whole-check verdicts are cached by a fingerprint of the declared inputs, so an unchanged check is skipped without starting its tools | Per-package analysis results are cached |
| **Beyond lint** | [Other kinds](check-kinds.md) run beside it under the same pins: `go vet`, module tidiness, `govulncheck`, tests, mutation testing, API compatibility, secrets, workflow audits, shell scripts, and non-Go lockfile vulnerabilities | Lint and formatting |
| **Fixes** | Never changes files; some findings carry a [hint](configuration.md#fix-hints) with the fix command | `--fix` applies the fixes linters suggest, and it can run formatters |
| **Platforms** | Linux and macOS | Linux, macOS, and Windows, with editor integrations |

golangci-lint is the better choice when you have one repository and are happy choosing and tuning linters yourself, when you need Windows, editor integration, or automatic fixes, or when you want a linter its catalog has and Levenshtein does not ship. Levenshtein is for several repositories that should share one reviewed rule set and one set of tool versions, with the same verdict locally, in CI, and for coding agents. The two can run side by side, and a rule Levenshtein lacks can also come from a [community rule module](community-rules.md).

## Does Levenshtein change my files?

No. `verify` reads the source and writes a report. A finding with a mechanical fix carries a [hint](configuration.md#fix-hints), such as `gofmt -w <file>`, for you or an agent to run. The shared checks never write to the repository; the one file `verify` writes there is the baseline, and only when you pass `--write-baseline`. Your own `command` checks run your scripts, which can write whatever they write.

## Do I need Docker?

Only for checks bound to a Dagger environment, which is every check of a repository without a `levenshtein.json`. Bind the checks to a [native environment](configuration.md#native-go-checks) to run them on the host's Go instead. `go-http`, `go-sql`, and `go-mutation` still need Dagger, and community rule modules run only there.

## Does it work for languages other than Go?

The lint rules are for Go. `workflow-lint` and `workflow-security` check GitHub Actions, `shell-lint` shell scripts, `secrets` any file, and `deps-vuln` npm, Python, Rust, Ruby, and other lockfiles. Anything else, such as another language's linter or test suite, runs as a [`command` check](configuration.md#native-commands) beside them.
