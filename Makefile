.PHONY: help start
# Run from anywhere: make -C ~/projects/my-vector <target>

help: ## List available targets (default)
	@grep -E '^[a-zA-Z_-]+:.*## ' $(firstword $(MAKEFILE_LIST)) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

start: ## Run the wire-pod server (needs the NOPASSWD sudoers rule)
	sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }
