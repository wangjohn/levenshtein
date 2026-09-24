# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately using [GitHub private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability) on this repository (Security tab → "Report a vulnerability"). Do not open a public issue for a suspected vulnerability.

## Scope

This policy covers:

- The Levenshtein CLI (`cmd/`, `internal/`)
- The Dagger runner module (`runner/`)
- The lint analyzers (`runner/lint/`)
- The community linter's runtime and builder (`runner/community/`)

## Not a sandbox

Native `command` checks execute trusted repository code by design; they run as ordinary host processes with normal filesystem access and are not a sandbox. See [Configuration](docs/configuration.md#native-commands) for details.

## Community lint rules

[Community rule modules](docs/community-rules.md) are third-party code. Levenshtein pins them exactly, builds them against the checksum database, and runs them in an isolated container step with no credentials and nothing shared writable, but that step can still reach the network. Enable a community rule on a private repository only if you would run its author's code against that repository yourself.

- A vulnerability in a community rule belongs to that module's maintainers; report it to them.
- A way for a rule module to escape the community lint step, read credentials, or change core findings or another consumer's results is a Levenshtein vulnerability; report it here.
- A malicious rule module should be reported here too, so the next release can list the version as withdrawn in `runner/rule-modules.json`, which makes it a configuration error for every consumer who updates.
- Community rules never run on the native executor. When native execution arrives, it will be trusted-only, like native `command` checks.
