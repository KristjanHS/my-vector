# shellcheck shell=bash
# Robot connection facts for every robot/ script: sources the main checkout's robot/robot.env (gitignored; template robot.env.example; ROBOT_ENV overrides the file, for tests).
# Sets esn ip key key_old server ssh_base ssh_opts (env ESN / SERVER override); SETUP exit 2 when a fact is missing. tts_url (ROBOT_TTS_URL) is optional.
# epod_dir = wire-pod's EPOD_DIR (T2): MYVECTOR_EPOD_DIR (a relative one is under <main>/chipper, wire-pod's cwd), else <main>/secrets/epod.
common=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --path-format=absolute --git-common-dir 2>/dev/null) ||
  common=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.git # git refused (root on a user-owned repo, no safe.directory): this checkout
robot_dir=$(dirname "$common")/robot
# shellcheck disable=SC2034 # robot/epod/{apply,interlock}.sh read it
epod_dir=${MYVECTOR_EPOD_DIR:-${robot_dir%/robot}/secrets/epod}
[[ $epod_dir == /* ]] || epod_dir=${robot_dir%/robot}/chipper/$epod_dir
env_file=${ROBOT_ENV:-$robot_dir/robot.env}
[[ -f $env_file ]] ||
  { echo "SETUP: no $env_file; cp robot/robot.env.example robot/robot.env and fill it in" >&2; exit 2; }
# shellcheck source=robot/robot.env.example
. "$env_file"
esn=${ESN:-${ROBOT_ESN:-}} ip=${ROBOT_IP:-} server=${SERVER:-${ROBOT_SERVER:-}} key=${ROBOT_KEY:-}
# shellcheck disable=SC2034 # voice-push.sh reads it
tts_url=${ROBOT_TTS_URL:-}
[[ -z $key || $key == /* ]] || key=$robot_dir/$key
# The image's victor-dev key: only sshkey/apply.sh reads it, to bootstrap after a reflash.
key_old=${ROBOT_KEY_OLD:-work/wire-os-victor/robot_sshkey}
[[ $key_old == /* ]] || key_old=$robot_dir/$key_old
[[ -n $esn && -n $ip && -n $server && -f $key ]] ||
  { echo "SETUP: $env_file needs ROBOT_ESN, ROBOT_IP, ROBOT_SERVER and ROBOT_KEY (an existing file)" >&2; exit 2; }
ssh_base=(-o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa)
# ssh_opts reuse the master mux_open starts (make ship), else connect directly; ssh_base has no ControlPath (hostkey/sshkey pass ControlPath=none).
# shellcheck disable=SC2034 # read by the sourcing scripts
ssh_opts=(-i "$key" "${ssh_base[@]}" -o "ControlPath=${XDG_RUNTIME_DIR:-$robot_dir/work}/mv-ssh-%C")
mux_open() { # a dead master's socket file would make the new one run unmultiplexed: remove it first
  ssh "${ssh_opts[@]}" -O check "root@$ip" 2>/dev/null && return 0
  rm -f "$(ssh -G "${ssh_opts[@]}" "root@$ip" 2>/dev/null | awk '$1 == "controlpath" { print $2 }')"
  ssh "${ssh_opts[@]}" -fN -o ControlMaster=yes -o ControlPersist=600 -o ServerAliveInterval=3 -o ServerAliveCountMax=2 "root@$ip" </dev/null >/dev/null 2>&1 || true; }
mux_close() { ssh "${ssh_opts[@]}" -O exit "root@$ip" 2>/dev/null || true; }
# The clone's build/deploy-v.sh reads the robot IP from <clone>/robot_ip.txt.
write_clone_ip() { [[ -d $1 ]] || { echo "SETUP: no victor clone at $1" >&2; exit 2; }; printf '%s\n' "$ip" >"$1/robot_ip.txt"; }
