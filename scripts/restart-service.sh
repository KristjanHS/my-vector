#!/usr/bin/env bash
# `make start` once wire-pod.service is enabled: restart it, wait for :443 (go run compiles first), else print the journal and exit 1.
set -uo pipefail
sudo -n systemctl restart wire-pod || { echo "restart refused: no sudoers rule (docs/run-without-sudo-password.md) or start-limit hit (wait 2 min)" >&2; exit 1; }
for ((i = 0; i < 120; i++)); do
  [[ -n $(ss -Hltn 'sport = :443') ]] && { echo "wire-pod up on :443; logs: journalctl -u wire-pod -f"; exit 0; }
  systemctl is-failed -q wire-pod && break
  sleep 1
done
echo "wire-pod not listening on :443 after ${i}s" >&2
journalctl -u wire-pod -n 30 --no-pager >&2
exit 1
