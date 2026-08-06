#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source_dir="$root/components/strongswan-simplus-simaka"
work=$(mktemp -d "${TMPDIR:-/tmp}/simplus-simaka-agent-test.XXXXXX")
cleanup() { [[ -d $work ]] && rm -rf -- "$work"; }
trap cleanup EXIT HUP INT TERM
output="$work/agent-test"

cc -std=c11 -Wall -Wextra -Werror -Wpedantic -DSIMPLUS_SIMAKA_TEST \
  -I"$source_dir" \
  "$source_dir/simplus_simaka_agent.c" \
  "$source_dir/tests/agent_test.c" \
  -o "$output"
"$output"
