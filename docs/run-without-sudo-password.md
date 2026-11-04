# `make start` without a password

One-time install (lets your user run only `chipper/start.sh` and `systemctl restart wire-pod` as root, without a password):

```
printf '%s\n' "$USER ALL=(root) NOPASSWD: $HOME/projects/my-vector/chipper/start.sh" "$USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart wire-pod" | sudo tee /etc/sudoers.d/my-vector-chipper >/dev/null && sudo chmod 440 /etc/sudoers.d/my-vector-chipper && sudo visudo -cf /etc/sudoers.d/my-vector-chipper
```

Check: `sudo -n -l | grep -cF -e 'NOPASSWD: /home' -e 'NOPASSWD: /usr/bin/systemctl restart wire-pod'` prints `2`. Remove: `sudo rm /etc/sudoers.d/my-vector-chipper`.

## Start with Windows

`make daemon-enable` (once, asks the sudo password); then in an **admin** Windows PowerShell register the logon keep-alive (`conhost --headless` is untested — drop it if the task won't start):

```
Register-ScheduledTask -Force -TaskName 'wire-pod keep-alive' -Trigger (New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME) -Action (New-ScheduledTaskAction -Execute conhost.exe -Argument '--headless C:\Windows\System32\wsl.exe -d Ubuntu-22.04 --exec sleep infinity') -Settings (New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries)
```

The Windows shortcut then only restarts the service; this task alone keeps WSL alive. Check after a reboot + logon: `systemctl is-active wire-pod` prints `active`. Remove: `Unregister-ScheduledTask -TaskName 'wire-pod keep-alive' -Confirm:$false` + `make daemon-disable`.
