package wirepod_ttr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// ForkQuietAPI is quiet.sh's lock control: POST ?on=<minutes> | ?off=1.
const ForkQuietAPI = "/myvector/quiet"

func init() {
	http.HandleFunc(ForkQuietAPI, forkQuietHandler)
}

// forkQuiet runs ahead of every other route (ProcessTextAll): a whole-phrase "shut up" locks robot sound, naps the robot
// and sends intent_system_sleep. Anything else returns false and routes on.
func forkQuiet(req interface{}, voiceText, botSerial string) bool {
	phrase, ok := quiet.Match(voiceText)
	if !ok {
		return false
	}
	forkQuietLock(botSerial, phrase, time.Now())
	IntentPass(req, "intent_system_sleep", voiceText, map[string]string{}, false)
	return true
}

// forkQuietLock is a "shut up" match's lock + listen off + nap + volume (wake-word and knowledge-graph turns).
func forkQuietLock(esn, phrase string, now time.Time) {
	_ = forkQuietStart([]string{esn}, now.Add(quiet.LockFor), "voice:"+phrase)
}

// forkQuietStart locks until max(current, until), then per robot: listen off, async nap to the lock's end, volume; the error is the lock write's.
func forkQuietStart(esns []string, until time.Time, source string) error {
	err := quiet.Set(until, source)
	if err != nil {
		logger.Println("my-vector quiet: lock: " + err.Error())
	}
	napUntil := until
	if l, rerr := quiet.Read(); rerr == nil && l.Until.After(napUntil) {
		napUntil = l.Until
	}
	for _, esn := range esns {
		forkListening.Deactivate(esn, "quiet")
		ip := forkRobotIP(esn)
		forkNaps.Add(1)
		go func(esn string) { defer forkNaps.Done(); forkNap(ip, esn, napUntil) }(esn)
	}
	forkNaps.Add(1)
	go func() { defer forkNaps.Done(); forkQuietVolume(esns) }()
	return err
}

// forkNaps tracks in-flight naps and volume changes so tests can wait them out.
var forkNaps sync.WaitGroup

func forkRobotIP(esn string) string {
	for _, r := range vars.BotInfo.Robots {
		if r.Esn == esn {
			return r.IPAddress
		}
	}
	return ""
}

func forkRobotESNs() []string {
	var out []string
	for _, r := range vars.BotInfo.Robots {
		out = append(out, r.Esn)
	}
	return out
}

func forkNap(ip, esn string, until time.Time) {
	if ip == "" {
		logger.Println("my-vector quiet: nap skipped: no IP for " + esn)
		return
	}
	if sent, err := quiet.Nap(ip, until); errors.Is(err, quiet.ErrNapOff) {
		logger.Println("my-vector quiet: nap skipped: " + err.Error())
	} else if err != nil {
		logger.Println("my-vector quiet: nap: " + err.Error())
	} else if sent.IsZero() {
		logger.Println("my-vector quiet: nap off on " + esn)
	} else {
		logger.Println("my-vector quiet: nap until " + sent.Format(time.RFC3339) + " on " + esn)
	}
}

// Volume and restore-timer seam; tests swap fakes.
var (
	forkVolumeGet  = forkRobotVolume
	forkVolumeSet  = forkSetRobotVolume
	forkQuietNow   = time.Now
	forkQuietAfter = func(d time.Duration, f func()) (stop func() bool) { return time.AfterFunc(d, f).Stop }
)

// forkVolumeTimeout bounds each robot settings RPC.
var forkVolumeTimeout = 5 * time.Second

// ForkQuietStartDelay: the start-up re-arm waits for vars.Init to load the robots (initwirepod fork_quiet.go).
const ForkQuietStartDelay = 30 * time.Second

// forkQuietMu serialises the lock file's volume field, the restore and forkQuietStop.
var (
	forkQuietMu   sync.Mutex
	forkQuietStop func() bool
)

// forkQuietVolume saves esns[0]'s master_volume into the lock (unless one is saved), sets quiet.LockVolume and arms the restore.
func forkQuietVolume(esns []string) {
	forkQuietMu.Lock()
	defer forkQuietMu.Unlock()
	l, err := quiet.Read()
	if err != nil || l.Until.IsZero() {
		logger.Println("my-vector quiet: volume skipped: no readable lock")
		return
	}
	saved := l.Vol != quiet.NoVolume
	if !saved && len(esns) > 0 {
		if v, err := forkVolumeGet(esns[0]); err != nil {
			logger.Println("my-vector quiet: volume read: " + err.Error() + " (volume left as is: nothing to restore it to)")
		} else if err := quiet.SetVol(l.Until, v, l.Source); err != nil {
			logger.Println("my-vector quiet: volume save: " + err.Error() + " (volume left as is)")
		} else {
			saved = true
		}
	}
	for _, esn := range esns {
		if !saved {
			break
		}
		if err := forkVolumeSet(esn, quiet.LockVolume); err != nil {
			logger.Println("my-vector quiet: volume set: " + err.Error())
		}
	}
	forkQuietArm()
}

// forkQuietArm (forkQuietMu held) points the restore timer at the lock file's until; no lock stops it.
func forkQuietArm() {
	if forkQuietStop != nil {
		forkQuietStop()
		forkQuietStop = nil
	}
	l, err := quiet.Read()
	if err != nil || l.Until.IsZero() {
		return
	}
	d := l.Until.Sub(forkQuietNow())
	if d < 0 {
		d = 0
	}
	forkQuietStop = forkQuietAfter(d, forkQuietExpire)
}

// forkQuietExpire is the restore timer: an extended lock re-arms, an expired one restores the volume.
func forkQuietExpire() {
	forkQuietMu.Lock()
	defer forkQuietMu.Unlock()
	forkQuietStop = nil
	l, err := quiet.Read()
	if err != nil || l.Until.IsZero() {
		return
	}
	if forkQuietNow().Before(l.Until) {
		forkQuietArm()
		return
	}
	forkQuietRestore(l)
}

// forkQuietRetry: a failed restore retries after this; meanwhile an expired lock file keeps the volume (start-up re-arm restores it too).
var forkQuietRetry = 5 * time.Minute

// forkQuietRestore (forkQuietMu held) sets l.Vol on every paired robot, then removes the lock; on a failure (or no robot
// known) it keeps l.Vol in a lock file expired at min(l.Until, now) and arms forkQuietRetry. A file changed meanwhile is
// kept and re-armed on instead; false = it now holds a live lock (a "shut up" during the RPCs).
func forkQuietRestore(l quiet.Lock) bool {
	was := l
	if l.Vol == quiet.NoVolume {
		logger.Println("my-vector quiet: lock ended, no saved volume")
	} else if !forkQuietSetAll(l.Vol) {
		if now := forkQuietNow(); l.Until.After(now) {
			l.Until = now
		}
		if ok, err := quiet.HoldIf(was, l); err != nil {
			logger.Println("my-vector quiet: volume keep: " + err.Error())
		} else if !ok {
			return forkQuietSuperseded()
		}
		logger.Println("my-vector quiet: volume " + strconv.Itoa(l.Vol) + " kept for a retry in " + forkQuietRetry.String())
		forkQuietStop = forkQuietAfter(forkQuietRetry, forkQuietExpire)
		return true
	}
	if ok, err := quiet.ClearIf(was); err != nil {
		logger.Println("my-vector quiet: unlock: " + err.Error())
	} else if !ok {
		return forkQuietSuperseded()
	}
	return true
}

// forkQuietSuperseded (forkQuietMu held): the file changed during the restore; re-arm on it, leave it be. Ended unless
// it now holds a live lock: a removed or expired file (quiet.sh off's fallback) is no lock.
func forkQuietSuperseded() bool {
	forkQuietArm()
	if l, err := quiet.Read(); err == nil && forkQuietNow().Before(l.Until) {
		logger.Println("my-vector quiet: a newer lock landed during the restore; kept")
		return false
	}
	logger.Println("my-vector quiet: the lock file changed during the restore (removed or expired); re-armed on it")
	return true
}

// forkQuietSetAll sets v on every paired robot; false when one fails or none is known.
func forkQuietSetAll(v int) bool {
	esns := forkRobotESNs()
	if len(esns) == 0 {
		logger.Println("my-vector quiet: volume restore: no robots known")
		return false
	}
	ok := true
	for _, esn := range esns {
		if err := forkVolumeSet(esn, v); err != nil {
			logger.Println("my-vector quiet: volume restore: " + err.Error())
			ok = false
		} else {
			logger.Println("my-vector quiet: volume " + strconv.Itoa(v) + " restored on " + esn)
		}
	}
	return ok
}

// ForkQuietRearm arms the restore from an existing lock file at server start; an expired lock restores at once.
func ForkQuietRearm() {
	forkQuietMu.Lock()
	defer forkQuietMu.Unlock()
	forkQuietArm()
}

// forkQuietOff ends the lock now: stop the timer, restore the volume, remove the file, nap 0 on every robot; false = a
// newer lock landed meanwhile and was kept (no nap 0).
func forkQuietOff() bool {
	forkQuietMu.Lock()
	if forkQuietStop != nil {
		forkQuietStop()
		forkQuietStop = nil
	}
	l, err := quiet.Read()
	if err != nil {
		logger.Println("my-vector quiet: off: " + err.Error())
		l = quiet.Lock{Vol: quiet.NoVolume}
	}
	ended := forkQuietRestore(l)
	forkQuietMu.Unlock()
	if !ended {
		return false
	}
	for _, esn := range forkRobotESNs() {
		ip := forkRobotIP(esn)
		forkNaps.Add(1)
		go func(esn string) { defer forkNaps.Done(); forkNap(ip, esn, time.Time{}) }(esn)
	}
	return true
}

// forkQuietHandler serves ForkQuietAPI: ?on=<minutes> locks every paired robot as a voice "shut up" does; ?off=1 unlocks (409 + the
// QUIET line when a newer lock was kept).
func forkQuietHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	on, off := q.Get("on"), q.Get("off")
	switch {
	case on != "" && off == "":
		min, err := strconv.Atoi(on)
		if err != nil || min < 1 {
			http.Error(w, "on: minutes, at least 1", http.StatusBadRequest)
			return
		}
		now := forkQuietNow()
		until := now.Add(time.Duration(min) * time.Minute)
		if err := forkQuietStart(forkRobotESNs(), until, "cli:quiet.sh"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		line, _ := quiet.Line(now)
		fmt.Fprintln(w, line)
	case off == "1" && on == "":
		if !forkQuietOff() {
			line, _ := quiet.Line(forkQuietNow())
			http.Error(w, line, http.StatusConflict)
			return
		}
		fmt.Fprintln(w, "robot sound unlocked")
	default:
		http.Error(w, "usage: POST "+ForkQuietAPI+"?on=<minutes> | ?off=1", http.StatusBadRequest)
	}
}

// forkRobotVolume reads master_volume from the robot's RobotSettings jdoc.
func forkRobotVolume(esn string) (int, error) {
	if quiet.RobotOff() {
		return 0, quiet.ErrNapOff
	}
	robot, err := vars.GetRobot(esn)
	if err != nil {
		return 0, err
	}
	// The robot sometimes answers with a BStat doc first (upstream get_sdk_settings retries the same way).
	for try := 0; try < 4; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), forkVolumeTimeout)
		resp, err := robot.Conn.PullJdocs(ctx, &vectorpb.PullJdocsRequest{JdocTypes: []vectorpb.JdocType{vectorpb.JdocType_ROBOT_SETTINGS}})
		cancel()
		if err != nil {
			return 0, err
		}
		if len(resp.NamedJdocs) == 0 || resp.NamedJdocs[0].Doc == nil {
			return 0, errors.New("robot returned no RobotSettings jdoc")
		}
		doc := resp.NamedJdocs[0].Doc.JsonDoc
		if strings.Contains(doc, "BStat.ReactedToTriggerWord") {
			time.Sleep(time.Second / 2)
			continue
		}
		var s struct {
			MasterVolume *int `json:"master_volume"`
		}
		if err := json.Unmarshal([]byte(doc), &s); err != nil {
			return 0, err
		}
		if s.MasterVolume == nil || *s.MasterVolume < 0 || *s.MasterVolume > quiet.MaxVolume {
			return 0, fmt.Errorf("RobotSettings master_volume missing or out of range: %s", doc)
		}
		return *s.MasterVolume, nil
	}
	return 0, errors.New("robot keeps returning a BStat doc for RobotSettings")
}

// forkSetRobotVolume is upstream /api-sdk/volume's update_settings master_volume, over gRPC with an error instead of a panic.
func forkSetRobotVolume(esn string, v int) error {
	if quiet.RobotOff() {
		return quiet.ErrNapOff
	}
	robot, err := vars.GetRobot(esn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), forkVolumeTimeout)
	defer cancel()
	resp, err := robot.Conn.UpdateSettings(ctx, &vectorpb.UpdateSettingsRequest{Settings: &vectorpb.RobotSettingsConfig{
		OneofMasterVolume: &vectorpb.RobotSettingsConfig_MasterVolume{MasterVolume: vectorpb.Volume(v)},
	}})
	if err != nil {
		return err
	}
	if resp.Code != vectorpb.ResultCode_SETTINGS_ACCEPTED {
		return fmt.Errorf("update_settings master_volume %d: %s", v, resp.Code)
	}
	return nil
}
