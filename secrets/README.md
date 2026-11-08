# secrets/ — keys, certs, tokens and bot-state backups

Everything in this folder is secret. Git tracks only this README and `.gitignore`, because `origin` is public. `scripts/tests/secrets.sh`, run by `make check`, fails if anything else here is tracked (or in an unpushed commit, when `make ship` checks), or isn't ignored. Keep the folder at mode 700 and private keys at mode 600.

| path | what it is | made by | used by |
|---|---|---|---|
| `myvector_ed25519` (+ `.pub`) | robot root SSH key (ed25519, no passphrase) | `robot/sshkey/keygen.sh` | every robot script, via `ROBOT_KEY=../secrets/myvector_ed25519` in `robot/robot.env`; installed on the robot by `robot/sshkey/apply.sh` |
| `server/` | wire-pod's private state: `apiConfig.json` (API keys), `myvector*.json` (fork settings incl. the Claude and search keys, prompt, search cache, memory), `jdocs/` (bot tokens), `session-certs/`, `certs/` (TLS key, `server_config.json`) | wire-pod itself, `setup.sh` (certs) | wire-pod (cwd `chipper/`, paths in `chipper/pkg/vars/fork_paths.go`), `tts/` + `et-stt/` + `robot/firmware/settings.sh` (read `server/myvectorConfig.json`) |
| `server-state/server-state-*.tgz` | snapshot of `server/` + `chipper/source.sh` | `robot/unlock/backup-server-state.sh` | restore by hand |
| `epod/` `ca.{crt,key}`, `ep.{crt,key}` | planned (Stage 4): own escapepod CA and server TLS leaf | planned `robot/epod/mkcert.sh` → `epod-new/`, then renamed to `epod/` | planned: wire-pod `EPOD_DIR` |

Not secret, so not here: `chipper/source.sh` (two flags), `robot/robot.env` and `robot/backup/robot-id-*.txt` (ESN, IP), the image's public `victor-dev` key at `robot/work/wire-os-victor/robot_sshkey`, and upstream's public `chipper/epod/ep.key`.

Rolling wire-pod back past cac09ab (`make server-pin <older sha>`) means moving `server/`'s files back to `chipper/` and `certs/` first: older builds look only there.

## Backup

Keep an offline copy of this whole folder somewhere other than this PC. Losing `myvector_ed25519` after the robot has been switched to it means getting back in through the image key, which only works after a reflash.

Restore with **LF line endings**. A copy that went through Windows can come back as CRLF, and then OpenSSH fails with `Load key …: error in libcrypto`. From the repo root, fix it with `(umask 077; tr -d '\r' <in >secrets/myvector_ed25519)`, then check it without printing the key:
`[[ $(ssh-keygen -y -P '' -f secrets/myvector_ed25519 | cut -d' ' -f1,2) == "$(cut -d' ' -f1,2 secrets/myvector_ed25519.pub)" ]] && echo OK`
