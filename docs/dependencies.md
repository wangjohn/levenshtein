# Reuse existing tooling

Levenshtein owns shared verification policy: named runs, target inputs, environment requirements, result identity, and freshness. Use established tools for general infrastructure.

| Concern | Implementation |
| --- | --- |
| Command-line flags and help | `spf13/pflag` |
| Container execution, module loading, engine lifecycle, and caching | Official Dagger Go SDK |
| Go cleanup lint | Staticcheck |
| Paths confined to a repository | Go `filepath.IsLocal` and `os.Root` |
| Cross-process locking and cancellation | `gofrs/flock` |
| Atomic cache and artifact publication | `google/renameio/v2` with `os.Root` |
| Binary builds, release archives, and checksums | GoReleaser |

Native commands still use Go's `os/exec`; process-group cancellation is needed to stop test subprocesses on timeout. Repository-specific policy and result reporting stay in Levenshtein. The current executor dispatches selected checks sequentially; Dagger reuses its dependency graph and caches within that execution. Scheduling more independent checks concurrently is separate work.

Dagger checks use a stable shared module, with source and freshness passed as function arguments. Do not embed changing consumer inputs into the module's source: Dagger uses module identity to namespace dependency and compiler caches.

The result cache retains Levenshtein's exact input fingerprints, original verification times, failure invalidation, and artifact validation. Dagger supplies execution caches; the outer result cache avoids starting the engine for an unchanged successful check. Native setup and build reuse remain separate from test verdicts.

Artifact publication uses `renameio` pending files so restored permissions match the recorded artifact, even when replacing an existing file. Rooted filesystem operations keep reads and writes within the opened checkout. Mutable output paths additionally reject symlinks; source symlinks disable result reuse.
