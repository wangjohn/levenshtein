# Contributing

## Build and test locally

Root module:

```sh
go build ./... && go test ./...
```

Go lint policy analyzers:

```sh
cd runner/lint && go test ./...
```

The runner's Dagger module needs its generated SDK before its own tests run:

```sh
cd runner && dagger develop --compat=skip && dagger run go test ./...
```

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

## Code style

Follow the conventions in [AGENTS.md](AGENTS.md) (spacing, struct literals, typed choices for finite values). Run the shared [Go lint rules](docs/go-lint.md) (`./verify go-lint`) when changing Go code.

## Pull requests

- Keep PRs small and focused on one change.
- Add or update tests for any behavior change.
- Keep docs in sync with the code they describe; update the relevant file under `docs/` in the same PR.
