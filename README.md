# Levenshtein

**An opinionated way to make your repos testable and lintable.**

Tests and lint rules give coding agents clear requirements and fast feedback. Levenshtein makes them easy to set up and improve, so teams can verify AI-generated changes before shipping to production.

Levenshtein keeps shared verification rules and execution setup in one repo. Your application repos use a pinned version. Improve it once, then adopt the change wherever it applies. Application tests stay beside the application code.

Your existing CI—GitHub Actions, CircleCI, or another provider—owns triggers, schedules, and workers. Levenshtein wraps the checks: select what to run, prepare the environment, and return results. The same wrapper runs locally.

For example, add a Go rule that catches misplaced `defer` calls, then adopt it across your Go repos. Fixes to shared tests and CI checks follow the same path.

## Usage

**First working slice:** shared Go lint for misplaced `defer`/`Close` calls, with Dagger and pinned tools. [Set it up](docs/setup.md).

```sh
./verify              # fast checks for your branch
./verify pre-merge    # key checks before merging
./verify main         # fresh audit; schedule this in your repo's CI
./verify go-lint      # shared Go rules, such as misplaced defers
```

`./verify` defaults to `branch`. Add your own named runs through configuration. Run the same checks locally and in CI.

To check another Go repo: `./verify go-lint --source /path/to/repo`. [Use it from your existing CI](docs/consumer-ci.md).

**Next:** validate the shared interface with Rust/Python fixtures, then wrap Benchplan's existing Swift checks through a common interface for container and native execution, with aggressive caching for fast repeated runs. Levenshtein's own workflow and daily schedule check its runner and fixtures.

## Documentation

- [Setup and usage](docs/setup.md): run the Go checks locally and in CI.
- [Configuration](docs/configuration.md): standalone planning, targets, and runs.
- [Implementation plan](docs/implementation.md): what we are building first and how.
- [Design notes](docs/design-notes.md): cache contracts and ideas for later.

See [Go lint rules](docs/go-lint.md) for the shared defer/close, typed-choice, and value-record policies.
