# Documentation

Every page, grouped by what you are trying to do. New to Levenshtein? Read the [project README](../README.md), then [setup](setup.md).

## Evaluate

- [Project README](../README.md): what Levenshtein checks, an example, and a quick start
- [FAQ](faq.md): Levenshtein or golangci-lint, Docker, and other languages
- [Checks](checks.md): every lint rule and the evidence for turning it on or leaving it off
- [Versioning](versioning.md): what a release may change and what to expect when you bump your pin
- [Changelog](../CHANGELOG.md): what each release changed

## Adopt

- [Setup](setup.md): prerequisites, and running `./verify` locally
- [Using it in CI](consumer-ci.md): the GitHub Action and other CI providers
- [Coding agents](agents.md): templates for Claude Code hooks, `AGENTS.md`, a workflow, and a starter config
- [Releases](releases.md): prebuilt archives, and pinning a release
- [Troubleshooting](troubleshooting.md): common errors and what to do about them

## Configure

- [Configuration](configuration.md): targets, environments, checks, runs, the baseline, and caching
- [Community lint rules](community-rules.md): run rules published as Go modules, or publish your own
- [Mutation testing](mutation.md): the `go-mutation` check and its accepted-survivors file
- [Semantic lint](semantic-lint.md) (experimental): an advisory review by a language model, through an external paid service

## Reference

- [CLI reference](reference/cli.md): flags, the run argument, exit codes, and output formats
- [Check kinds](check-kinds.md): every check kind, its executors, caching, baseline support, and default runs
- [Glossary](glossary.md): target, workspace, executor, run, baseline, and the other terms the docs use

## Contribute

- [Contributing](../CONTRIBUTING.md): building, testing, proposing a rule, and pull requests
- [Security policy](../SECURITY.md): reporting a vulnerability privately
- [Code of conduct](../CODE_OF_CONDUCT.md)

## Internals

- [Architecture](architecture.md): how `verify` plans, executes, caches, and reports
- [Dependencies](dependencies.md): the libraries Levenshtein builds on, and the Dagger SDK patch
- [Language fixtures](language-fixtures.md): the Rust and Python fixtures that test the configuration interface
- [Roadmap](roadmap.md): the internal pilot plan
