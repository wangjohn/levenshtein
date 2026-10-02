# Pilot worksheet

Use this worksheet with a consenting team trying Levenshtein on a real repository. No external pilot results are recorded here yet. Recruiting teams, contacting contributors, and publishing quotes need separate consent. Keep repository code, findings, identities, and credentials private unless the team agrees to share them.

Copy the worksheet for each pilot. Agree on the evaluation period and the work the team would otherwise do before measuring. Record observed results, including failed setup and abandoned trials. Leave unknown values unknown rather than estimating them as successes.

| Field | Observation |
| --- | --- |
| Team/repository alias and consent scope | |
| Trial dates, maintainer, existing verification workflow | |
| Levenshtein release/full revision and shared rule pin | |
| OS/architecture, Go version, native/Dagger executor, CI environment | |
| Repository size: Go packages/files/lines and other languages | |
| Setup start to first completed check, including installation and CI setup | |
| First useful finding: check/rule, why useful, time to finding | |
| Checks attempted, passed, failed, unavailable, or skipped | |
| Findings accepted as useful / noisy / undecided, with denominator and rubric | |
| Suppressions: rule, reason, scope, time to write/review | |
| Baseline: entries at start/end, additions/removals, review time | |
| Verification wall times: cold installation/build, warm, edit, fresh audit; repeated samples and cache state | |
| Upgrade: old/new pins, new findings, changes needed, engineer time | |
| Retained use: check at end of trial and agreed follow-up date; runs/active contributors | |
| Abandonment or rollback: when, why, competing workflow | |
| Consent to publish results or a quote, exact scope and reviewer | |

For each finding, distinguish a defect caught, useful cleanup, house-policy preference, and false positive. Count distinct findings consistently, and do not equate baseline entries or suppressions with defects fixed. Capture both the first impression and the team's decision after using the tool. Record the denominator for useful/noisy rates and all missing observations.

Use [the performance harness](performance.md) to understand its synthetic cache behavior, then measure the actual consumer separately with an explicit cache policy. Include installation, engine/image startup, rule-module compilation, and upgrade effort where they occur. Compare equivalent checks under equivalent conditions; changing the selected rules changes the work being measured.

Before publishing, have the participating team verify the observations and approve exactly what may be shared. Link the release/revision and methodology, anonymize identifying material when requested, and preserve contrary observations. A completed local fixture run supports a synthetic measurement claim; it does not support an adoption claim or a testimonial.
