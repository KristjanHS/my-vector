package robotconn

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// setUserTimeout is a net.Dialer Control: TCP_USER_TIMEOUT (ms) on the socket before connect.
// x/sys/unix, not syscall: syscall lacks TCP_USER_TIMEOUT on linux/amd64, 386 and arm.
func setUserTimeout(_, _ string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(userTimeout.Milliseconds()))
	}); err != nil {
		return err
	}
	return serr
}
