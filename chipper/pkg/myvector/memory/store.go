// Package memory is the fork's long-term memory: captured episodes, extracted facts, profiles and a diary per robot,
// and the {facts}/{diary} prompt blocks built from them.
package memory

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

// Path is the store file; vars/fork_prompt.go points it beside apiConfig.json (memory must not import vars).
var Path = func() string { return "myvectorMemory.json" }

var logf = logger.Println

// Caps in runes (ProfileCap in prompt.Estimate tokens).
const (
	FactCap     = 160
	EvidenceCap = 200
	ProfileCap  = 150
	DiaryCap    = 300
)

// Categories is the closed fact category set; a candidate with any other category is dropped.
var Categories = []string{"identity", "family", "pets", "home", "work", "health", "likes", "dislikes", "routine", "plans"}

// Fact statuses.
const (
	Unconfirmed = "unconfirmed"
	Active      = "active"
	Superseded  = "superseded"
	Removed     = "removed"
)

// Fact is one remembered statement about a robot's user: Text in English, Evidence verbatim from a user turn.
type Fact struct {
	ID           string     `json:"id"`
	ESN          string     `json:"esn"`
	Category     string     `json:"category"`
	Text         string     `json:"text"`
	Evidence     string     `json:"evidence"`
	EventAt      string     `json:"event_at,omitempty"`
	LearnedAt    time.Time  `json:"learned_at"`
	Session      string     `json:"session,omitempty"`
	Status       string     `json:"status"`
	SupersededBy string     `json:"superseded_by,omitempty"`
	RemovedAt    *time.Time `json:"removed_at,omitempty"`
}

// Entry is one diary line per (ESN, day); Status is "" or Removed.
type Entry struct {
	ID        string     `json:"id"`
	ESN       string     `json:"esn"`
	Day       string     `json:"day"`
	Text      string     `json:"text"`
	At        time.Time  `json:"at"`
	Status    string     `json:"status,omitempty"`
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

// Profile is an ESN's English summary of its active facts.
type Profile struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Store is myvectorMemory.json.
type Store struct {
	Facts     []Fact             `json:"facts"`
	Diary     []Entry            `json:"diary"`
	Profiles  map[string]Profile `json:"profiles"`
	Extracted []string           `json:"extracted"`
	Retries   map[string]int     `json:"retries,omitempty"` // session id → unparsable extraction replies
	LastRun   time.Time          `json:"last_run"`
	LastGuard string             `json:"last_guard,omitempty"`
}

var mu sync.Mutex

// load reads the store; missing → empty, corrupt → empty with a log (the next save replaces it). Caller holds mu.
func load() (*Store, error) {
	s := &Store{}
	b, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return s.init(), nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		logf("myvector: memory store corrupt, starting empty:", Path(), err)
		s = &Store{}
	}
	return s.init(), nil
}

func (s *Store) init() *Store {
	if s.Profiles == nil {
		s.Profiles = map[string]Profile{}
	}
	if s.Retries == nil {
		s.Retries = map[string]int{}
	}
	return s
}

// Read returns a snapshot of the store.
func Read() (*Store, error) {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

// Update loads the store, applies fn and saves it atomically; an fn error saves nothing.
func Update(fn func(*Store) error) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := load()
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return conf.WriteFile(Path(), ".myvectorMemory-*", append(b, '\n'), 0600)
}
