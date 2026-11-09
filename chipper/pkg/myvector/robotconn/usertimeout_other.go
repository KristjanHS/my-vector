//go:build !linux
// +build !linux

package robotconn

import "syscall"

// setUserTimeout is a no-op off Linux (upstream also builds for Windows/macOS); keepalive still applies.
func setUserTimeout(_, _ string, _ syscall.RawConn) error { return nil }
