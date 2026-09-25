# Check kinds

Every check in `levenshtein.json` has a `kind`. This table is the one place
that lists every kind and what it supports; other documents link here rather
than repeat it. It is generated from the kind descriptors in
`internal/verify/kinds.go`, and a test fails when the two disagree. After
changing a descriptor, regenerate it with:

```sh
(cd internal/verify && GOTOOLCHAIN=local go test -run TestCheckKindsDoc -update)
```

- **Executors**: which `executor` an environment may name to run the kind
  ([configuration](configuration.md)).
- **Results cached**: whether a verdict is reused while the check's inputs and
  tooling are unchanged, and why not where it is never reused.
- **Baseline**: whether a [baseline](configuration.md#baseline) file can hold
  the kind's findings.
- **Target**: whether the kind checks the whole repository and so needs a
  target whose `dir` is `"."`.
- **Default runs**: which gate runs of the defaults a repository without
  `levenshtein.json` gets include the kind. Every kind in the defaults also
  has a run of its own named after it; `main` is a fresh run.

<!-- check-kinds:start -->
| Kind | What it checks | Executors | Results cached | Baseline | Target | Default runs |
| --- | --- | --- | --- | --- | --- | --- |
| `go-lint` | Staticcheck, the curated upstream analyzers and Levenshtein's own LV rules, plus any configured community rule modules on Dagger | dagger, native | Yes | Yes | Any | `branch`, `pre-merge`, `main` |
| `go-vet` | The pinned Go toolchain's default vet checks | dagger, native | Yes | No | Any | `branch`, `pre-merge`, `main` |
| `go-mod` | `go mod tidy -diff` and `go mod verify`: manifests tidy, downloads matching `go.sum` | dagger, native | Never: go mod verify always checks the current module cache | No | Any | `branch`, `pre-merge`, `main` |
| `go-test` | `go test -race ./...` on the pinned toolchain | dagger, native | Yes | No | Any | Its own run only |
| `go-http` | bodyclose alone: HTTP response bodies are closed | dagger | Yes | Yes | Any | Its own run only |
| `go-sql` | sqlclosecheck alone: database rows and statements are closed | dagger | Yes | Yes | Any | Its own run only |
| `go-vuln` | govulncheck: reachable known vulnerabilities in Go dependencies | dagger, native | Never: vulnerability scans always query current advisory data | No | Any | `main` |
| `workflow-lint` | actionlint: GitHub Actions syntax and expressions | dagger, native | Yes | No | Repository root | Its own run only |
| `workflow-security` | zizmor's offline audits of workflows, composite actions and Dependabot configuration | dagger, native | Yes | No | Repository root | Its own run only |
| `shell-lint` | ShellCheck's warnings and errors in the repository's shell scripts | dagger, native | Yes | Yes | Repository root | Its own run only |
| `secrets` | gitleaks' default rules over the repository's files, every value redacted | dagger, native | Yes | No | Repository root | Its own run only |
| `deps-vuln` | osv-scanner: known vulnerabilities in non-Go dependency lockfiles | dagger, native | Never: vulnerability scans always query current advisory data | No | Repository root | Its own run only |
| `self-test` | Levenshtein's own good and bad fixtures, for developing the shared checks | dagger | Yes | No | Any | Not in the defaults |
| `command` | A command the repository declares, run as a trusted host process | native | Only with `command.cache` | No | Any | Not in the defaults |
| `semantic-lint` | Advisory Jev judgments about Go comments, errors, tests, docs and PR shape | native | Never | No | Any | Not in the defaults |
| `go-mutation` | gremlins mutation testing of the Go files a branch changed | dagger | Never by the CLI: the mutated files depend on the base branch; Dagger reuses identical runs | No | Any | Not in the defaults |
| `go-imports` | The repository's layering rules: which of its packages may import which | dagger, native | Yes | Yes | Any | Not in the defaults |
| `go-generate` | `go generate ./...` in a scratch copy: every file it would add, change or delete | dagger, native | Yes | No | Any | Its own run only |
| `go-apidiff` | apidiff between the branch's merge base and the working tree: incompatible exported API changes | dagger, native | Never by the CLI: the comparison depends on where the base branch points; Dagger reuses identical runs | No | Any | Its own run only |
<!-- check-kinds:end -->
