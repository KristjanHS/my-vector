# `make start` without a password

One-time install (lets your user run only `chipper/start.sh` and `systemctl restart wire-pod` as root, without a password):

```
printf '%s\n' "$USER ALL=(root) NOPASSWD: $HOME/projects/my-vector/chipper/start.sh" "$USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart wire-pod" | sudo tee /etc/sudoers.d/my-vector-chipper >/dev/null && sudo chmod 440 /etc/sudoers.d/my-vector-chipper && sudo visudo -cf /etc/sudoers.d/my-vector-chipper
```

Check: `sudo -n -l $HOME/projects/my-vector/chipper/start.sh` and `sudo -n -l systemctl restart wire-pod` print the command. Remove: `sudo rm /etc/sudoers.d/my-vector-chipper`.

## Start with Windows

`make daemon-enable` (once, asks the sudo password); then from an **admin** Windows terminal keep WSL alive at logon — WSL stops the distro ~15 s after its last `wsl.exe` exits, service or not:

```
schtasks /Create /F /TN "wire-pod keep-alive" /SC ONLOGON /TR "conhost.exe --headless C:\Windows\System32\wsl.exe -d Ubuntu-22.04 --exec sleep infinity"
```

Check after a reboot + logon: `systemctl is-active wire-pod` prints `active`. Remove: `schtasks /Delete /TN "wire-pod keep-alive" /F` + `make daemon-disable`.
