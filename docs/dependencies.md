# Reuse existing tooling

Levenshtein owns shared verification policy: named runs, target inputs, environment requirements, result identity, and freshness. Use established tools for general infrastructure.

| Concern | Implementation |
| --- | --- |
| Command-line flags and help | `spf13/pflag` |
| Container execution, module loading, engine lifecycle, and caching | Official Dagger Go SDK |
| Go lint and custom policy | Staticcheck runner with `go/analysis` analyzers |
| Paths confined to a repository | Go `filepath.IsLocal` and `os.Root` |
| Binary builds, release archives, and checksums | GoReleaser |

Native commands still use Go's `os/exec`; process-group cancellation is needed to stop test subprocesses on timeout. Repository-specific policy and result reporting stay in Levenshtein. The current executor dispatches selected checks sequentially; Dagger reuses its dependency graph and caches within that execution. Scheduling more independent checks concurrently is separate work.

Dagger checks use a stable shared module, with source and freshness passed as function arguments. Do not embed changing consumer inputs into the module's source: Dagger uses module identity to namespace dependency and compiler caches.
