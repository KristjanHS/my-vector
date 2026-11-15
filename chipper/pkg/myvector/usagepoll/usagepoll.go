// Package usagepoll is the prod-evidence robot poll (claudeconf plan 2026-11-15-my-vector-prod-evidence.md, Stage 2.2):
// hourly, wire-pod reads a read-only snapshot of the robot over the robot/env.sh ssh lane (boot_id, clock, uptime,
// tailscale0 up, each vic-* unit's ActiveEnterTimestampMonotonic + NRestarts), diffs it against the previous one and
// adds the unplanned restarts (those outside every ship window) and the day's tailnet state to the usage ledger.
//
// Decisions:
//   - First-ever poll, or a lost/unreadable state file: the snapshot is the baseline, 0 restarts.
//   - boot_id changed: 1 reboot at the boot time (robot now - uptime); the new boot's unit starts are part of it, but a
//     unit with NRestarts > 0 in the new boot adds NRestarts at its ActiveEnter time.
//   - Same boot: a unit whose ActiveEnter moved or whose NRestarts grew is max(1, ΔNRestarts) restarts at its
//     ActiveEnter time (a manual start resets NRestarts, so a negative Δ still counts 1).
//   - A unit that appeared counts only its NRestarts (0 = a newly started/listed unit, no count); one that disappeared
//     counts nothing (a stop is not a restart; if it comes back it appears). A unit that crashed and stays failed is
//     not a restart either: its ActiveEnter does not move.
//   - An event at a time in [start-Slack, end+Slack] (inclusive) of any ship window is exempt. Days are the local date
//     of the EVENT, so a poll after a gap books a restart on the day it happened.
//   - A ship in progress (ShipActive file < ActiveMaxAge old): the poll writes nothing at all, neither tailnet nor
//     restarts nor snapshot, so a mid-deploy probe (tailscale0 not up yet) cannot mark the day down, and the next poll
//     diffs from the older snapshot once the window line exists. An older marker (a ship killed before its EXIT trap)
//     is ignored.
//   - The probe goes over env.sh's $ip (the robot's tailnet address). If it fails, it is retried once over
//     $ROBOT_LAN_IP (the env.sh settings layers or robot.env; unset, or equal to $ip, = no retry): a LAN answer is used
//     with tailnet forced down, so the tailnet column means "the PC-robot tailnet link and the robot's tailscale0 both
//     up". Both failing (or the answer not parsing) writes nothing.
//   - A successful poll sets today's tailnet: down is sticky for the day, up otherwise. The snapshot is saved only after
//     every ledger update succeeded.
//
// Known limits:
//   - All restarts of a unit since the last poll are booked at its latest ActiveEnter time, so a crash followed by an
//     exempt ship restart in the same poll interval is lost (exempt with the ship).
//
// Nothing identifying is stored: the state holds boot_id, robot clock/uptime and vic-* unit names + counters.
package usagepoll

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/usage"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

var (
	// First is the delay before the first poll after wire-pod starts; Every the interval after that.
	First = time.Minute
	Every = time.Hour
	// Slack widens each ship window on both sides.
	Slack = 120 * time.Second
	// ActiveMaxAge is how long a ship-active marker holds the diff off; an older one is a ship that died untrapped.
	ActiveMaxAge = 3 * time.Hour
	// Timeout bounds one ssh probe (ssh's own ConnectTimeout is 8 s, from robot/env.sh).
	Timeout = 30 * time.Second
)

// Script runs on the robot: read-only (proc files, date, ip link show, systemctl list-units/show), no sound.
// `end` marks a complete answer; systemctl show prints Key=Value so the property order does not matter.
const Script = `read b </proc/sys/kernel/random/boot_id; echo "boot $b"
echo "now $(date +%s)"
read u _ </proc/uptime; echo "uptime $u"
if ip link show tailscale0 2>/dev/null | grep -q "[<,]UP[,>]"; then echo "tailnet 1"; else echo "tailnet 0"; fi
for s in $(systemctl list-units --no-legend --plain "vic-*" | awk '{print $1}'); do
  echo "unit $s" $(systemctl show -p ActiveEnterTimestampMonotonic -p NRestarts "$s")
done
echo end`

// Unit is one vic-* unit's counters: EnterUS = ActiveEnterTimestampMonotonic (µs since boot).
type Unit struct {
	EnterUS   int64 `json:"enter_us"`
	NRestarts int   `json:"n_restarts"`
}

// Snapshot is one probe answer. Now is the robot's epoch (s), UptimeUS its uptime (µs).
type Snapshot struct {
	BootID   string          `json:"boot_id"`
	Now      int64           `json:"now"`
	UptimeUS int64           `json:"uptime_us"`
	Tailnet  bool            `json:"-"`
	Units    map[string]Unit `json:"units"`
}

// BootUS is the robot's boot time, epoch µs.
func (s Snapshot) BootUS() int64 { return s.Now*1e6 - s.UptimeUS }

var (
	bootRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	unitRe = regexp.MustCompile(`^vic-[A-Za-z0-9@._:-]+$`)
	upRe   = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]{1,6}))?$`)
)

// Parse reads Script's output. It needs boot, now, uptime, tailnet, at least one vic-* unit with an
// ActiveEnterTimestampMonotonic, and the end line; other lines (env.sh chatter) are ignored.
func Parse(out string) (Snapshot, error) {
	s := Snapshot{Units: map[string]Unit{}}
	var seen = map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		var err error
		switch f[0] {
		case "boot":
			if len(f) != 2 || !bootRe.MatchString(f[1]) {
				return s, fmt.Errorf("probe: bad boot_id line %q", sc.Text())
			}
			s.BootID = f[1]
		case "now":
			if len(f) != 2 {
				return s, fmt.Errorf("probe: bad now line %q", sc.Text())
			}
			if s.Now, err = strconv.ParseInt(f[1], 10, 64); err != nil || s.Now <= 0 {
				return s, fmt.Errorf("probe: bad now %q", f[1])
			}
		case "uptime":
			m := []string(nil)
			if len(f) == 2 {
				m = upRe.FindStringSubmatch(f[1])
			}
			if m == nil {
				return s, fmt.Errorf("probe: bad uptime line %q", sc.Text())
			}
			sec, _ := strconv.ParseInt(m[1], 10, 64)
			frac, _ := strconv.ParseInt((m[2] + "000000")[:6], 10, 64)
			s.UptimeUS = sec*1e6 + frac
		case "tailnet":
			if len(f) != 2 || (f[1] != "0" && f[1] != "1") {
				return s, fmt.Errorf("probe: bad tailnet line %q", sc.Text())
			}
			s.Tailnet = f[1] == "1"
		case "unit":
			if len(f) < 2 || !unitRe.MatchString(f[1]) {
				return s, fmt.Errorf("probe: bad unit line %q", sc.Text())
			}
			u, enter := Unit{}, false
			for _, kv := range f[2:] {
				k, v, _ := strings.Cut(kv, "=")
				switch k {
				case "ActiveEnterTimestampMonotonic":
					u.EnterUS, err = strconv.ParseInt(v, 10, 64)
					enter = true
				case "NRestarts": // absent on an old systemd: 0
					u.NRestarts, err = strconv.Atoi(v)
				}
				if err != nil {
					return s, fmt.Errorf("probe: bad %s on %s", k, f[1])
				}
			}
			if !enter {
				return s, fmt.Errorf("probe: no ActiveEnterTimestampMonotonic for %s", f[1])
			}
			s.Units[f[1]] = u
		case "end":
			seen["end"] = true
			continue
		default:
			continue
		}
		seen[f[0]] = true
	}
	for _, k := range []string{"boot", "now", "uptime", "tailnet", "unit", "end"} {
		if !seen[k] {
			return s, fmt.Errorf("probe: incomplete answer, no %s line", k)
		}
	}
	return s, nil
}

// Diff is the unplanned restarts between prev and cur per local day (of the event, in loc), minus those in a ship
// window ([2]int64{start, end} epochs) widened by slack. A nil prev is the baseline: no restarts.
func Diff(prev *Snapshot, cur Snapshot, windows [][2]int64, slack time.Duration, loc *time.Location) map[string]int {
	if prev == nil {
		return nil
	}
	type event struct {
		us int64
		n  int
	}
	var evs []event
	boot := cur.BootUS()
	if prev.BootID != cur.BootID {
		evs = append(evs, event{boot, 1})
		for _, u := range cur.Units {
			if u.NRestarts > 0 {
				evs = append(evs, event{boot + u.EnterUS, u.NRestarts})
			}
		}
	} else {
		for name, u := range cur.Units {
			p, ok := prev.Units[name]
			switch {
			case !ok:
				if u.NRestarts > 0 {
					evs = append(evs, event{boot + u.EnterUS, u.NRestarts})
				}
			case u.EnterUS != p.EnterUS || u.NRestarts > p.NRestarts:
				n := u.NRestarts - p.NRestarts
				if n < 1 {
					n = 1
				}
				evs = append(evs, event{boot + u.EnterUS, n})
			}
		}
	}
	out := map[string]int{}
	sl := int64(slack / time.Second)
	for _, e := range evs {
		if exempt(e.us, windows, sl) {
			continue
		}
		out[usage.Day(time.UnixMicro(e.us).In(loc))] += e.n
	}
	return out
}

func exempt(us int64, windows [][2]int64, slack int64) bool {
	for _, w := range windows {
		if us >= (w[0]-slack)*1e6 && us <= (w[1]+slack)*1e6 {
			return true
		}
	}
	return false
}

// Poller is one robot poll's inputs; Default wires wire-pod's, tests inject Probe and Now.
type Poller struct {
	Ledger      *usage.Ledger
	StatePath   string
	WindowsPath string
	ActivePath  string
	Probe       func(context.Context) (string, error) // over the tailnet
	LANProbe    func(context.Context) (string, error) // the fallback when Probe fails; nil = none
	Now         func() time.Time                      // its Location is the ledger's local day
}

// Default is wire-pod's poller: the ledger and paths from vars (cwd chipper/), the probe over ssh.
func Default() *Poller {
	return &Poller{
		Ledger:      usage.Open(vars.UsageLedgerPath),
		StatePath:   vars.UsagePollStatePath,
		WindowsPath: vars.ShipWindowsPath,
		ActivePath:  vars.ShipActivePath,
		Probe:       SSHProbe,
		LANProbe:    SSHProbeLAN,
		Now:         time.Now,
	}
}

// Poll probes the robot (over the LAN only if the tailnet probe fails) and applies the result (package doc). An error means nothing was written, except when a
// ledger update failed part-way: then the snapshot is not saved, and the next poll re-applies the diff.
func (p *Poller) Poll(ctx context.Context) error {
	out, err := p.Probe(ctx)
	viaLAN := false
	if err != nil {
		if p.LANProbe == nil {
			return err
		}
		lanOut, lanErr := p.LANProbe(ctx)
		if lanErr != nil {
			return fmt.Errorf("%v; LAN retry: %v", err, lanErr)
		}
		out, viaLAN = lanOut, true
	}
	cur, err := Parse(out)
	if err != nil {
		return err
	}
	if viaLAN {
		cur.Tailnet = false // the robot answered, but not over the tailnet
	}
	now := p.Now()
	if p.shipActive(now) {
		return nil // nothing at all: the window line does not exist yet, and a mid-deploy tailnet state is not the day's
	}
	// Tailnet first: it is idempotent, so a retry after a failed restart update repeats it harmlessly.
	if err := p.Ledger.Update(usage.Day(now), func(r *usage.Row) {
		if !cur.Tailnet {
			r.Tailnet = usage.TailnetDown
		} else if r.Tailnet != usage.TailnetDown {
			r.Tailnet = usage.TailnetUp
		}
	}); err != nil {
		return err
	}
	days := Diff(p.loadState(), cur, p.windows(), Slack, now.Location())
	keys := make([]string, 0, len(days))
	for d, n := range days {
		if n > 0 {
			keys = append(keys, d)
		}
	}
	sort.Strings(keys)
	for _, d := range keys {
		n := days[d]
		if err := p.Ledger.Update(d, func(r *usage.Row) { r.UnplannedRestarts += n }); err != nil {
			return err
		}
	}
	return p.saveState(cur)
}

// shipActive: the marker exists and its start epoch (content, else mtime) is less than ActiveMaxAge before now.
func (p *Poller) shipActive(now time.Time) bool {
	b, err := os.ReadFile(p.ActivePath)
	if err != nil {
		return false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		fi, serr := os.Stat(p.ActivePath)
		if serr != nil {
			return false
		}
		start = fi.ModTime().Unix()
	}
	return now.Unix()-start < int64(ActiveMaxAge/time.Second)
}

// windows reads ship.sh's "start end" lines; a malformed or half-appended line is skipped.
func (p *Poller) windows() [][2]int64 {
	b, err := os.ReadFile(p.WindowsPath)
	if err != nil {
		return nil
	}
	var w [][2]int64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		s, err1 := strconv.ParseInt(f[0], 10, 64)
		e, err2 := strconv.ParseInt(f[1], 10, 64)
		if err1 != nil || err2 != nil || e < s {
			continue
		}
		w = append(w, [2]int64{s, e})
	}
	return w
}

// loadState is the saved snapshot; nil (baseline) when missing or unreadable.
func (p *Poller) loadState() *Snapshot {
	b, err := os.ReadFile(p.StatePath)
	if err != nil {
		return nil
	}
	var s Snapshot
	if json.Unmarshal(b, &s) != nil || !bootRe.MatchString(s.BootID) {
		return nil
	}
	return &s
}

func (p *Poller) saveState(s Snapshot) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.StatePath), ".usage-poll-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p.StatePath)
}

// Loop polls first after `first`, then every `every`, until ctx ends. A failed poll (or a panic in one) logs one line;
// it never stops the loop or wire-pod.
func (p *Poller) Loop(ctx context.Context, first, every time.Duration) {
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := p.safePoll(ctx); err != nil {
			logger.Println("myvector usagepoll: " + err.Error())
		}
		t.Reset(every)
	}
}

func (p *Poller) safePoll(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return p.Poll(ctx)
}

// probeCmd sources robot/env.sh ($1) and runs Script ($2) as root on the robot: over $ip, or with $3 = lan over
// $ROBOT_LAN_IP (exit 2 when it is unset or equal to $ip: no distinct LAN lane). silent=1 (unexported) skips env.sh's
// sound-lock guard: the probe makes no sound. ServerAlive ends a half-open link's ssh within ~10 s.
const probeCmd = `declare +x silent=1; . "$1" || exit
h=$ip
if [[ $3 == lan ]]; then
  h=${ROBOT_LAN_IP:-}
  [[ -n $h && $h != "$ip" ]] || { echo "no ROBOT_LAN_IP distinct from ROBOT_IP" >&2; exit 2; }
fi
ssh "${ssh_opts[@]}" -o ServerAliveInterval=5 -o ServerAliveCountMax=2 "root@$h" "$2"`

// SSHProbe runs Script on the robot over robot/env.sh's ssh lane (its key, options and mux) to $ip, from wire-pod's cwd.
func SSHProbe(ctx context.Context) (string, error) { return sshProbe(ctx, "tailnet") }

// SSHProbeLAN is SSHProbe over $ROBOT_LAN_IP.
func SSHProbeLAN(ctx context.Context) (string, error) { return sshProbe(ctx, "lan") }

func sshProbe(ctx context.Context, lane string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", probeCmd, "_", vars.RobotEnvPath, Script, lane)
	cmd.WaitDelay = time.Second // an orphaned ssh holding the output pipe must not outlive the timeout
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("robot probe (%s): no answer in %v", lane, Timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(errOut.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("robot probe (%s): exit %d: %s", lane, exit.ExitCode(), msg)
		}
		return "", fmt.Errorf("robot probe (%s): %v", lane, err)
	}
	return out.String(), nil
}
