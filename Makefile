.PHONY: help start check robot-test ship
# Run from anywhere: make -C ~/projects/my-vector <target>

help: ## List available targets (default)
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'
	@echo "  syncwt|commit|commitwt  shared worktree targets (~/.config/make/worktree.mk, from dotfiles)"

start: ## Run the wire-pod server (needs the NOPASSWD sudoers rule)
	sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }

check: ## Fast checks (syncwt gate): upstream drift, robot/scripts shell syntax, go vet sdkapp, chipper myvector Go tests
	$(CURDIR)/scripts/check.sh

robot-test: ## On-robot smoke test (~45 s, needs wire-pod + the docked robot); exit 0 PASS, 1 FAIL, 2 setup
	$(CURDIR)/robot/firmware/smoke.sh

ship: ## Ship committed HEAD: check, patch, build (live), deploy the robot firmware, robot smoke test (fail = no push), git push; NO_DEPLOY=1 stops after the build; log in robot/work/ship-logs/
	$(CURDIR)/robot/firmware/ship.sh $(if $(NO_DEPLOY),--no-deploy)

# Shared worktree/commit targets (syncwt, commit, commitwt) from dotfiles. At the bottom so
# the include can't steal the default goal; `-include` so a clone without dotfiles still runs.
-include $(HOME)/.config/make/worktree.mk
