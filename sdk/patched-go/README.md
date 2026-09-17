# Temporary patched Go SDK

Dagger 0.21.9 forces OpenTelemetry logging dependencies back to v0.16.0,
including the HTTP log exporter affected by GO-2026-4985. Editing the runner's
`go.mod` alone cannot fix this: module loading regenerates the overrides.

This adapter builds Dagger's own Go generator from the pinned 0.21.9 commit,
with updated dependency pins in its embedded SDK manifest. It uses the upstream
`generate-module` and `generate-typedefs` commands and compiles the runner with
the project's pinned Go image. Generation and runtime compilation therefore
use the same patched dependencies. Download and compiler caches are retained.

The adapter uses Python to avoid bootstrapping another Go module with the same
vulnerable dependency override. Consumers still use the normal Levenshtein
commands; they do not need Python installed on their host.

## Validation and removal

Run `./scripts/test-sdk-security` from the repository root. It regenerates twice,
asserts the effective logging module versions, and scans a generated executable
with the live vulnerability database. `./verify go-vuln` also scans the configured
source modules through the normal Dagger execution path.

This is a workaround for the module SDK, not a patched Dagger engine. Keep the
engine and CLI updated separately. When a compatible upstream Go SDK retains
fixed logging versions, switch `dagger.json` back to `go`, run these checks, and
remove this adapter. Do not just remove the runner's replacements.
