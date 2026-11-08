# secrets/ — keys, certs and bot-state backups

Everything in this folder is secret. Git tracks only this README and `.gitignore`, because `origin` is public. `scripts/tests/secrets.sh`, run by `make check`, fails if anything else here gets tracked or stops being ignored. Keep the folder at mode 700 and private keys at mode 600.

| file | what it is | made by | used by |
|---|---|---|---|
| `myvector_ed25519` (+ `.pub`) | robot root SSH key (ed25519, no passphrase) | `robot/sshkey/keygen.sh` | every robot script, via `ROBOT_KEY=../secrets/myvector_ed25519` in `robot/robot.env`; installed on the robot by `robot/sshkey/apply.sh` |
| `epod/` `ca.{crt,key}`, `ep.{crt,key}` | own escapepod CA and server TLS leaf (plan Stage 4) | `robot/epod/mkcert.sh` → `epod-new/`, then renamed to `epod/` | wire-pod (`EPOD_DIR`) |
| `server-state/server-state-*.tgz` | bot state: apiConfig, botConfig, jdocs, session-certs, source.sh | `robot/unlock/backup-server-state.sh` | restore by hand |

Not here: files that upstream code reads in place (`chipper/session-certs/`, `chipper/apiConfig.json`, …). They stay put and the root `.gitignore` covers them. The image's public `victor-dev` key stays at `robot/work/wire-os-victor/robot_sshkey`.

## Backup

Keep an offline copy of this whole folder somewhere other than this PC. Losing `myvector_ed25519` after the robot has been switched to it means getting back in through the image key, which only works after a reflash.

Restore with **LF line endings**. A copy that went through Windows can come back as CRLF, and then OpenSSH fails with `Load key …: error in libcrypto`. Fix it with `tr -d '\r' <in >out`, then check it without printing the key:
`[[ $(ssh-keygen -y -P '' -f secrets/myvector_ed25519 | cut -d' ' -f1,2) == "$(cut -d' ' -f1,2 secrets/myvector_ed25519.pub)" ]] && echo OK`
