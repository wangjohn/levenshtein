# Contributing

## Build and test locally

Root module:

```sh
GOTOOLCHAIN=local go build ./... && GOTOOLCHAIN=local go test ./...
```

Go lint policy analyzers:

```sh
(cd runner/lint && GOTOOLCHAIN=local go test ./...)
```

The runner's Dagger module needs its generated SDK before its own tests run. Run `dagger develop` from the repository root, where `dagger.json` lives:

```sh
dagger develop --compat=skip
(cd runner && GOTOOLCHAIN=local dagger run go test ./...)
```

`GOTOOLCHAIN=local` keeps Go from downloading another toolchain when the host version differs from `.go-version`; the launcher enforces the exact version.

Repo self-checks, matching what CI runs:

```sh
./verify pre-merge
```

`./verify` needs a Docker-compatible container runtime (Docker Desktop or Colima) and the pinned Dagger CLI. Install the CLI with:

```sh
./scripts/install-dagger
export PATH="$HOME/.local/bin:$PATH"
```

See ["Develop the shared checks"](docs/setup.md#develop-the-shared-checks) for the full local development recipe, including the broader `dagger develop` / `go test -race` / `./scripts/test-consumers` sequence CI runs.

## Optional local hooks

`lefthook.yml` describes a `pre-commit` that runs `gofmt` over staged Go files
and `go build ./...`, and a `pre-push` that runs `go test ./...`. Nothing
installs them for you:

```sh
brew install lefthook
lefthook install     # lefthook uninstall to stop
```

They are a convenience, not a gate: CI runs the same checks either way.

## Code style

Follow the conventions in [AGENTS.md](AGENTS.md) (spacing, struct literals, typed choices for finite values). Run the shared [Go lint rules](docs/go-lint.md) (`./verify go-lint`) when changing Go code.

## Pull requests

- Keep PRs small and focused on one change.
- Add or update tests for any behavior change.
- Keep docs in sync with the code they describe; update the relevant file under `docs/` in the same PR.
