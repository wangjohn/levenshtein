# Rust and Python compatibility fixtures

These small consumers exercise the common configuration and native executor. They are interface tests, not complete language integrations. Their application code and assertions stay beside the fixture; no Rust/Python branches were added to the planner.

## Run

Use the pinned Go version, a C linker/toolchain for Rust, `rustup`, and Python 3 to run the harness. The fixture installer downloads checksum-verified uv, then uses your existing `rustup` to install the pinned Rust toolchain and uv to install the pinned Python runtime; it does not install `rustup` itself (CI relies on the runner image for that):

```sh
./scripts/install-fixture-tools "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
./scripts/test-language-contracts
```

The harness builds the standalone CLI once, copies fixtures into temporary repositories, and starts a new CLI process for each verification. It cleans up the temporary repositories and result caches. Package/tool downloads remain in the native tools' caches. The normal Go verification path does not require these extra tools.

Pins verified September 16, 2026. Each one lives in the fixture that owns it and the installer reads it from there, so there is a single place to change: `tests/fixtures/rust/rust-toolchain.toml` (Rust channel), `tests/fixtures/python/.python-version` (CPython), `tests/fixtures/python/.uv-version` (uv) and `tests/fixtures/python/pyproject.toml` (pytest). Cargo and uv lockfiles are committed, and `scripts/fixture-checksums.txt` holds the reviewed uv archive hashes — refresh it with `.uv-version`. The Python fixture asserts the running interpreter against `.python-version` itself; the `identity` strings in `tests/fixtures/*/levenshtein.json` repeat the versions because JSON has nowhere to reference them from, so update those with the pin.

## What is exercised

| Contract | Rust workspace | Python package |
| --- | --- | --- |
| Workspace context | Check runs from `crates/app`; root manifests and sibling path dependency are inputs | Shared pytest fixtures, package source, scripts, project metadata and lockfile are inputs |
| Checks | Cargo tests with a separate `--no-run` build stage | Small AST lint policy and pytest share one locked uv environment |
| Warm reuse | Completed result reused across CLI processes | Both check results reused across CLI processes |
| Fresh execution | Test execution marker advances; build stage remains reusable | Test execution marker advances; dependency environment remains reusable |
| Invalidation | Source, root lockfile, and feature selection | Tests, lockfile, shared `conftest.py`, and selected tests |
| Regression detection | Changing the sibling dependency makes a real assertion fail | Changing the shared expected-value fixture makes a real assertion fail |

The harness also proves that an unrelated README edit retains hits, original verification timestamps survive hits, and a Python test edit retains dependency preparation. It prints end-to-end CLI timings for cold, warm, unrelated-edit, small-edit, variant, lockfile-edit, and fresh runs. These tiny fixtures validate behavior; consumer latency budgets require measurements on real repositories.

GitHub Actions runs `language-contracts` alongside the existing Go verification job. Missing tooling or broken preparation fails the job. A tool error cannot substitute for the intentionally failing test assertions.
