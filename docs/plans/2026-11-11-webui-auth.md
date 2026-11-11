# Web-UI auth on :80 / :8080 (T4)

Status: planned, not started. Executes item 4 of `2026-11-07-public-wifi-security.md (docs/plans/)` §Recommended plan,
T4 part only ("auth on the web API"). Out of scope: TTS :8091, chipper gRPC :443/:8084 (and its private `/ok` mux),
hotspot/router. The nft guard (`scripts/wire-pod-guard.nft`) stays as the outer layer. Every `[USER]` step is a live run:
Claude asks first. A step that makes robot sound runs only after `robot/quiet/quiet.sh status` ≠ 3 and the user's yes,
then `robot/quiet/scan.sh '<start>'` (exit 1 → stop sound tests, `make quiet`, tell the user).

## Refactor-first assessment

Skip test fires: no restructure first. `config-ws.StartWebServer` and `sdkapp.BeginServer` both serve
`http.DefaultServeMux` via `ListenAndServe(…, nil)`; that `nil` is the single seam. One wrapper at the two call sites
covers every route, fork routes included, with a 1-line edit each. Per-route wrapping (~25 `HandleFunc` sites over 9
files) would grow the upstream diff and miss any route an upstream merge adds.

## Rulings

| id | ruling | where |
|---|---|---|
| D1 | One handler `webauth.Handler(next)` (process singleton, `sync.Once`), so sessions and rate limits are shared by both ports (cookies ignore the port). Upstream edits: `ListenAndServe(":"+vars.WebPort, forkWebHandler())` and `ListenAndServe(":80", forkWebHandler())`; `forkWebHandler()` lives in one `fork_webauth.go` per package and returns `webauth.Handler(http.DefaultServeMux)` (no upstream import change). | `chipper/pkg/myvector/webauth/`; `config-ws/{webserver.go,fork_webauth.go}`; `sdkapp/{server.go,fork_webauth.go}`; `scripts/upstream-drift.sh` |
| D2 | Browsers: password-only login page (`LoginPath`, inline CSS, `go:embed`), POST form → `Set-Cookie: CookieName` = `exp.nonce.HMAC-SHA256(SessionKey)`, `HttpOnly; SameSite=Strict; Path=/`, no `Secure` (plain HTTP; browser paths are lo or WireGuard-encrypted tailnet). Stateless, survives a restart; sliding re-issue when < `SessionRenew` is left. Webroot `fetch`/XHR are same-origin → cookie rides along, **no JS change**. | `webauth/{session.go,login.go,login.html}` |
| D3 | Scripts: `Authorization: Bearer <token>`. Header line kept in `WebAuthHeaderPath`; callers run `curl -H @"$wp_auth_header"` so the token never hits argv. Server keeps only `sha256(token)`, compared with `subtle.ConstantTimeCompare`. Bearer requests skip the CSRF checks (D6). | `webauth/store.go`; `scripts/lib/wirepod-auth.sh` |
| D4 | Bootstrap: `WebAuthPath` absent at start → generate password, token, `SessionKey`; write `WebAuthPath` (bcrypt hash, token sha256, key), `WebPasswordPath`, `WebAuthHeaderPath`, all 0600, dirs 0700; log the **paths**, never a value. Present but unparsable → fail closed (every non-exempt path 503 + one log line), never regenerate over it. Reset/rotate = delete `WebAuthPath` + restart (all sessions die: new key). | `webauth/store.go`; `vars/fork_paths.go`; `secrets/README.md` |
| D5 | Exempt set (exact paths unless marked): `/ok`, `/ok:80` (robot keep-alive; `robot/netguard/apply.sh` probe); `LoginPath`, `LogoutPath`; `GET /api/get_ota/` prefix (robot fetches the OTA from `:WebPort/api/get_ota/…` during BLE setup — `setup/ble.go` `otaUrl`; public firmware proxy, no secret; upstream's switch matches only bare `get_ota`, so it 404s either way). Everything else needs auth, incl. `/session-certs/`, `/sdk-app`, `/cam-stream`. | `webauth` `exempt()` |
| D6 | CSRF (cookie-authenticated only): refuse 403 when `Sec-Fetch-Site` ∈ {`cross-site`,`same-site`} (another `localhost:<port>` page is same-site); unsafe methods (not GET/HEAD/OPTIONS) also need `Origin` (else `Referer`) host == `r.Host`. Login POST gets the same Origin check. | `webauth/csrf.go` |
| D7 | CORS: for `/api/` responses the wrapper deletes `Access-Control-Allow-Origin`/`-Headers` before `WriteHeader` (a header-filter writer applied to `/api/` only, so `/cam-stream` keeps its own `Flusher`). No upstream edit to `apiHandler`. | `webauth/cors.go` |
| D8 | Loopback is **not** exempt: under mirrored WSL, Windows localhost arrives on lo (`loopback0`, guard comment), so any Windows process or local web page reaches :8080 as 127.0.0.1; the robot reaches the laptop over LAN/tailnet, never lo; scripts get the token from a file for free. | `webauth` tests |
| D9 | Unauthenticated reply: `GET` with `Accept` containing `text/html` → 303 to `LoginPath?next=<path>` (`next` must start with `/`, not `//`, else `/`); everything else → 401 + `WWW-Authenticate: Bearer realm="wire-pod"`. | `webauth/handler.go` |
| D10 | Login rate limit: per source IP `LoginFailMax` failures per `LoginWindow` → 429 + `Retry-After`; process-wide `LoginFailGlobal` per window → 429 for all; bcrypt cost adds ~0.25 s per try. Body capped at `MaxLoginBody`. | `webauth/limit.go` |
| D11 | `/session-certs/<esn>`: not robot-fetched — the robot clone's `cloud/` has no reference; only PC-side SDK tools do (`tools/sdk/…/configure`, `download_cert.sh`), and those read Anki's URL. Behind auth. ⚠ hedge; falsifier: `grep -rIl session-certs robot/work/wire-os-victor --exclude-dir=tools --exclude-dir=apps` non-empty → exempt it. | D5 |
| D12 | BLE setup after a factory reset: credentials exist from the first start (D4), so the user logs in, then runs the wizard (`initial.js`, `ble.js` → `/api-ble/*`, `/api/*`) as today; the robot's only HTTP call in that flow is D5's `get_ota`. No chicken-and-egg. | — |
| D13 | :80 gets the same handler as :8080 (one policy); narrowing :80 to `/ok` only is not done. | D1 |
| D14 | Own password: `go run ./pkg/myvector/webauth/cmd/passwd` (reads stdin, rewrites the hash in `WebAuthPath`, deletes `WebPasswordPath`); `make webauth-passwd` wraps it. | `webauth/cmd/passwd/`; `Makefile` |
| DK | Key readback after login → see `[USER DECISION] DK`; default B. | Stage 4 |

### [USER DECISION] DK — can a logged-in browser still read the stored API keys?

- **A — readable after login.** `/api/get_config`, `/api/get_kg_api`, `/api/get_weather_api` keep returning keys to an
  authenticated caller. Settings page unchanged; a stolen session or token = the LLM key. Stage 4 dropped.
- **B — write-only (recommended).** The wrapper rewrites those responses' non-empty key fields to `MaskedKey`; a
  `set_*` POST carrying `MaskedKey` gets the current key substituted before upstream sees it. Settings page shows
  `MaskedKey`, saving unchanged keeps the key; reading a key back needs `secrets/server/apiConfig.json`.

## Constants (source = `webauth` consts / `vars/fork_paths.go`)

| symbol | value | validated by |
|---|---|---|
| `CookieName` | `mv_session` | `TestLoginSetsStrictCookie` |
| `SessionTTL` / `SessionRenew` | 14 d / re-issue when < 7 d left | `TestSessionExpiry`, `TestSessionRenew` |
| `LoginPath` / `LogoutPath` | `/myvector/login` / `/myvector/logout` | `TestExemptSet` |
| `BcryptCost` | 12 | `TestStoreBootstrap` (`bcrypt.Cost`) |
| password / token / `SessionKey` | 26 chars base32 (130 bit) / 32 B base64url / 32 B | `TestStoreBootstrap` |
| `LoginFailMax` / `LoginWindow` / `LoginFailGlobal` | 5 / 15 min / 30 | `TestLoginRateLimit` |
| `MaxLoginBody` | 4 KiB | `TestLoginBodyCap` |
| `vars.WebAuthPath` | `SecretsDir + "/webauth.json"` (`secrets/server/`) | `git check-ignore -v secrets/server/webauth.json` |
| `vars.WebPasswordPath` | `../secrets/webui-password` | `git check-ignore -v secrets/webui-password` |
| `vars.WebAuthHeaderPath` / env `WIREPOD_AUTH_HEADER` | `../secrets/wirepod-auth.header` (`Authorization: Bearer <token>`) | `scripts/tests/wirepod-auth.sh` |
| `MaskedKey` (DK=B) | `********` | `TestRedactKeys` |

## Stage 1 — `chipper/pkg/myvector/webauth` (no wiring, nothing live)

Focus: `chipper/pkg/myvector/webauth/*.go`, `login.html`; `golang.org/x/crypto/bcrypt` (already in `chipper/go.mod`).

| slice | task | falsifier (red demo first) | gate |
|---|---|---|---|
| Stage 1.1 | `store.go`: `Load(paths)` → bootstrap (D4), `CheckPassword`, `CheckToken`; `session.go`: `Issue`/`Verify` (D2) | `TestStoreBootstrap` (temp dir: 3 files, mode 0600, header line shape, log has paths, no value); `TestStoreCorruptFailsClosed`; `TestCheckToken` (right/wrong/empty); `TestSessionExpiry` + `TestSessionTamper` (flip one MAC byte). Red: make `Verify` skip the `exp` check → `TestSessionExpiry` fails; restore | `cd chipper && go test ./pkg/myvector/webauth/...`; `make check` |
| Stage 1.2 | `handler.go` + `csrf.go` + `cors.go` + `limit.go` + `login.go`: `Handler(next)` with D5–D10, login/logout, sliding renew | httptest table over a stub mux: no creds from `127.0.0.1:5000` → 401 (D8); HTML GET → 303 with safe `next` (`//evil` → `/`); bearer → 200; cookie → 200; cookie + `Sec-Fetch-Site: same-site` → 403; cookie POST with foreign `Origin` → 403; exempt `/ok`, `/ok:80`, `GET /api/get_ota/x` → reach stub, `POST /api/get_ota/x` → 401; `/api/` reply has no `Access-Control-Allow-Origin`; 6th bad login → 429; `TestLoginSetsStrictCookie`. Red: add `/api/` to `exempt()` → table fails; restore. Red: drop the `same-site` arm → its row fails; restore | same |

## Stage 2 — wire both ports + browser

Focus: `config-ws/{webserver.go,fork_webauth.go}`, `sdkapp/{server.go,fork_webauth.go}`, `vars/fork_paths.go`,
`scripts/upstream-drift.sh`, `scripts/tests/webauth-wired.sh` (new), `scripts/check.sh`, `secrets/README.md`, `chipper/webroot/index.html`.

| slice | task | falsifier (red demo first) | gate |
|---|---|---|---|
| Stage 2.1 | D1 glue + 2 line edits; `vars.WebAuth*` paths; drift: extend the `webserver.go` comment, add `chipper/pkg/wirepod/sdkapp/server.go # ListenAndServe(":80", forkWebHandler()) (fork_webauth.go, webauth)`; `secrets/README.md` lists the 3 files | `scripts/tests/webauth-wired.sh`: each of the 2 files has exactly 1 `ListenAndServe(.*forkWebHandler())` and 0 `ListenAndServe(.*, nil)`. Red: revert `server.go` to `nil` → FAIL; restore. `scripts/upstream-drift.sh` red: remove the new entry → FAIL; restore. `TestForkWebHandlerGuards` (sdkapp, `-run Fork`): `forkWebHandler()` on `/api-sdk/x` without creds → 401 | `make check` (new `opt webauth-wired` line) |
| Stage 2.2 | `index.html` nav gains a `LogoutPath` link (file already in `allowed_edits`; extend its comment) | `TestLogoutClearsCookie`: reply sets `mv_session` with `Max-Age=0` + 303 to `LoginPath`. Red: drop `MaxAge` → FAIL; restore | `make check` |

Do not restart the live wire-pod between 2.1 and 3.2: script callers lack the token until Stage 3.

## Stage 3 — script callers send the token

Focus: `scripts/lib/wirepod-auth.sh` (new: `wp_auth_header=${WIREPOD_AUTH_HEADER:-<main checkout>/secrets/wirepod-auth.header}`,
`wp_auth=(-H "@$wp_auth_header")`, SETUP exit 2 when the file is missing; main checkout found as `robot/env.sh` does), `robot/env.sh` sources it.

| slice | task | falsifier (red demo first) | gate |
|---|---|---|---|
| Stage 3.1 | helper + `scripts/tests/wirepod-auth.sh`; `scripts/llm.sh` (`get_kg`/`set_kg`), `scripts/live-check.sh` (every `$api` curl) | test: temp header file → `wp_auth` = `-H @<file>`; missing file → exit 2 + SETUP line; a fake `curl` on PATH run under `llm.sh show` logs `-H @…wirepod-auth.header`. Red: drop `"${wp_auth[@]}"` from `get_kg` → FAIL; restore | `make check` (new `opt` line) |
| Stage 3.2 | `robot/firmware/{settings,smoke}.sh`, `robot/openmic/{lib,probe}.sh`, `robot/volume-probe/{lib,pcvoice}.sh`, `robot/unlock/power-stress.sh`: every `$server`/`$api` curl gets `"${wp_auth[@]}"` | `robot/firmware/tests/pc-voice.sh`: its curl stub log must show `-H @` on every `/api-sdk/` line. Red: remove it from one `settings.sh` curl → FAIL; restore. `grep -rnE 'curl[^|]*(\$server|\$api|:8080)' scripts robot --include=*.sh \| grep -v wp_auth` → empty | `make check` |
| Stage 3.3 `[USER]` | deploy (server pin moves via `make ship`, user's ask only; then `make start`) — silent checks only | see Stage 5 rows 1–4 | — |

## Stage 4 — key readback (only if DK = B)

Focus: `webauth/redact.go`, `webauth/redact_test.go`.

| slice | task | falsifier (red demo first) | gate |
|---|---|---|---|
| Stage 4.1 | authenticated GET `/api/get_config`, `/api/get_kg_api`, `/api/get_weather_api`: buffer the upstream body, set non-empty `Weather.Key`, `Knowledge.Key` (and `key` fields of the two `get_*_api` replies) to `MaskedKey`; POST `set_kg_api`/`set_weather_api` with key == `MaskedKey` → substitute `vars.APIConfig`'s current key | `TestRedactKeys` (stub handler returns a known key → body lacks it, has `MaskedKey`); `TestMaskedKeyRoundTrip` (POST `MaskedKey` → stub sees the original key). Red: skip the `Knowledge.Key` rewrite → `TestRedactKeys` fails; restore | `go test ./pkg/myvector/webauth/...`; `make check` |

## Stage 5 — full verify, live e2e, review

| # | check (ranked, silent first) | correct |
|---|---|---|
| 0 | `make check` | exit 0 |
| 1 | from the laptop: `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/get_config` and `:80/api/get_config` | 401 both |
| 2 | `curl -s -o /dev/null -w '%{http_code}' -H @secrets/wirepod-auth.header http://127.0.0.1:8080/api/get_config` | 200 (DK=B: body holds `********`, no key) |
| 3 | `curl -s http://127.0.0.1/ok`; robot read-only `ssh … curl -s -m4 http://escapepod.local/ok`; `robot/netguard/apply.sh --selftest` | 200 / 200 / PASS |
| 4 | `[USER]` browser on the laptop → `http://localhost:8080/` | redirect to `/myvector/login`; `secrets/webui-password` logs in; settings + `/myvector/voice` pages load; DevTools cookie `mv_session` HttpOnly, SameSite=Strict |
| 5 | `[USER]` phone over tailnet → `http://<wsl tailnet ip>:8080/` | login page; login works; logout link returns to login |
| 6 | `scripts/llm.sh show`; `scripts/live-check.sh` silent parts | no 401 |
| 7 | `[USER]` sound — quiet status ≠ 3 + ask: one voice turn ("Hey Vector, what time is it?"), then `robot/quiet/scan.sh` | robot answers; scan exit 0 |
| 8 | code review: `Agent(subagent_type="code-reviewer")` over Stages 1–4 diff (CSRF/redirect/limit logic, fail-closed paths, drift entries) | findings fixed or ruled |

Archive at ship per `plan-hygiene.md`; owed `[USER]` rows go to `docs/backlog.md` §OWED[USER].
