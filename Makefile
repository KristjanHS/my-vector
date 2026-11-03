.PHONY: start
# Run from anywhere: make -C ~/projects/my-vector start
start:
	sudo -n $(CURDIR)/chipper/start.sh || { echo "no sudoers rule? see docs/run-without-sudo-password.md"; exit 1; }
