.PHONY: help start
# Run from anywhere: make -C ~/projects/my-vector <target>

help: ## List available targets (default)
	@grep -E '^[a-zA-Z0-9_.-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'
	@echo "  syncwt|commit|commitwt  shared worktree targets (~/.config/make/worktree.mk, from dotfiles)"

start: ## Run the wire-pod server (needs the NOPASSWD sudoers rule)
	sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }

# Shared worktree/commit targets (syncwt, commit, commitwt) from dotfiles. At the bottom so
# the include can't steal the default goal; `-include` so a clone without dotfiles still runs.
-include $(HOME)/.config/make/worktree.mk
