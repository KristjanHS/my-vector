# my-vector — personal fork of wire-pod

Self-hosted server for the Anki Vector robot. Own repo, not a GitHub fork; changes never go back upstream.

- **Remotes**: `origin` = `KristjanHS/my-vector` (push here); `upstream` = `kercre123/wire-pod` (fetch-only, push URL disabled). Update: `git fetch upstream && git merge upstream/main` — merge, never rebase (`main` is published).
- **Keep the upstream diff small**: fork code goes in `chipper/pkg/myvector/`, glue into an upstream package in a `fork_*.go` file; any other upstream edit must be listed in `scripts/upstream-drift.sh` (run it before committing — it fails on unlisted ones). Don't edit upstream `README.md`/docs — own notes go in `docs/`.
- **`origin` is PUBLIC**: never commit keys or bot state. `.gitignore` already covers `chipper/apiConfig.json`, `botConfig.json`, `jdocs/*.json`, `session-certs/`, `*.key`, `source.sh` — `git check-ignore -v <path>` before adding anything config-like.
- **Layout**: Go server in `chipper/` (module `github.com/kercre123/wire-pod/chipper`, entrypoints `chipper/cmd/{vosk,coqui,leopard,experimental}`, code in `chipper/pkg/`, web UI in `chipper/webroot/`); `vector-cloud/` is a separate DDL Go module. Software that runs on the unlocked robot goes in `robot/<name>/`, one folder each (`robot/unlock/` first). Downloads, robot backups and scratch/clones (e.g. `wire-os-victor`) go in the gitignored `robot/ota/`, `robot/backup/`, `robot/work/` — never `/tmp` or the session scratchpad. Setup/run: `sudo ./setup.sh`; `make daemon-enable` runs wire-pod as you (only `CAP_NET_BIND_SERVICE`), foreground `sudo ./chipper/start.sh` as root.
- **License**: MIT — keep `LICENSE` and the original copyright notice.
