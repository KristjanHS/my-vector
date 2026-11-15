#!/usr/bin/env bash
# scripts/lib/settings.sh layer precedence (defaults < dotfiles < companion < robot.env) and robot/env.sh's --no-defaults SETUP.
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
mkdir -p "$t/dot/shared-settings" "$t/priv"
printf '# c\nPC_LAN_IP=10.0.0.1\nPC_TAILNET_IP=10.0.0.4\nROBOT_IP=10.0.0.2\n' >"$t/dot/shared-settings/settings.env"
printf 'ROBOT_IP=10.0.0.3\nROBOT_ESN=0000abcd\nROBOT_SERVER=http://localhost:1\n' >"$t/priv/settings.env"
touch "$t/key"; printf 'ROBOT_KEY=%s\n' "$t/key" >"$t/robot.env"
fail() { echo "FAIL settings: $*" >&2; exit 1; }
export DOTFILES_DIR=$t/dot MY_VECTOR_OVERLAY_DIR=$t/priv
dump=$(bash "$dir/scripts/lib/settings.sh" --dump)
for want in PC_LAN_IP=10.0.0.1 ROBOT_IP=10.0.0.3 ROBOT_ESN=0000abcd PHONE_TAILNET_IP=192.0.2.53; do
  grep -qx "$want" <<<"$dump" || fail "--dump lacks $want: $dump"
done
nd=$(bash "$dir/scripts/lib/settings.sh" --dump --no-defaults)
grep -qx ROBOT_IP=10.0.0.3 <<<"$nd" || fail "--no-defaults dropped the companion layer: $nd"
if grep -q '^PHONE_TAILNET_IP=' <<<"$nd"; then fail "--no-defaults leaked a public default"; fi
rc=0; bash "$dir/scripts/lib/settings.sh" --dump --no-default >/dev/null 2>&1 || rc=$?
[[ $rc == 2 ]] || fail "settings.sh accepted a mistyped --no-defaults (rc=$rc)"
grep -qx 'define ROBOT_IP = 10.0.0.3' <<<"$("$dir/scripts/guard-nft.sh" </dev/null)" || fail "guard-nft.sh ignores the companion layer"
# shellcheck disable=SC2016 # the inner bash expands them
envsh() { env -u ESN -u SERVER -u ROBOT_TTS_URL -u ROBOT_IP -u MYVECTOR_EPOD_DIR ROBOT_ENV="$1" bash -c 'silent=1; . "$0/robot/env.sh"; echo "$ip $esn $tts_url"' "$dir"; }
[[ $(envsh "$t/robot.env") == "10.0.0.3 0000abcd http://10.0.0.4:8091" ]] || fail "env.sh layering: $(envsh "$t/robot.env" 2>&1)"
echo ROBOT_IP=10.0.0.9 >>"$t/robot.env"
[[ $(envsh "$t/robot.env") == "10.0.0.9 "* ]] || fail "robot.env does not override the settings layers"
export DOTFILES_DIR=/nonexistent MY_VECTOR_OVERLAY_DIR=/nonexistent
printf 'ROBOT_KEY=%s\nROBOT_ESN=0000abcd\nROBOT_SERVER=http://localhost:1\n' "$t/key" >"$t/bare.env" # all but ROBOT_IP
rc=0; out=$(envsh "$t/bare.env" 2>&1) || rc=$?
[[ $rc == 2 && $out == SETUP:* ]] || fail "no layers: want SETUP exit 2 (public placeholders must not reach env.sh), got rc=$rc: $out"
echo "settings OK: layer precedence, robot.env override, env.sh skips public defaults"
