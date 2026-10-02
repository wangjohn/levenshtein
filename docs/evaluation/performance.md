# Measuring verification cost

Use [`scripts/measure-performance`](https://github.com/wangjohn/levenshtein/blob/main/scripts/measure-performance) to measure a fixed **synthetic** one-package Go consumer through the native executor. These measurements describe this fixture on your machine; they do not establish performance on real repositories or adoption by external teams. The harness checks verdicts, finding codes, cache states, verification times, and implementation identities before writing a result. It has no latency threshold.

From a source checkout with Git, Python 3.9 or later, and the [pinned Go toolchain](../setup.md), run:

```sh
scripts/measure-performance --repeats 3 --output /tmp/levenshtein-performance.json
```

Each repetition starts with an empty Go module-download cache, compilation cache, Staticcheck cache, and verification cache. The harness refuses a nonempty effective `GOCACHEPROG` setting, including one configured in the Go environment file, because an external compilation cache could bypass the empty local cache. The fresh install stage builds the CLI from the current source snapshot, including dependency downloads. This is a source build, not installation of a release archive. The cold tool stage includes building the actual shared linter and loading the consumer. Source copying and fixture setup are preparation outside the measured commands; their cost is not part of a consumer's CLI invocation. The CLI and helper build costs are separate recorded phases, so first-launch overhead stays visible.

For offline or faster repeated measurements, explicitly reuse a populated Go module-download cache:

```sh
scripts/measure-performance --repeats 3 --module-cache "$(go env GOMODCACHE)" \
  --output /tmp/levenshtein-performance-downloads-reused.json
```

This variant reports reused downloads and must not be described as a completely cold install. Go compilation, linter analysis, and verification caches still start empty in each repetition. Other environment settings, network, toolchain download caches, CPU contention, and filesystem caches can affect results. The CLI build inherits your Go settings; the native fixture explicitly forwards the Go compilation/module cache paths, GOENV, result-changing Go settings, and module download settings to helper builds and analysis; the verification report hashes result-changing settings rather than printing their values. Record unusual conditions separately without copying credentials. A Go toolchain downloaded outside the measured build is not counted as installation work.

The harness copies tracked working-tree files once into a temporary snapshot and uses that fixed snapshot for every repetition, so committed and edited source are measured as they exist on disk. It records the original Git revision, whether the original checkout was dirty, and a content digest of the copied tracked tree. Untracked shared-source edits are excluded. Run from a clean checkout for publishable revision comparisons. Temporary copies and caches are removed when each repetition finishes. Staticcheck uses the executor-owned analysis directory within the new verification cache; explicit fresh runs use a separate empty analysis directory.

| Phase | Expected evidence |
| --- | --- |
| `fresh_install` | Wall time for CLI compilation and, in default mode, module downloads; empty compilation cache |
| `cold_tool_build` | Passing fixture, verification miss, native toolchain and implementation identity |
| `warm_unchanged` | Passing result hit with the original verification timestamp and implementation |
| `source_edit` | Changed cache key, miss, failing result with `LV1003` after combining two struct fields |
| `shared_rule_revision` | Changed implementation digest and cache key, miss, passing result after disabling `LV1003` in the temporary shared policy |
| `revised_warm` | Passing result hit under the revised shared policy |
| `explicitly_fresh` | Passing `audit` run, `fresh` cache status, new verification timestamp despite unchanged inputs |
| `rule_restored_negative` | Restoring the original shared policy rediscovers `LV1003`; a previous failure requires fresh execution |

The rule-revision phase is a controlled local policy change, not a claim about released versions. The [consumer fixture](https://github.com/wangjohn/levenshtein/tree/main/tests/fixtures/performance) has one Go file and no third-party dependencies. Its only deliberate finding is the struct-field rule. A failure in any correctness expectation aborts the run without producing a successful measurement report. Speed alone never proves cache correctness.

Choose a new output filename for every invocation; the harness refuses to overwrite an existing measurement.

The snapshot is built outside Git, so the CLI build identity truthfully reports an unavailable commit/state; the original revision and measured tracked-tree digest identify the measured source separately.

The JSON includes repeat samples, native executor, OS/architecture, Python version, CLI build and effective Go identities, source revision/tree digest, case size, cache states and keys, diagnostic codes, and command templates with temporary paths replaced by labels. For each phase it provides count, minimum, median, maximum, mean, and sample standard deviation in wall seconds. At least two repetitions are required. No source text, raw diagnostic messages, Go setting values, environment dump, or arbitrary tool output is published. Cache and implementation digests identify inputs; treat them as identifiers rather than secret sanitizers.

Compare matching phases with the same fixture, executor, cache state, machine, and download-cache mode. Keep full samples, not just the fastest run. For an input or rule revision comparison, rerun the complete harness: its correctness phases deliberately change both inputs and shared policy on every repetition. For changes to Levenshtein itself, run clean checkouts of both revisions with identical options and retain both JSON artifacts.

This harness currently measures native Go lint only. Dagger engine startup, container image pulls, multi-package consumers, network checks, and other languages require separately described cases and are not represented by these numbers. Run the repository's existing [Dagger integration checks](../maintainers/development.md) when an engine is available; report an unavailable engine explicitly rather than substituting native timings. Real adoption evidence belongs in the [pilot worksheet](pilot.md).
