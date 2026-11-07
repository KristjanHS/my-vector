// Package wifi is the fork's WiFi page: the robot's saved networks, kept by connman on the robot.
// connman joins the strongest saved network in range at boot, so two saved networks let the robot move between floors.
// Every robot step is robot/wifi/wifi.sh over SSH. Passwords are write-only: the page sends one, wire-pod hands it to
// wifi.sh on stdin and keeps no copy (no file, no log, no reply); on the robot it is in connman's root-only store.
package wifi

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

const (
	Page = "/myvector/wifi"
	API  = "/myvector/api/wifi"
	// ExitConnected is wifi.sh's exit for "the robot is on that network now": it is left as is.
	ExitConnected = 3
)

// Timeout bounds one wifi.sh run (list scans ~4 s; its SSH ConnectTimeout is 8 s).
var Timeout = 20 * time.Second

// Run runs wifi.sh args with stdin and returns its stdout, stderr and exit code (-1: it did not run); tests replace it.
var Run = run

var changing sync.Mutex

//go:embed wifi.html
var page []byte

// Network is one SSID the robot has saved and/or sees.
type Network struct {
	SSID      string `json:"ssid"`
	Hex       string `json:"hex"`
	Saved     bool   `json:"saved"`
	Seen      bool   `json:"seen"`
	Connected bool   `json:"connected"`
}

type reply struct {
	Networks []Network `json:"networks,omitempty"`
	Message  string    `json:"message,omitempty"`
}

// request is a POST: {"action":"add","ssid","password"} or {"action":"forget","hex"}.
type request struct {
	Action   string `json:"action"`
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	Hex      string `json:"hex"`
}

var (
	hexSSID = regexp.MustCompile(`^([0-9a-f]{2}){1,32}$`)
	psk     = regexp.MustCompile(`^([\x20-\x7e]{8,63}|[0-9A-Fa-f]{64})$`)
	line    = regexp.MustCompile(`^(saved|seen|connected) (([0-9a-f]{2}){1,32})$`)
)

func (q *request) valid() error {
	switch q.Action {
	case "add":
		if n := len(q.SSID); n < 1 || n > 32 {
			return errors.New("ssid: 1 to 32 bytes")
		}
		if !utf8.ValidString(q.SSID) || strings.IndexFunc(q.SSID, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return errors.New("ssid: no control characters")
		}
		if !psk.MatchString(q.Password) {
			return errors.New("password: 8 to 63 printable ASCII characters (WPA/WPA2)")
		}
	case "forget":
		if !hexSSID.MatchString(q.Hex) {
			return errors.New("hex: the SSID as lowercase hex")
		}
	default:
		return errors.New(`action: "add" or "forget"`)
	}
	return nil
}

// Parse reads wifi.sh list output into networks: connected first, then saved, then by name.
func Parse(out string) []Network {
	by := map[string]*Network{}
	for _, l := range strings.Split(out, "\n") {
		m := line.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		n := by[m[2]]
		if n == nil {
			b, _ := hex.DecodeString(m[2])
			n = &Network{SSID: string(b), Hex: m[2]}
			by[m[2]] = n
		}
		switch m[1] {
		case "saved":
			n.Saved = true
		case "seen":
			n.Seen = true
		case "connected":
			n.Connected = true
		}
	}
	list := make([]Network, 0, len(by))
	for _, n := range by {
		list = append(list, *n)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Connected != b.Connected {
			return a.Connected
		}
		if a.Saved != b.Saved {
			return a.Saved
		}
		return a.SSID < b.SSID
	})
	return list
}

// Handler serves API: GET lists the networks; POST (application/json request) adds or forgets one.
// A refusal (the robot is on that network) is a 409; a wifi.sh or SSH failure is a 502 with its last line.
func Handler(w http.ResponseWriter, r *http.Request) {
	var q request
	args, stdin := []string{"list"}, ""
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		if !decode(w, r, &q) {
			return
		}
		if q.Action == "add" {
			args, stdin = []string{"add"}, q.SSID+"\n"+q.Password+"\n"
		} else {
			args = []string{"forget", q.Hex}
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost { // one change at a time: two would race on the stage dir
		changing.Lock()
		defer changing.Unlock()
	}
	out, errOut, code := Run(r.Context(), stdin, args...)
	if code != 0 {
		msg := lastLine(errOut)
		if msg == "" {
			msg = fmt.Sprintf("wifi.sh exit %d", code)
		}
		logger.Println("myvector wifi " + args[0] + ": " + msg) // never the password: it is not in args or stderr
		status := http.StatusBadGateway
		if code == ExitConnected {
			status = http.StatusConflict
		}
		http.Error(w, msg, status)
		return
	}
	rep := reply{Message: lastLine(out)}
	if r.Method == http.MethodGet {
		rep = reply{Networks: Parse(out)}
	} else {
		logger.Println("myvector wifi " + strings.Join(args, " ") + ": " + rep.Message)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(rep)
}

// decode reads an application/json POST body into q and validates it; on failure it has written the error.
// The JSON-only body makes a cross-site form post fail its CORS preflight.
func decode(w http.ResponseWriter, r *http.Request, q *request) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	err := dec.Decode(q)
	if err == nil {
		err = q.valid()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l[len(l)-1])
}

func run(ctx context.Context, stdin string, args ...string) (string, string, int) {
	script, err := conf.FindUp(filepath.Join("robot", "wifi", "wifi.sh"))
	if err != nil {
		return "", err.Error(), -1
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.WaitDelay = time.Second // an orphaned ssh holding the output pipe must not outlive the timeout
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Sprintf("wifi.sh: no answer in %v", Timeout), -1
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), errOut.String(), exit.ExitCode()
	}
	if err != nil {
		return out.String(), err.Error(), -1
	}
	return out.String(), errOut.String(), 0
}

// PageHandler serves the page at Page.
func PageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(page)
}
