#!/usr/bin/env bash
# Claude Code Stop hook: while the repository has uncommitted Go changes and
# Levenshtein's fast run fails, keep the agent working and hand it the
# findings. Copy to .claude/hooks/levenshtein-stop.sh; docs/agents.md in the
# Levenshtein repository explains it.
#
#   LEVENSHTEIN          the pinned Levenshtein checkout
#                        (default: ../levenshtein beside the project)
#   LEVENSHTEIN_RUN      the run to execute (default: branch); keep it native
#   LEVENSHTEIN_STOP_ONCE=1
#                        block only the first stop of a turn, so the agent can
#                        finish with a report after one attempt at the fixes
#
# Exit 0 lets the agent stop, exit 2 blocks it with stderr as the reason, and
# any other exit shows the user a hook error without blocking.
set -uo pipefail

input=$(cat)
project=${CLAUDE_PROJECT_DIR:-$PWD}
field() {
  command -v jq > /dev/null 2>&1 && printf '%s' "$input" | jq -r "$1 // empty" 2> /dev/null
}

if [[ ${LEVENSHTEIN_STOP_ONCE:-0} == 1 && $(field .stop_hook_active) == true ]]; then
  exit 0
fi

# In a worktree CLAUDE_PROJECT_DIR stays at the original checkout; the hook
# input's cwd is where the agent is working.
cwd=$(field .cwd)
source=$(git -C "${cwd:-$project}" rev-parse --show-toplevel 2> /dev/null) || source=${cwd:-$project}
levenshtein=${LEVENSHTEIN:-$project/../levenshtein}
run=${LEVENSHTEIN_RUN:-branch}

# Nothing Go-related changed since the last commit, and no commit the branch
# has not pushed to its upstream touches Go: nothing to check. Agents often
# commit before they stop, so committing alone must not skip the run. A branch
# without an upstream is judged on uncommitted changes only. Outside a git work
# tree the change is unknown, so the run goes ahead.
paths=('*.go' '*go.mod' '*go.sum' '*go.work' 'levenshtein.json' '.levenshtein')
unpushed=$(git -C "$source" diff --name-only '@{upstream}...HEAD' -- "${paths[@]}" 2> /dev/null) || unpushed=''
if changes=$(git -C "$source" status --porcelain -- "${paths[@]}" 2> /dev/null) && [[ -z $changes && -z $unpushed ]]; then
  exit 0
fi

if [[ ! -x $levenshtein/verify ]]; then
  echo "Levenshtein hook: no verify launcher at $levenshtein; set LEVENSHTEIN to the pinned checkout" >&2
  exit 1
fi

report=$("$levenshtein/verify" "$run" --source "$source" --format text 2>&1)
status=$?
case $status in
  0)
    exit 0
    ;;
  1)
    {
      echo "Levenshtein's $run run fails. Fix every finding below before finishing."
      echo "A hint line gives a known fix. Suppress a finding only when it is a deliberate"
      echo "exception, with //lint:ignore CODE reason on the line above it."
      echo
      echo "$report"
    } >&2
    exit 2
    ;;
  *)
    {
      echo "Levenshtein could not run $run (exit $status):"
      echo "$report"
    } >&2
    exit 1
    ;;
esac
