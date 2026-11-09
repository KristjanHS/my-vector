package robotconn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// counted swaps dial for one that counts and resets the pool; restored on cleanup.
func counted(t *testing.T, extra ...grpc.DialOption) *atomic.Int32 {
	var n atomic.Int32
	mu.Lock()
	conns = map[string]*entry{}
	mu.Unlock()
	prod := dial
	dial = func(target, guid string) (*grpc.ClientConn, error) {
		n.Add(1)
		return dialRobot(target, guid, extra...)
	}
	t.Cleanup(func() {
		dial = prod
		mu.Lock()
		for _, e := range conns {
			e.cc.Close()
		}
		conns = map[string]*entry{}
		mu.Unlock()
	})
	return &n
}

func TestGetReuses(t *testing.T) {
	n := counted(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Get("esn1", "guid", "127.0.0.1:1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	v, err := Get("esn1", "guid", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
	if v.Cfg.SerialNo != "esn1" || v.Cfg.Target != "127.0.0.1:1" || v.Cfg.Token != "guid" {
		t.Fatalf("Cfg = %+v", v.Cfg)
	}
}

func TestGetRedialsOnChange(t *testing.T) {
	n := counted(t)
	if _, err := Get("esn1", "guid", "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	old := conns["esn1"].cc
	if _, err := Get("esn1", "guid", "127.0.0.1:2"); err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("dials = %d, want 2", got)
	}
	if s := old.GetState(); s != connectivity.Shutdown {
		t.Fatalf("old conn state = %v, want Shutdown", s)
	}
}

// robot is a TLS gRPC server with no services: a reachable robot answers Unimplemented.
func robot(t *testing.T, addr string) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})))
	go srv.Serve(lis)
	return lis.Addr().String(), srv.Stop
}

func selfSigned(t *testing.T) tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func call(t *testing.T, target string) codes.Code {
	v, err := Get("esn1", "guid", target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = v.Conn.BatteryState(ctx, &vectorpb.BatteryStateRequest{})
	return status.Code(err)
}

func TestGetAfterRobotRestart(t *testing.T) {
	counted(t, grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.Config{BaseDelay: 10 * time.Second, Multiplier: 1, MaxDelay: 10 * time.Second}}))
	addr, stop := robot(t, "127.0.0.1:0")
	if c := call(t, addr); c != codes.Unimplemented {
		t.Fatalf("up: %v, want Unimplemented", c)
	}
	stop()
	for i := 0; i < 5; i++ {
		if c := call(t, addr); c != codes.Unavailable {
			t.Fatalf("down call %d: %v, want Unavailable", i, c)
		}
		time.Sleep(300 * time.Millisecond)
	}
	_, stop = robot(t, addr)
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c := call(t, addr)
		if c == codes.Unimplemented {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("back up: %v after 2 s, want Unimplemented", c)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
