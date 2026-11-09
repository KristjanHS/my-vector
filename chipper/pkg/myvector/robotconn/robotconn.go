// Package robotconn holds one gRPC ClientConn per robot, shared by every caller and never closed per call.
// A changed target or guid closes the old conn, so a caller still holding its Vector sees Canceled.
package robotconn

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// A conn shared for life must notice a robot that vanished without FIN/RST (blackout, WiFi drop):
// unacked writes fail after userTimeout instead of TCP's ~15 min retransmit, and an idle conn with an
// open stream is pinged every keepaliveTime. keepaliveTime stays >= 5 min, gRPC's server MinTime,
// so the robot never answers too_many_pings with GOAWAY.
const (
	userTimeout      = 10 * time.Second
	keepaliveTime    = 5 * time.Minute
	keepaliveTimeout = 20 * time.Second
)

type entry struct {
	cc           *grpc.ClientConn
	target, guid string
}

var (
	mu    sync.Mutex
	conns = map[string]*entry{}
	dial  = func(target, guid string) (*grpc.ClientConn, error) { return dialRobot(target, guid) }
)

// Get is a Vector for esn on the robot's shared conn; a changed target or guid closes the old conn and dials anew.
func Get(esn, guid, target string) (*vector.Vector, error) {
	if target == "" || guid == "" {
		return nil, errors.New("robotconn: target and guid required")
	}
	mu.Lock()
	defer mu.Unlock()
	e := conns[esn]
	if e != nil && (e.target != target || e.guid != guid) {
		e.cc.Close()
		e = nil
	}
	if e == nil {
		cc, err := dial(target, guid)
		if err != nil {
			delete(conns, esn)
			return nil, err
		}
		e = &entry{cc: cc, target: target, guid: guid}
		conns[esn] = e
	} else if e.cc.GetState() == connectivity.TransientFailure {
		e.cc.ResetConnectBackoff() // a restarted robot answers the next call, not after the backoff
	}
	v := &vector.Vector{Conn: vectorpb.NewExternalInterfaceClient(e.cc)}
	v.Cfg.SerialNo, v.Cfg.Target, v.Cfg.Token = esn, target, guid
	return v, nil
}

// dialRobot is the SDK's vector.New dial (TLS without verify, bearer guid per RPC) plus dead-peer detection.
func dialRobot(target, guid string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial(target, append([]grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})),
		grpc.WithPerRPCCredentials(token(guid)),
		grpc.WithContextDialer(dialTCP),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: keepaliveTime, Timeout: keepaliveTimeout})}, extra...)...)
}

// dialTCP dials with TCP_USER_TIMEOUT = userTimeout where the OS has it (setUserTimeout).
func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Control: setUserTimeout}
	return d.DialContext(ctx, "tcp", addr)
}

type token string

func (t token) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

func (token) RequireTransportSecurity() bool { return true }
