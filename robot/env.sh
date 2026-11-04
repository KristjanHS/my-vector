# shellcheck shell=bash
# Robot connection facts for every robot/ script: sources the main checkout's robot/robot.env (gitignored; template robot.env.example).
# Sets esn ip key server ssh_opts (env ESN / SERVER override); SETUP exit 2 when a fact is missing.
robot_dir=$(dirname "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --path-format=absolute --git-common-dir)")/robot
[[ -f $robot_dir/robot.env ]] ||
  { echo "SETUP: no $robot_dir/robot.env; cp robot/robot.env.example robot/robot.env and fill it in" >&2; exit 2; }
# shellcheck source=robot/robot.env.example
. "$robot_dir/robot.env"
esn=${ESN:-${ROBOT_ESN:-}} ip=${ROBOT_IP:-} server=${SERVER:-${ROBOT_SERVER:-}} key=${ROBOT_KEY:-}
[[ -z $key || $key == /* ]] || key=$robot_dir/$key
[[ -n $esn && -n $ip && -n $server && -f $key ]] ||
  { echo "SETUP: $robot_dir/robot.env needs ROBOT_ESN, ROBOT_IP, ROBOT_SERVER and ROBOT_KEY (an existing file)" >&2; exit 2; }
# shellcheck disable=SC2034 # read by the sourcing scripts
ssh_opts=(-i "$key" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa)
# The clone's build/deploy-v.sh reads the robot IP from <clone>/robot_ip.txt.
write_clone_ip() { printf '%s\n' "$ip" >"$1/robot_ip.txt"; }
