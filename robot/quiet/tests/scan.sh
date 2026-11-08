#!/usr/bin/env bash
# Host test of scan.sh (make check): fixture journals through the real quietmatch.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$here/../../../.git")")
tmp=$(mktemp -d "$main/robot/work/scan-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: scan.sh host test: $*" >&2; exit 1; }
j() { printf '%s\n' "2026/11/08 21:00:00 Bot 00e20100 Transcribed text: what time is it" "2026/11/08 21:00:09 Bot 00e20100 Transcribed text (et-EE): $1" >"$tmp/j"; }
j "vector shut up"
rc=0; out=$(SCAN_JOURNAL=$tmp/j "$here/../scan.sh" '1 min ago' 2>/dev/null) || rc=$?
((rc == 1)) && [[ $out == "HIT: vector shut up" ]] || die "'vector shut up': want exit 1 + HIT, got $rc: $out"
j "turn off the kitchen light"
SCAN_JOURNAL=$tmp/j "$here/../scan.sh" '1 min ago' >/dev/null || die "'turn off the kitchen light' read as a stop phrase"
mkdir -p "$tmp/bin"; printf '#!/bin/sh\necho "Failed to parse timestamp" >&2\nexit 1\n' >"$tmp/bin/journalctl"; chmod +x "$tmp/bin/journalctl"
rc=0; out=$(PATH="$tmp/bin:$PATH" "$here/../scan.sh" 'not a time' 2>&1) || rc=$?
((rc == 2)) && [[ $out == *SETUP:* ]] || die "failed journalctl: want exit 2 + SETUP, got $rc: $out"
echo "scan.sh host test OK"
