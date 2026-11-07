.PHONY: help start server-pin daemon-enable daemon-disable tts-setup tts-enable tts-disable stt-setup stt-enable stt-disable check robot-test ship
# Run from anywhere: make -C ~/projects/my-vector <target>
# robot/work of the main checkout (venvs + models live there, also when make runs in a worktree)
WORK := $(shell dirname "$$(git -C $(CURDIR) rev-parse --path-format=absolute --git-common-dir)")/robot/work
HF_HOME ?= $(HOME)/.cache/huggingface
HF_HUB_CACHE ?= $(HF_HOME)/hub

help: ## List available targets (default)
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
	@echo "  syncwt|commit|commitwt  shared worktree targets (~/.config/make/worktree.mk, from dotfiles)"

start: ## Restart wire-pod.service (runs the last shipped commit, robot/work/server-sha) once daemon-enable ran, else go run the working tree in the foreground (NOPASSWD sudoers rules)
	@if systemctl is-enabled -q wire-pod 2>/dev/null; then $(CURDIR)/scripts/restart-service.sh; \
	else sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }; fi

server-pin: ## Pin wire-pod to the committed HEAD and restart it (run a committed server fix without a full ship)
	$(CURDIR)/scripts/pin-server.sh HEAD
	$(CURDIR)/scripts/restart-service.sh

daemon-enable: ## Install + (re)start wire-pod.service (builds + runs the pinned commit; starts with WSL, restarts on crash); asks the sudo password
	@systemctl is-active -q wire-pod || [ -z "$$(ss -Hltn 'sport = :443')" ] || { echo "something already listens on :443 (a foreground wire-pod?): stop it first"; exit 1; }
	@[ -s $(CURDIR)/robot/work/server-sha ] || $(CURDIR)/scripts/pin-server.sh HEAD
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

stt-setup: ## Create/refresh the STT venv + TalTech turbo-et and large-v3-turbo models in robot/work/et-stt (no sudo; downloads ~6 GB on first run)
	$(CURDIR)/et-stt/setup.sh

stt-enable: ## Install + (re)start myvector-stt.service (127.0.0.1:8092, all speech; one model on the GPU, per lang); asks the sudo password
	@[ -f $(WORK)/et-stt/models/taltech-et/ct2/model.bin ] && [ -f $(WORK)/et-stt/models/turbo-stock/model.bin ] || { echo "no STT model: run make stt-setup first"; exit 1; }
	sed -e 's|@REPO@|$(CURDIR)|g' -e 's|@WORK@|$(WORK)|g' -e 's|@USER@|$(USER)|g' $(CURDIR)/scripts/myvector-stt.service | sudo tee /etc/systemd/system/myvector-stt.service >/dev/null
	sudo systemctl daemon-reload
	sudo systemctl enable myvector-stt
	sudo systemctl restart myvector-stt
	@echo "logs: journalctl -u myvector-stt -f   live check: et-stt/smoke.sh"

stt-disable: ## Stop + remove myvector-stt.service
	sudo systemctl disable --now myvector-stt
	sudo rm -f /etc/systemd/system/myvector-stt.service
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
