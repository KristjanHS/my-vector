.PHONY: help start daemon-enable daemon-disable tts-setup tts-enable tts-disable check robot-test ship
# Run from anywhere: make -C ~/projects/my-vector <target>
# robot/work of the main checkout (venvs + models live there, also when make runs in a worktree)
WORK := $(shell dirname "$$(git -C $(CURDIR) rev-parse --path-format=absolute --git-common-dir)")/robot/work
HF_HOME ?= $(HOME)/.cache/huggingface
HF_HUB_CACHE ?= $(HF_HOME)/hub

help: ## List available targets (default)
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
	@echo "  syncwt|commit|commitwt  shared worktree targets (~/.config/make/worktree.mk, from dotfiles)"

start: ## Restart wire-pod.service once daemon-enable ran, else run the server in the foreground (NOPASSWD sudoers rules)
	@if systemctl is-enabled -q wire-pod 2>/dev/null; then $(CURDIR)/scripts/restart-service.sh; \
	else sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }; fi

daemon-enable: ## Install + (re)start wire-pod.service (starts with WSL, restarts on crash); asks the sudo password
	@systemctl is-active -q wire-pod || [ -z "$$(ss -Hltn 'sport = :443')" ] || { echo "something already listens on :443 (a foreground wire-pod?): stop it first"; exit 1; }
	sed 's|@REPO@|$(CURDIR)|g' $(CURDIR)/scripts/wire-pod.service | sudo tee /etc/systemd/system/wire-pod.service >/dev/null
	sudo systemctl daemon-reload
	sudo systemctl enable wire-pod
	sudo systemctl restart wire-pod
	@echo "logs: journalctl -u wire-pod -f"

daemon-disable: ## Stop + remove wire-pod.service (make start runs in the foreground again)
	sudo systemctl disable --now wire-pod
	sudo rm -f /etc/systemd/system/wire-pod.service
	systemctl is-active -q myvector-tts || sudo nft delete table inet wirepod_guard 2>/dev/null || true # the TTS port keeps its guard
	sudo systemctl daemon-reload

tts-setup: ## Create/refresh the TTS venv in robot/work + voice models (Kokoro into your HF cache; no sudo; downloads ~3 GB on first run)
	$(CURDIR)/tts/setup.sh

tts-enable: ## Install + (re)start myvector-tts.service (:8091, runs as you, starts with WSL); asks the sudo password
	@[ -x $(WORK)/tts/venv/bin/python ] || { echo "no TTS venv: run make tts-setup first"; exit 1; }
	@[ -d $(HF_HUB_CACHE)/models--hexgrad--Kokoro-82M ] || { echo "no Kokoro in HF_HUB_CACHE=$(HF_HUB_CACHE) (the unit runs offline): export HF_HOME/HF_HUB_CACHE as make tts-setup saw them"; exit 1; }
	sed -e 's|@REPO@|$(CURDIR)|g' -e 's|@WORK@|$(WORK)|g' -e 's|@USER@|$(USER)|g' -e 's|@HF_HOME@|$(HF_HOME)|g' -e 's|@HF_HUB_CACHE@|$(HF_HUB_CACHE)|g' $(CURDIR)/scripts/myvector-tts.service | sudo tee /etc/systemd/system/myvector-tts.service >/dev/null
	sudo systemctl daemon-reload
	sudo systemctl enable myvector-tts
	sudo systemctl restart myvector-tts
	@echo "logs: journalctl -u myvector-tts -f   live check (after ~30 s warm-up): tts/smoke.sh"

tts-disable: ## Stop + remove myvector-tts.service
	sudo systemctl disable --now myvector-tts
	sudo rm -f /etc/systemd/system/myvector-tts.service
	systemctl is-active -q wire-pod || sudo nft delete table inet wirepod_guard 2>/dev/null || true # wire-pod keeps its guard
	sudo systemctl daemon-reload

check: ## Fast checks (syncwt gate): upstream drift, robot/scripts shell syntax, go vet sdkapp, chipper myvector + vars fork Go tests
	$(CURDIR)/scripts/check.sh

robot-test: ## On-robot smoke test (~45 s, needs wire-pod + the docked robot); exit 0 PASS, 1 FAIL, 2 setup
	$(CURDIR)/robot/firmware/smoke.sh

ship: ## Ship committed HEAD: check, patch, build (live), deploy the robot firmware, robot smoke test (fail = no push), git push; NO_DEPLOY=1 stops after the build; log in robot/work/ship-logs/
	$(CURDIR)/robot/firmware/ship.sh $(if $(NO_DEPLOY),--no-deploy)

# Shared worktree/commit targets (syncwt, commit, commitwt) from dotfiles. At the bottom so
# the include can't steal the default goal; `-include` so a clone without dotfiles still runs.
-include $(HOME)/.config/make/worktree.mk
