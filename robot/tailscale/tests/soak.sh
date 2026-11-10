#!/usr/bin/env bash
# Host test of robot/tailscale/soak.sh (make check): a fake ssh replays $tmp/samples, one line per call; SOAK_INTERVAL=0.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
tmp=$(mktemp -d "$main/robot/work/tailscale-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: tailscale soak test: $*" >&2; exit 1; }

mkdir -p "$tmp/bin"
cat >"$tmp/bin/ssh" <<'EOF'
#!/bin/sh
n=$(($(cat "$FAKE_N" 2>/dev/null || echo 0) + 1)); echo "$n" >"$FAKE_N"
sed -n "${n}p" "$FAKE_SAMPLES"
EOF
chmod +x "$tmp/bin/ssh"
: >"$tmp/key"
printf '%s\n' ROBOT_ESN=0 ROBOT_IP=192.0.2.9 ROBOT_SERVER=x "ROBOT_KEY=$tmp/key" >"$tmp/robot.env"
pids='vic-anim.service=790 vic-init.service=0'
run() { # min, then sample lines
  local m=$1; shift; printf '%s\n' "$@" >"$tmp/samples"; rm -f "$tmp/n"
  PATH="$tmp/bin:$PATH" ROBOT_ENV=$tmp/robot.env FAKE_SAMPLES=$tmp/samples FAKE_N=$tmp/n SOAK_INTERVAL=0 \
    "$here/../soak.sh" "$m" 2>&1
}

out=$(run 3 "26000 214000 1 $pids" "29000 200000 1 $pids" "27000 210000 1 $pids") || die "clean samples failed: $out"
[[ $out == *"max VmRSS 29000 kB"*"min MemAvailable 200000 kB"* ]] || die "clean summary: $out"
[[ $(cat "$tmp/n") == 3 ]] || die "clean run took $(cat "$tmp/n") samples, want 3"

if out=$(run 3 "26000 214000 1 $pids" "41000 214000 1 $pids" "26000 214000 1 $pids"); then die "RSS 41000 passed: $out"; fi
[[ $out == *RSS_CAP* ]] || die "RSS over cap out: $out"

if out=$(run 2 "26000 214000 1 $pids" "26000 214000 1 vic-anim.service=1234 vic-init.service=0"); then die "vic-anim restart passed: $out"; fi
[[ $out == *"MainPID changed"* ]] || die "vic restart out: $out"

if out=$(run 2 "26000 214000 1 $pids" "26000 214000 0 $pids"); then die "tailscale0 down passed: $out"; fi
[[ $out == *"not UP"* ]] || die "tailscale0 down out: $out"

if out=$(run 2 "26000 214000 1 $pids" "0 214000 0 $pids"); then die "dead tailscaled passed: $out"; fi
[[ $out == *"no tailscaled"* ]] || die "dead tailscaled out: $out"
echo "tailscale soak test OK"
