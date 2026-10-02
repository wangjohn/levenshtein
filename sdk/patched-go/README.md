# Temporary patched Go SDK

Dagger 0.21.9 forces OpenTelemetry logging dependencies back to v0.16.0,
including the HTTP log exporter affected by GO-2026-4985. The earlier
v0.20.0 patch still leaves the gRPC log exporter affected by
[GO-2026-6508](https://pkg.go.dev/vuln/GO-2026-6508); the adapter now pins all
four logging modules to v0.21.0. Editing the runner's
`go.mod` alone cannot fix this: module loading regenerates the overrides.

This adapter builds Dagger's own Go generator from the pinned 0.21.9 commit,
with updated dependency pins in its embedded SDK manifest. It uses the upstream
`generate-module` and `generate-typedefs` commands and compiles the runner with
the project's pinned Go image. Generation and runtime compilation therefore
use the same patched dependencies. Download and compiler caches are retained.

Logging v0.21.0 moves log values and attributes to the stable `attribute` API,
which Dagger's telemetry fork does not yet support. The shipped
[`otel-go`](otel-go/PROVENANCE.md) module applies that mechanical migration to
v1.43.0, retaining its upstream Apache license and protobuf value behavior.
The generator uses the same local replacement as the runner. Its two slog
bridge parameters use the new attribute type, and `patch-generator.sh` makes
its embedded manifest preserve local directory replacements. A generator test
executes the replacement commands twice; the security gate also requires the
shipped replacement after two complete Dagger generations.

Trace exporters are pinned to v1.45.0 to fix
[GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505). The engine remains 0.21.9.

The adapter uses Python to avoid bootstrapping another Go module with the same
vulnerable dependency override. Consumers still use the normal Levenshtein
commands; they do not need Python installed on their host.

## Locked dependencies

Dagger's Python SDK runtime installs this adapter on every Dagger run, for
Levenshtein and for every consumer. `pyproject.toml` pins `dagger-io` to the
engine in `.dagger-version` and the `uv_build` backend exactly, and `uv.lock`
pins everything else with hashes: with the lock present the runtime runs
`uv lock` and `uv sync`, which install the locked versions instead of resolving
PyPI at run time. `uv lock` re-resolves a lock that no longer matches
`pyproject.toml`, so after changing either file run
`uv lock --directory sdk/patched-go`; `scripts/test-sdk-lock` fails until the
lock is current. Release archives ship the lock.

## Validation and removal

Run `./scripts/test-sdk-security` from the repository root. It regenerates twice,
asserts the effective logging module versions and local telemetry replacement,
runs telemetry compatibility tests, and scans a generated executable
with the live vulnerability database. `./verify go-vuln` also scans the configured
source modules through the normal Dagger execution path.

This is a workaround for the module SDK, not a patched Dagger engine. Keep the
engine and CLI updated separately. When a compatible upstream Go SDK retains
fixed logging versions, switch `dagger.json` back to `go`, run these checks, and
remove this adapter. Do not just remove the runner's replacements.
