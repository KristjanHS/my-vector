# `make start` without a password

One-time install (it lets your user run only `chipper/start.sh` as root, without a password):

```
echo "$USER ALL=(root) NOPASSWD: $HOME/projects/my-vector/chipper/start.sh" | sudo tee /etc/sudoers.d/my-vector-chipper >/dev/null && sudo chmod 440 /etc/sudoers.d/my-vector-chipper && sudo visudo -cf /etc/sudoers.d/my-vector-chipper
```

Check: `sudo -n -l $HOME/projects/my-vector/chipper/start.sh` prints the path. Remove: `sudo rm /etc/sudoers.d/my-vector-chipper`.
