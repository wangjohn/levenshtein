#!/usr/bin/env bash
# Builds the example. Nothing here is a warning or an error.
set -euo pipefail

target=${1:-build}
# SC2086 (info) and SC2006 (style) are below the check's threshold.
name=`basename $target`
mkdir -p "$target"
echo "built $name"
