# deps-vulnerable

A lockfile that pins two npm packages with published advisories, and a Go
module with one, which `deps-vuln` must leave to `go-vuln`. The files end in
`.fixture` so that GitHub's dependency graph, Dependabot, and dependency review
never mistake them for Levenshtein's own dependencies; the tests copy them
without the suffix before scanning.
