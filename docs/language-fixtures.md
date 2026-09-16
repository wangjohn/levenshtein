# Rust and Python compatibility fixtures

These small consumers exercise the common configuration and native executor. They are interface tests, not complete language integrations. Their application code and assertions stay beside the fixture; no Rust/Python branches were added to the planner.

## Run

Use the pinned Go version, a C linker/toolchain for Rust, `rustup`, and Python 3 to run the harness. The fixture installer downloads checksum-verified uv and installs the pinned language runtimes:

```sh
./scripts/install-fixture-tools "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
./scripts/test-language-contracts
```

The harness builds the standalone CLI once, copies fixtures into temporary repositories, and starts a new CLI process for each verification. It cleans up the temporary repositories and result caches. Package/tool downloads remain in the native tools' caches. The normal Go verification path does not require these extra tools.

Pins verified September 16, 2026: [Rust 1.98.1](https://rust-lang.org/), [uv 0.12.15](https://github.com/astral-sh/uv/releases/tag/0.12.15), [Python 3.14.7](https://www.python.org/downloads/release/python-3147/), and [pytest 9.1.1](https://pypi.org/project/pytest/9.1.1/). Cargo and uv lockfiles are committed. Rust uses its checked-in toolchain file; Python uses `.python-version` and validates the runtime in an actual assertion.

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
