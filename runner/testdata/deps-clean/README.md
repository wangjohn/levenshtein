# deps-clean

A lockfile whose one npm package has no published advisory, and a Go module
with one, which `deps-vuln` must leave to `go-vuln`. The files end in
`.fixture`, like `deps-vulnerable`'s, and the tests copy them without the
suffix before scanning.
