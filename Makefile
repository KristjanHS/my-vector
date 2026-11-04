.PHONY: help start daemon-enable daemon-disable check robot-test ship
# Run from anywhere: make -C ~/projects/my-vector <target>

help: ## List available targets (default)
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
	@echo "  syncwt|commit|commitwt  shared worktree targets (~/.config/make/worktree.mk, from dotfiles)"

start: ## Restart wire-pod.service once daemon-enable ran, else run the server in the foreground (NOPASSWD sudoers rules)
	@if systemctl is-enabled -q wire-pod 2>/dev/null; then \
	  sudo -n systemctl restart wire-pod || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }; \
	  echo "wire-pod restarted; logs: journalctl -u wire-pod -f"; \
	else sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }; fi

daemon-enable: ## Install + start wire-pod.service (starts with WSL, restarts on crash); asks the sudo password
	@systemctl is-active -q wire-pod || ! pgrep -f '[/]chipper/start.sh' >/dev/null || { echo "a foreground wire-pod is running: stop it first"; exit 1; }
	sed 's|@REPO@|$(CURDIR)|g' $(CURDIR)/scripts/wire-pod.service | sudo tee /etc/systemd/system/wire-pod.service >/dev/null
	sudo systemctl daemon-reload
	sudo systemctl enable --now wire-pod
	@echo "logs: journalctl -u wire-pod -f"

daemon-disable: ## Stop + remove wire-pod.service (make start runs in the foreground again)
	sudo systemctl disable --now wire-pod
	sudo rm -f /etc/systemd/system/wire-pod.service
	sudo systemctl daemon-reload

check: ## Fast checks (syncwt gate): upstream drift, robot/scripts shell syntax, go vet sdkapp, chipper myvector Go tests
	$(CURDIR)/scripts/check.sh

robot-test: ## On-robot smoke test (~45 s, needs wire-pod + the docked robot); exit 0 PASS, 1 FAIL, 2 setup
	$(CURDIR)/robot/firmware/smoke.sh

ship: ## Ship committed HEAD: check, patch, build (live), deploy the robot firmware, robot smoke test (fail = no push), git push; NO_DEPLOY=1 stops after the build; log in robot/work/ship-logs/
	$(CURDIR)/robot/firmware/ship.sh $(if $(NO_DEPLOY),--no-deploy)

# Shared worktree/commit targets (syncwt, commit, commitwt) from dotfiles. At the bottom so
# the include can't steal the default goal; `-include` so a clone without dotfiles still runs.
-include $(HOME)/.config/make/worktree.mk
