# Temporary patched Go SDK

Dagger 0.21.9 forces OpenTelemetry logging dependencies back to v0.16.0,
including the HTTP log exporter affected by GO-2026-4985. Logging 0.20.0
also has GO-2026-6508 in its gRPC exporter; the fixed release is 0.21.0. Editing the runner's
`go.mod` alone cannot fix this: module loading regenerates the overrides.

This adapter builds Dagger's own Go generator from the pinned 0.21.9 commit,
with updated dependency pins in its embedded SDK manifest. It uses the upstream
`generate-module` and `generate-typedefs` commands and compiles the runner with
the project's pinned Go image. Generation and runtime compilation therefore
use the same patched dependencies: logging 0.21.0 and core/exporters 1.45.0.
The trace exporters also fix GO-2026-6505. Download and compiler caches are retained.

The generator itself is built with `golang.org/x/tools` v0.50.0 rather than
Dagger's v0.45.0. It type-checks the runner from `go list` export data, and Go
1.27.2 writes export data version 5, which earlier `x/tools` cannot decode;
the generator would then type every function parameter as `any`.

Logging 0.21.0 removes `log.Value`, `log.KeyValue`, and their constructors in
favor of `attribute.Value` and `attribute.KeyValue`. The pinned Dagger
`otel-go` v1.43.0 still uses them, so `otel-go.patch` adapts its writers and OTLP
conversions. The adapter generates the patched library in `internal/telemetry`
as a local Go module and adds a relative replacement; the generator compiles
against the same patch. Generated telemetry remains ignored by Git and is
regenerated with the SDK. The generator's logging bridge is adapted too.

The HTTP trace and metric exporters in 1.45.0 use `/` for endpoint URLs without
a path. The compatibility patch explicitly supplies `/v1/traces` or
`/v1/metrics` in that case, preserving Dagger's previous routing. Explicit
paths, including `/`, keep their meaning. Tests cover value conversions,
writer output, and endpoint paths.

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
asserts the effective logging module versions, tests the patched telemetry,
and scans a generated executable
with the live vulnerability database. `./verify go-vuln` also scans the configured
source modules through the normal Dagger execution path.

This is a workaround for the module SDK, not a patched Dagger engine. Keep the
engine and CLI updated separately. When a compatible upstream Go SDK retains
fixed logging versions, switch `dagger.json` back to `go`, run these checks, and
remove this adapter. Do not just remove the runner's replacements.
