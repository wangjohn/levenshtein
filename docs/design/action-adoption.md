# GitHub Action adoption plan

Status: implementation PR #112 is merged and v0.3.0 is published; both consumer
smoke cases passed their expected outcomes. Marketplace publication requires
a new reviewed release containing PR #114's shortened action description;
listing validation and verified listing links remain pending.
Created October 2, 2026.

## Goal

Make it immediately clear that Levenshtein can verify a Go repository in
GitHub Actions, give readers a small workflow they can copy, and publish the
existing action to GitHub Marketplace.

The action already exists at the repository root. It sets up Go, chooses a
run from the workflow event, caches results, emits annotations, and writes a
job summary. This project needs adoption documentation and publication work;
it does not need a new runner implementation.

Success means a reader can find the GitHub Actions setup near the top of the
README, add one workflow file to a single-module Go repository, and see useful
results on a pull request. The Marketplace listing must offer valid installation
syntax and point readers to that same setup.

## 1. Check the current public state

- [x] Confirm the default branch and latest published release, and that the
  chosen release contains the root `action.yml` and all its runtime files.
- [x] Check whether a Marketplace listing already exists. The public Marketplace
  search for `levenshtein` returned only an unrelated action on October 2, 2026.
  The expected listing URL returned 404; name eligibility still needs the release form.
- [ ] Read GitHub's current [Marketplace publishing requirements](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace).
  Confirm repository eligibility, including its contents, root metadata file,
  and uniqueness of the action name `Levenshtein verify`. Use GitHub's release
  form to check name availability rather than assuming it from a search.
- [ ] Keep the current repository and consumer action path if eligible. If
  GitHub reports an eligibility problem, record the exact issue before choosing
  a repository split or metadata change.

Use the latest published release's full commit SHA with its version as a
comment for consumer examples. The initial implementation used v0.2.0; the
post-publication consumer examples now use v0.3.0. Do not substitute a branch or
an unpublished commit.

## 2. Add a minimal starter workflow

Create `templates/github/workflows/levenshtein-minimal.yml`. Keep the existing
`templates/github/workflows/levenshtein.yml` as the advanced example so existing
links and its code scanning setup continue to work.

- [x] Name the workflow `Levenshtein` and the job `verify`.
- [x] Trigger it on `pull_request` and `workflow_dispatch`.
- [x] Use `ubuntu-24.04`, a 20-minute timeout, and `contents: read` permissions.
- [x] Include just checkout and the Levenshtein action. Copy the checkout pin
  from existing workflows, set `persist-credentials: false`, and pin
  Levenshtein to a published release SHA with the version comment.
- [x] Let the action choose its run and use its default caching and annotations.
  Do not require a `levenshtein.json`, extra secrets, SARIF upload, or a separate
  Go setup step for the single-module starter.
- [x] Explain in a short comment that consumers save the file as
  `.github/workflows/levenshtein.yml`; link to the consumer CI guide.

The initial template is a pull request check. Document optional additions
separately: `merge_group` before making it required in a merge queue, pushes
for branch checks, and a daily schedule for the `main` vulnerability audit.
The starter alone does not provide a daily dependency audit. Default checks
use Dagger; verify the chosen GitHub-hosted Ubuntu runner supplies the required
Docker runtime during the consumer smoke test.

## 3. Make the README's adoption path obvious

Update `README.md` after the short product description and before the longer
background and check catalog.

- [x] Add a section titled `Add to GitHub Actions` with the instruction:
  “Save this as `.github/workflows/levenshtein.yml` in your Go repository.”
- [x] Include the complete minimal workflow, matching the new template.
- [x] State that a single Go module at the repository root needs no configuration,
  and that the action sets up Go and reports failures on pull requests.
- [x] Link to the minimal template and `docs/consumer-ci.md` for customization.
- [x] Keep local CLI setup easy to find with a nearby link to Quickstart.
- [x] Remove or shorten the later duplicate action introduction so readers get
  one clear setup path rather than competing instructions.
- [x] Link to the baseline instructions for repositories with existing findings.
  Keep this as an optional next step, not part of the basic installation.

Do not add a Marketplace badge with a guessed URL. Add it after the listing
has been published and its actual URL has been verified.

## 4. Align the consumer guide and release checklist

Update `docs/consumer-ci.md` and `docs/maintainers/releases.md` in the same change.

- [x] Start the GitHub Actions section with the minimal setup and a link to its
  template. Keep the existing full workflow under an advanced setup subsection.
- [x] Explain when to add merge queue support, daily audits, SARIF uploads, and
  required status checks. Keep the simple and advanced examples clearly labeled.
- [x] Check existing links to `#github-actions` and the current template still work.
- [x] Add Marketplace publication to the maintainer release checklist, preserving
  the existing draft release, archive validation, and consumer pin update process.
- [x] Document the owner steps: accept the Marketplace Developer Agreement if
  needed, enable the publication checkbox, resolve metadata validation errors,
  choose the closest available code quality/security categories, and publish.
- [x] Include checking Marketplace publication on subsequent releases so its
  displayed version does not silently lag behind GitHub releases.

## 5. Validate the implementation

- [x] Run `./scripts/test-doc-pins`; use `--latest` when updating examples to the
  newest published release. New files must also be covered by validation:
  the current script uses `git grep`, so untracked files are omitted until
  staged or otherwise included in the check.
- [x] Run `./scripts/test-workflows` to check shared action pins and workflow policy.
- [x] Run the repository's existing actionlint/workflow-security checks over the
  new template as well as the existing examples. Inspect their file coverage;
  do not assume adding a template makes it automatically included.
- [x] Compare the README workflow with the minimal template and inspect Markdown
  rendering and local links. Do not add a test suite just to compare prose.
- [x] Exercise the copied starter in a disposable single-module consumer repo
  on GitHub-hosted Ubuntu, without custom Levenshtein configuration. Test a
  passing pull request and a deliberate lint violation: the latter must fail
  the job and produce an annotation and job summary.
- [x] Record the release SHA, workflow run links, and outcomes in the PR.

Existing action behavior remains unchanged. Go code edits are unnecessary;
if implementation expands to Go changes, run the shared Go lint rules too.

## 6. Publish to Marketplace

After the documentation and template changes are merged:

- [ ] Open the root `action.yml` on GitHub and follow the release publication flow.
- [ ] Choose a reviewed release containing the action. If the documentation
  changes need a new release, use the existing release process; do not move an
  existing tag. If an existing eligible release can be listed through GitHub's
  UI, check its metadata and README before using it.
- [ ] Have the repository owner accept the Developer Agreement if necessary.
- [ ] Select `Publish this Action to the GitHub Marketplace`, resolve validation
  errors, select the categories, and publish the release after its usual checks.
- [ ] Open the resulting listing and verify its name, description, README,
  displayed version, repository link, and copied installation syntax.

GitHub Actions listings provide workflow syntax to copy. A repository-selection
installation flow would require a GitHub App and is outside this plan. See
[GitHub's Action installation experience](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/find-and-customize-actions).

## 7. Add the published listing link

In a small follow-up documentation change:

- [ ] Add a `GitHub Marketplace` badge beside the README's current badges,
  linking to the verified listing URL.
- [ ] Add a `Use this Action` link beside the new README setup section and in
  the consumer guide.
- [ ] Update consumer release pins through the normal release process if needed.
- [ ] Re-run documentation pin and workflow policy checks, then confirm the
  README links open the live listing and its correct installation instructions.

## Delivery order and completion criteria

1. One implementation PR: minimal template, README setup, consumer guide,
   release checklist, and relevant validation coverage adjustments.
2. Owner publication step: Marketplace validation and release publication.
3. One follow-up PR: verified Marketplace links and any required release pin updates.

The work is complete when the minimal setup passes the consumer smoke test,
the Marketplace listing is public with a usable release, and readers can reach
both setup and listing directly from the README. Any eligibility or publication
blocker should be recorded with GitHub's exact error and the remaining owner action.

## Implementation evidence

- Initial October 2 observation: public repository `main`; then-latest published
  release v0.2.0, commit
  `3d47ab4c589fdf3a30107b6dd3f0816c1f346c85`. The annotated tag was checked
  through GitHub's API and contains the root action. The prepared v0.3.0
  changelog section on main was then unpublished and was not used by consumers.
- Local validation: `scripts/test-doc-pins --latest`, `scripts/test-workflows`,
  `scripts/test-tool-checks`, actionlint over both templates and the changed
  security workflow, offline zizmor 1.30.1 audits, and starter snippet/link
  checks passed. New templates are tracked and included in the pin checks.
- Security coverage: explicitly name both template files in `security.yml`.
  Zizmor's directory discovery skips workflow templates outside `.github/workflows`;
  passing `templates/github/workflows` alone collected no inputs locally.
- Consumer validation uses a disposable private repository with the copied
  starter workflow, one dependency-free Go module, and no custom configuration.
  [Smoke test pull request](https://github.com/wangjohn/levenshtein-action-adoption-smoke-20261002/pull/1).
  The [passing run](https://github.com/wangjohn/levenshtein-action-adoption-smoke-20261002/actions/runs/37073516681)
  passed `go-lint`, `go-vet`, and `go-mod`, emitted no findings, and completed
  the annotation and summary steps. The first uncached run took about five minutes.
  The [deliberately failing run](https://github.com/wangjohn/levenshtein-action-adoption-smoke-20261002/actions/runs/37073990715)
  failed with `SA5001` in `broken.go:8`; GitHub recorded the finding as an
  annotation, and the annotation and job summary steps both completed successfully.
  The private test repository was archived after validation to preserve evidence.
- Initial publication blocker: the available browser redirected the release
  edit form to GitHub sign-in. The owner subsequently signed in and accepted
  Developer Agreement v2.4.
- The authenticated release form rejected v0.2.0: “Description must be less
  than 125 characters.” PR #114 shortens the description to 114 characters.
  A new reviewed release must contain the correction; published tags stay intact.
- Current release state: [v0.3.0](https://github.com/wangjohn/levenshtein/releases/tag/v0.3.0)
  is published at `a49d41322e457abb0ac453a575aa2763f3abd576`, with both `v0.3.0`
  and `runner/lint/v0.3.0` tags pointing to that commit. Marketplace listing
  validation and its verified link remain pending. No Marketplace listing or
  guessed badge URL was added.

Merged implementation PR: [#112](https://github.com/wangjohn/levenshtein/pull/112).
PR #112 merged as `7e9506eb807d37d62c98052bebaa90e3504fea81`; its ready-PR
checks, including all four release platforms and release smoke, passed.
