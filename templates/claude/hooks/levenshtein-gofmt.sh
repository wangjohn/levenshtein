#!/usr/bin/env bash
# Claude Code PostToolUse hook: after the agent edits or writes a Go file,
# report it at once if gofmt would change it (the LV1005 rule), instead of at
# the end of the turn. It never rewrites the file. Copy to
# .claude/hooks/levenshtein-gofmt.sh; docs/agents.md in the Levenshtein
# repository explains it.
#
# It needs jq to read the hook input and gofmt on PATH, and does nothing
# without them. Exit 2 shows stderr to the agent; the edit itself stands.
set -uo pipefail

command -v jq > /dev/null 2>&1 || exit 0
command -v gofmt > /dev/null 2>&1 || exit 0
file=$(jq -r '.tool_input.file_path // empty' 2> /dev/null) || exit 0
if [[ $file != *.go || ! -f $file ]]; then
  exit 0
fi

if ! unformatted=$(gofmt -l -- "$file" 2>&1); then
  printf '%s does not parse:\n%s\n' "$file" "$unformatted" >&2
  exit 2
fi
if [[ -n $unformatted ]]; then
  echo "$file is not gofmt-formatted (LV1005). Run: gofmt -w $file" >&2
  exit 2
fi
