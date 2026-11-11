#!/usr/bin/env bash
# Both web ports serve through forkWebHandler() (webauth), never the bare DefaultServeMux.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
rc=0
for f in chipper/pkg/wirepod/config-ws/webserver.go chipper/pkg/wirepod/sdkapp/server.go; do
  wired=$(grep -cE 'ListenAndServe\(.*forkWebHandler\(\)\)' "$root/$f" || true)
  bare=$(grep -cE 'ListenAndServe\(.*, *nil\)' "$root/$f" || true)
  if [[ $wired != 1 || $bare != 0 ]]; then echo "FAIL: $f: $wired forkWebHandler() serve call(s), $bare bare nil (want 1 / 0)"; rc=1; fi
done
((rc == 0)) && echo "webauth wired OK: both ports"
exit $rc
