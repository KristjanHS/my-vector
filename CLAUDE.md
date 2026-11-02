# my-vector — personal fork of wire-pod

Self-hosted server for the Anki Vector robot. Own repo, not a GitHub fork; changes never go back upstream.

- **Remotes**: `origin` = `KristjanHS/my-vector` (push here); `upstream` = `kercre123/wire-pod` (fetch-only, push URL disabled). Update: `git fetch upstream && git merge upstream/main` — merge, never rebase (`main` is published).
- **Keep the upstream diff small**: prefer new files/packages over editing upstream ones; every edited upstream line is a future merge conflict. Don't edit upstream `README.md`/docs — own notes go in `docs/`.
- **`origin` is PUBLIC**: never commit keys or bot state. `.gitignore` already covers `chipper/apiConfig.json`, `botConfig.json`, `jdocs/*.json`, `session-certs/`, `*.key`, `source.sh` — `git check-ignore -v <path>` before adding anything config-like.
- **Layout**: Go server in `chipper/` (module `github.com/kercre123/wire-pod/chipper`, entrypoints `chipper/cmd/{vosk,coqui,leopard,experimental}`, code in `chipper/pkg/`, web UI in `chipper/webroot/`); `vector-cloud/` is a separate DDL Go module. Setup/run: `sudo ./setup.sh`, `sudo ./chipper/start.sh` (root required).
- **License**: MIT — keep `LICENSE` and the original copyright notice.
