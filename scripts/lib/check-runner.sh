# Parallel job runner (sourced by check.sh): `job NAME CMD [ARG...]` queues, `run_jobs LOGDIR` runs all at once.
# Each job is its own process group; the first failure kills the rest, prints its log and returns 1.
_cr_names=() _cr_cmds=() _cr_pids=()
job() { local q; _cr_names+=("$1"); shift; printf -v q '%q ' "$@"; _cr_cmds+=("$q"); }
_cr_us() { local t=${1/[.,]/}; echo $((10#$t)); }
_cr_kill() { local p; for p in "${_cr_pids[@]}"; do kill -TERM -- "-$p" 2>/dev/null || :; done; }
run_jobs() {
  local logs=$1 i pid rc failed='' t; local -A at=() t0=(); local -a state=() secs=()
  trap '_cr_kill; exit 130' INT TERM
  set -m
  for i in "${!_cr_names[@]}"; do
    (set +m; eval "${_cr_cmds[i]}") >"$logs/$i.log" 2>&1 </dev/null &
    at[$!]=$i t0[$i]=$EPOCHREALTIME _cr_pids+=("$!") state[i]=killed
  done
  set +m # -m only to give each job its own group: left on, bash reaps jobs into "Done" notices that wait -n then misses
  while ((${#at[@]})); do
    rc=0; wait -n -p pid || rc=$?
    i=${at[$pid]}; unset "at[$pid]"
    t=$(($(_cr_us "$EPOCHREALTIME") - $(_cr_us "${t0[$i]}"))); secs[i]=$((t / 1000000)).$((t / 100000 % 10))
    if ((rc)); then state[i]=FAIL failed=$i; break; fi
    state[i]=ok
  done
  if [[ -n $failed ]]; then _cr_kill; wait || :; fi
  trap - INT TERM
  for i in "${!_cr_names[@]}"; do
    printf '%-6s %-20s %6s  %s\n' "${state[i]}" "${_cr_names[i]}" "${secs[i]:+${secs[i]} s}" "$(tail -n1 "$logs/$i.log" | cut -c1-70)"
  done
  [[ -z $failed ]] && return 0
  { echo "--- ${_cr_names[failed]} failed (exit $rc), its log:"; cat "$logs/$failed.log"; } >&2
  return 1
}
