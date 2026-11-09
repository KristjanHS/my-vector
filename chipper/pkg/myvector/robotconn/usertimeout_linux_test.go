package robotconn

import (
	"context"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDialTCPSetsUserTimeout(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	c, err := dialTCP(context.Background(), lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rc, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var gerr error
	if err := rc.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
	}); err != nil {
		t.Fatal(err)
	}
	if gerr != nil {
		t.Fatal(gerr)
	}
	if want := int(userTimeout.Milliseconds()); got != want {
		t.Fatalf("TCP_USER_TIMEOUT = %d ms, want %d", got, want)
	}
}
