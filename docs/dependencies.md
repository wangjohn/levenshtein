# Reuse existing tooling

Levenshtein owns shared verification policy: named runs, target inputs, environment requirements, result identity, and freshness. Use established tools for general infrastructure.

| Concern | Implementation |
| --- | --- |
| Command-line flags and help | `spf13/pflag` |
| Container execution, module loading, engine lifecycle, and caching | Official Dagger Go SDK |
| Go lint and custom policy | Staticcheck runner with `go/analysis` analyzers |
| Paths confined to a repository | Go `filepath.IsLocal` and `os.Root` |
| Cross-process locking and cancellation | `gofrs/flock` |
| Atomic cache and artifact publication | `google/renameio/v2` with `os.Root` |
| Binary builds, release archives, and checksums | GoReleaser |

Native commands still use Go's `os/exec`; process-group cancellation is needed to stop test subprocesses on timeout. Repository-specific policy and result reporting stay in Levenshtein. Independent checks in a run execute concurrently with a small worker cap (`maxCheckParallelism` in `internal/verify`); Dagger reuses one session and its dependency graph within that execution. Native preparation still serializes mutable stage ownership inside the native executor.

Dagger checks use a stable shared module, with source and freshness passed as function arguments. Do not embed changing consumer inputs into the module's source: Dagger uses module identity to namespace dependency and compiler caches.

The result cache retains Levenshtein's exact input fingerprints, original verification times, failure invalidation, and artifact validation. Dagger supplies execution caches; the outer result cache avoids starting the engine for an unchanged successful check. Native setup and build reuse remain separate from test verdicts.

Artifact publication uses `renameio` pending files so restored permissions match the recorded artifact, even when replacing an existing file. Rooted filesystem operations keep reads and writes within the opened checkout. Mutable output paths additionally reject symlinks; source symlinks disable result reuse.

## Dagger wrapper dependency security

The wrapper pins gRPC 1.83.2, OpenTelemetry 1.44.0, and x/text 0.41.0 to address reported advisories. Dagger 0.21.9 normally forces logging modules back to 0.16.0 during development and module loading, reintroducing [GO-2026-4985](https://pkg.go.dev/vuln/GO-2026-4985) even when `go.mod` requests a fixed version.

The temporary [patched Go SDK](../sdk/patched-go/README.md) builds Dagger's pinned upstream generator with logging replacements at 0.20.0. Both generated source and the runtime use those dependencies. This changes the module SDK, not the Dagger engine itself. Remove the adapter once a compatible upstream SDK preserves fixed versions.

`./scripts/test-sdk-security` regenerates twice, checks effective dependency versions, and scans the generated executable. The live `./verify go-vuln` source scans remain enabled without suppressions.
