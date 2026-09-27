# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately using [GitHub private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability) on this repository (Security tab → "Report a vulnerability"). Do not open a public issue for a suspected vulnerability.

You should get a response within 14 days.

## Supported versions

Levenshtein has not reached 1.0. Security fixes land on `main` and ship in the next release, and consumers get them by moving their pin to it.

Only the newest release is supported: fixes are not backported to older releases. See [supported releases](docs/versioning.md#supported-releases).

## Scope

This policy covers:

- The Levenshtein CLI (`cmd/`, `internal/`)
- The Dagger runner module (`runner/`)
- The lint analyzers (`runner/lint/`)
- The community linter's runtime and builder (`runner/community/`)
- The GitHub Action (`action.yml`), which runs in consumers' workflows with their checkouts and tokens
- The templates in `templates/`, which consumers copy into their repositories, including the workflow and the coding agent hooks

## Not a sandbox

Native `command` checks execute trusted repository code by design; they run as ordinary host processes with normal filesystem access and are not a sandbox. See [Configuration](docs/configuration.md#native-commands) for details.

In Dagger, the checks that run repository code (`go-test`, `go-generate`, and `go-mutation`) use Go module and build cache volumes separate from the ones the linters and pinned tools are built from; a way for repository code to change a tool build, or the verdict of a check that does not run repository code, through a shared cache is a Levenshtein vulnerability.

Those untrusted volumes are kept per clone: the CLI names them by a hash of the checkout's git common directory, so every worktree of one clone shares them and separate clones never do, and the repository cannot choose the name. Code one of those checks runs can still affect a later `go-test`, `go-generate`, or `go-mutation` result for the same clone, on any of its worktrees. Across clones:

- **Module sources** are verified before each of those steps runs anything: `go mod verify` compares the cached copy of every downloaded dependency with the hash recorded when it was downloaded, and Go compares that hash with the repository's `go.sum` when it loads the module, so a changed module source is a check error instead of being compiled. This covers the modules `go.sum` lists; a program a generator fetches with `go run pkg@version` is checked only by Go's own download checks.
- **The build cache** has no such check. It is protected only by the per-clone name, so a repository reaching another clone's volumes, for example through a direct Dagger call without a key, which shares `unkeyed` volumes with every other such call, could change what that clone's tests compile.

A way for one clone's code to reach another clone's untrusted volumes through the CLI, or to get a changed module source compiled, is a Levenshtein vulnerability. The per-clone name is not a sandbox: a shared self-hosted runner that checks repositories you do not trust should still give each job an ephemeral Dagger engine.

## Community lint rules

[Community rule modules](docs/community-rules.md) are third-party code. Levenshtein pins them exactly, builds them against the checksum database, and runs them in an isolated container step with no credentials and nothing shared writable, but that step can still reach the network. Enable a community rule on a private repository only if you would run its author's code against that repository yourself.

- A vulnerability in a community rule belongs to that module's maintainers; report it to them.
- A way for a rule module to escape the community lint step, read credentials, or change core findings or another consumer's results is a Levenshtein vulnerability; report it here.
- A malicious rule module should be reported here too, so the next release can list the version as withdrawn in `runner/rule-modules.json`, which makes it a configuration error for every consumer who updates.
- Community rules never run on the native executor. When native execution arrives, it will be trusted-only, like native `command` checks.
