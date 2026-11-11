package webauth

import (
	"sync"
	"time"
)

const (
	LoginFailMax    = 5 // per source IP per LoginWindow
	LoginWindow     = 15 * time.Minute
	LoginFailGlobal = 30 // all IPs per LoginWindow
	MaxLoginBody    = 4 << 10
	LoginConcurrent = 4 // parallel bcrypt checks; more → 429
)

type limiter struct {
	mu     sync.Mutex
	perIP  map[string][]time.Time
	global []time.Time
}

func newLimiter() *limiter { return &limiter{perIP: map[string][]time.Time{}} }

func recent(ts []time.Time, now time.Time) []time.Time {
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= LoginWindow {
		i++
	}
	return ts[i:]
}

// try reserves a login attempt for ip, counted as a failure until forgive; >0 = refused, wait that long.
// Check and record share one lock, so parallel posts cannot all pass before any failure lands.
func (l *limiter) try(ip string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.global = recent(l.global, now)
	ts := recent(l.perIP[ip], now)
	if len(ts) == 0 {
		delete(l.perIP, ip)
	} else {
		l.perIP[ip] = ts
	}
	var d time.Duration
	if len(ts) >= LoginFailMax {
		d = LoginWindow - now.Sub(ts[len(ts)-LoginFailMax])
	}
	if len(l.global) >= LoginFailGlobal {
		if g := LoginWindow - now.Sub(l.global[len(l.global)-LoginFailGlobal]); g > d {
			d = g
		}
	}
	if d > 0 {
		return d
	}
	l.perIP[ip] = append(l.perIP[ip], now)
	l.global = append(l.global, now)
	if len(l.perIP) > 4096 { // spray of IPs: keep the map bounded
		for k, ts := range l.perIP {
			if len(recent(ts, now)) == 0 {
				delete(l.perIP, k)
			}
		}
	}
	return 0
}

// forgive drops the attempt try(ip, now) reserved (a successful login).
func (l *limiter) forgive(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.perIP[ip] = dropLast(l.perIP[ip], now)
	l.global = dropLast(l.global, now)
}

func dropLast(ts []time.Time, t time.Time) []time.Time {
	for i := len(ts) - 1; i >= 0; i-- {
		if ts[i].Equal(t) {
			return append(ts[:i], ts[i+1:]...)
		}
	}
	return ts
}
