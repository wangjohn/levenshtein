# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately using [GitHub private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability) on this repository (Security tab → "Report a vulnerability"). Do not open a public issue for a suspected vulnerability.

## Scope

This policy covers:

- The Levenshtein CLI (`cmd/`, `internal/`)
- The Dagger runner module (`runner/`)
- The lint analyzers (`runner/lint/`)

## Not a sandbox

Native `command` checks execute trusted repository code by design; they run as ordinary host processes with normal filesystem access and are not a sandbox. See [Configuration](docs/configuration.md#native-commands) for details.
