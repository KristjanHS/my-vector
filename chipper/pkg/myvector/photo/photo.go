// Package photo keeps each robot's last camera photo for picture follow-ups (2026-11-10-photo-followup.md
// (docs/plans/) F1, F2, F6).
package photo

import (
	"sync"
	"time"
)

// Keep is how long after its last answer a photo stays fresh (F2).
const Keep = 120 * time.Second

// Shot is one kept photo.
type Shot struct {
	JPEG  []byte
	Taken time.Time
	Lang  string
}

type entry struct {
	shot     Shot
	answered time.Time // last spoken answer about shot; zero = none yet
	turn     bool      // answered during the current request (F6)
	listen   bool      // the open follow-up listen was armed by a photo answer (F6)
}

// Store holds one Shot per ESN; the zero value is ready.
type Store struct {
	mu sync.Mutex
	m  map[string]*entry
}

func (s *Store) get(esn string) *entry {
	if s.m == nil {
		s.m = map[string]*entry{}
	}
	e := s.m[esn]
	if e == nil {
		e = &entry{}
		s.m[esn] = e
	}
	return e
}

// Put replaces esn's photo with a new capture; it is not fresh until Answered.
func (s *Store) Put(esn string, jpeg []byte, lang string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	e.shot, e.answered = Shot{JPEG: jpeg, Taken: now, Lang: lang}, time.Time{}
}

// PutFresh stores a photo already shown to the user (native photo, N3): fresh from now, not answered this turn.
func (s *Store) PutFresh(esn string, jpeg []byte, lang string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	e.shot, e.answered = Shot{JPEG: jpeg, Taken: now, Lang: lang}, now
}

// Answered marks esn's photo answered at now: fresh again, and answered this turn.
func (s *Store) Answered(esn string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	if e.shot.JPEG != nil {
		e.answered, e.turn = now, true
	}
}

// Fresh is esn's photo when its last answer is at most Keep before now.
func (s *Store) Fresh(esn string, now time.Time) (Shot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	if e.answered.IsZero() || now.Before(e.answered) || now.Sub(e.answered) > Keep {
		return Shot{}, false
	}
	return e.shot, true
}

// TakeTurn reports and clears whether esn's photo was answered since the last TakeTurn.
func (s *Store) TakeTurn(esn string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	t := e.turn
	e.turn = false
	return t
}

// Listen records whether esn's just-armed follow-up listen was armed by a photo answer.
func (s *Store) Listen(esn string, photo bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.get(esn).listen = photo
}

// TakeListen reports and clears Listen.
func (s *Store) TakeListen(esn string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.get(esn)
	l := e.listen
	e.listen = false
	return l
}
