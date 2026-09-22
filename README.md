# Levenshtein

**An opinionated way to make your repos testable and lintable.**

Tests and lint rules give coding agents clear requirements and fast feedback. Levenshtein makes them easy to set up and improve, so teams can verify AI-generated changes before shipping to production.

Levenshtein keeps shared verification rules and execution setup in one repo. Your application repos use a pinned version. Improve it once, then adopt the change wherever it applies. Application tests stay beside the application code.

Your existing CI—GitHub Actions, CircleCI, or another provider—owns triggers, schedules, and workers. Levenshtein wraps the checks: select what to run, prepare the environment, and return results. The same wrapper runs locally.

For example, add a Go rule that catches misplaced `defer` calls, then adopt it across your Go repos. Fixes to shared tests and CI checks follow the same path.

## Usage

**Available now:** shared Go lint, vet, resource and vulnerability checks, workflow lint, native commands, an advisory model-backed semantic lint, and local caching of results and compatible setup/builds. [Set up Go checks](docs/setup.md) or [configure native checks](docs/configuration.md).

```sh
./verify              # fast checks for your branch
./verify pre-merge    # key checks before merging
./verify main         # fresh audit; schedule this in your repo's CI
./verify go-lint      # shared Go rules, such as misplaced defers
./verify semantic-lint # advisory Jev review of the branch; needs TYPESAFE_API_KEY
```

`./verify` defaults to `branch`. In version 1 configuration, fresh audits explicitly set `rerun_checks: true`. Add your own named runs through configuration. Run the same checks locally and in CI.

To check another Go repo: `./verify go-lint --source /path/to/repo`. [Use it from your existing CI](docs/consumer-ci.md).

## Documentation

- [Setup and usage](docs/setup.md): run the Go checks locally and in CI.
- [Contributing](CONTRIBUTING.md): how to build, test, and send changes.
- [Security policy](SECURITY.md): how to report a vulnerability privately.
- [Configuration](docs/configuration.md): standalone planning, targets, and runs.
- [Semantic lint](docs/semantic-lint.md): advisory Jev questions about Go, docs, and pull request shape.
- [Use from your existing CI](docs/consumer-ci.md): local and CI examples for application repos.
- [Release archives](docs/releases.md): package the CLI and shared checks with GoReleaser.
- [Language fixtures](docs/language-fixtures.md): Rust/Python compatibility checks.
- [Architecture](docs/architecture.md): how the CLI plans, executes, and caches checks.
- [Dependency choices](docs/dependencies.md): which responsibilities use established tools.
- [Design notes](docs/design-notes.md): cache and language adapter contracts.
- [Roadmap](docs/roadmap.md): planned work beyond the current release.

See [Go lint rules](docs/go-lint.md) for the shared defer/close, typed-choice, and value-record policies.

## License

[MIT](LICENSE)
