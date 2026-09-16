# Levenshtein

**An opinionated way to make your repos testable and lintable.**

Tests and lint rules give coding agents clear requirements and fast feedback. Levenshtein makes them easy to set up and improve, so teams can verify AI-generated changes before shipping to production.

Levenshtein keeps shared rules, test commands, and CI checks in one repo. Your application repos use a pinned version of that setup. Improve it once, then adopt the change wherever it applies.

It works with your existing CI: the CI provider runs the jobs, and Levenshtein supplies the shared checks and configuration.

For example, add a Go rule that catches misplaced `defer` calls, then adopt it across your Go repos. Fixes to shared tests and CI checks follow the same path.

## Usage

**First working slice:** shared Go lint for misplaced `defer`/`Close` calls, with Dagger and pinned tools. [Set it up](docs/setup.md).

```sh
./verify              # fast checks for your branch
./verify pre-merge    # key checks before merging
./verify main         # fresh audit; CI runs this daily
./verify go-lint      # shared Go rules, such as misplaced defers
```

`./verify` defaults to `branch`. Add your own named runs through configuration. Run the same checks locally and in CI.

To check another Go repo: `./verify go-lint --source /path/to/repo`.

Shared application tests, adoption across repos, and background agents that propose improvements are next steps in the pilot.

## Documentation

- [Setup and usage](docs/setup.md): run the Go checks locally and in CI.
- [Implementation plan](docs/implementation.md): what we are building first and how.
- [Design notes](docs/design-notes.md): optional ideas for later.
