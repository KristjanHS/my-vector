# shellcheck shell=bash
# Ship receipts (sourced by ship.sh): one annotated tag ship/<YYYYMMDD-HHMM>-<sha7> per green ship, pushed with the sha.
# The body carries counts and hashes only — never an IP, ESN or hostname (origin is public); receipt_body refuses a dotted quad.
# Tested by tests/receipt.sh.

# receipt_link <ip>: tailnet when the robot address is in Tailscale's 100.64.0.0/10, else lan.
receipt_link() {
  local a b
  IFS=. read -r a b _ <<<"$1"
  if [[ $a == 100 && $b =~ ^[0-9]+$ ]] && ((b >= 64 && b <= 127)); then echo tailnet; else echo lan; fi
}

# receipt_body <shipped commit> <server sha> <fw-tree hash|unchanged> <full|server> <tailnet|lan>: the tag message; exit 1 on a dotted quad.
receipt_body() {
  local body
  body=$(printf 'ship %s\n\nserver %s\nfw-tree %s\nsmoke %s PASS\nlink %s\n' "${1:0:7}" "$2" "$3" "$4" "$5")
  if [[ $body =~ [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ ]]; then echo "receipt body carries an address: ${BASH_REMATCH[0]}" >&2; return 1; fi
  printf '%s\n' "$body"
}

# receipt_tag <repo> <commit> <server sha> <fw-tree|unchanged> <full|server> <ip>: creates the tag, prints its name.
receipt_tag() {
  local tag body
  tag=ship/$(TZ=Europe/Tallinn date +%Y%m%d-%H%M)-$(git -C "$1" rev-parse --short=7 "$2")
  body=$(receipt_body "$(git -C "$1" rev-parse "$2")" "$3" "$4" "$5" "$(receipt_link "$6")") || return 1
  git -C "$1" tag -a -m "$body" "$tag" "$2" >&2 || return 1
  echo "$tag"
}
