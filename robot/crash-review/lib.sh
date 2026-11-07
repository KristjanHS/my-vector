# shellcheck shell=bash
# Sourced by restarts.sh -d and catch.sh -d, after robot/env.sh: refuse to deploy a clone that is off the series tip,
# has uncommitted edits, or holds a source file newer than its last build; then point deploy-v.sh at the robot.
clone_ready() {
  local src=$1 fw=$2 tip stale
  tip=$("$fw/apply.sh" --check | tail -1) || { echo "SETUP: robot/firmware/apply.sh --check failed (above)" >&2; exit 2; }
  [[ $(git -C "$src" rev-parse HEAD) == "$tip" ]] ||
    { echo "SETUP: clone HEAD is not the series tip ${tip:0:12} (run robot/firmware/apply.sh, then build)" >&2; exit 2; }
  [[ -z $(git -C "$src" status --porcelain --untracked-files=no) ]] || { echo "SETUP: uncommitted edits in $src" >&2; exit 2; }
  stale=$(find "$src" \( -path "$src/_build" -o -name .git \) -prune -o -type f -newer "$src/_build/vicos/Release/.ninja_log" \
    ! -name .clangd ! -name compile_commands.json ! -name robot_ip.txt -print -quit)
  [[ -z $stale ]] || { echo "SETUP: ${stale#"$src"/} is newer than the last build (run ./build/build-v.sh in $src)" >&2; exit 2; }
  write_clone_ip "$src"
}
