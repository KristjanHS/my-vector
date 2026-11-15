#!/usr/bin/env bash
# wire-pod-guard.nft loaded in a throwaway user+net namespace: every `tcp dport` rule's port set = want.
set -euo pipefail
export DOTFILES_DIR=/nonexistent MY_VECTOR_OVERLAY_DIR=/nonexistent # never this host's settings layers
dir=$(cd "$(dirname "$0")/../.." && pwd)
want="80 443 8080 8084 8091 8093"
nft=/usr/sbin/nft
unshare -rn true 2>/dev/null || { echo "guard test skipped: no unprivileged unshare"; exit 0; }
rules=$("$dir/scripts/guard-nft.sh" <"$dir/scripts/wire-pod-guard.nft" | unshare -rn sh -c "$nft -f - && $nft list table inet wirepod_guard" | grep 'tcp dport')
[[ $(wc -l <<<"$rules") -eq 7 ]] || { echo "FAIL guard: expected 7 tcp dport rules, got: $rules" >&2; exit 1; }
while read -r line; do
  got=$(sed -E 's/.*tcp dport \{ ([^}]*) \}.*/\1/' <<<"$line" | tr -d ' ' | tr , '\n' | sort -n | xargs)
  [[ $got == "$want" ]] || { echo "FAIL guard: ports {$got} != {$want} in: $line" >&2; exit 1; }
done <<<"$rules"
echo "guard OK: 7 rules guard {$want}"
