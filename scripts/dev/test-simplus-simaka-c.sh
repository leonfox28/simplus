#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source_dir="$root/third_party/strongswan/simplus_simaka"
output="${TMPDIR:-/tmp}/simplus-simaka-agent-test"

cc -std=c11 -Wall -Wextra -Werror -Wpedantic -DSIMPLUS_SIMAKA_TEST \
  -I"$source_dir" \
  "$source_dir/simplus_simaka_agent.c" \
  "$source_dir/tests/agent_test.c" \
  -o "$output"
"$output"
