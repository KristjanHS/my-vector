package photo

import (
	"testing"
	"time"
)

func TestFresh(t *testing.T) {
	var s Store
	t0 := time.Unix(1000, 0)
	if _, ok := s.Fresh("e", t0); ok {
		t.Fatal("empty store is fresh")
	}
	s.Put("e", []byte{1}, "et-EE", t0)
	if _, ok := s.Fresh("e", t0); ok {
		t.Fatal("unanswered capture is fresh")
	}
	s.Answered("e", t0.Add(5*time.Second))
	if sh, ok := s.Fresh("e", t0.Add(5*time.Second+Keep)); !ok || sh.JPEG[0] != 1 || sh.Lang != "et-EE" {
		t.Fatalf("fresh at Keep = %v %v", sh, ok)
	}
	if _, ok := s.Fresh("e", t0.Add(6*time.Second+Keep)); ok {
		t.Fatal("fresh past Keep")
	}
	if _, ok := s.Fresh("other", t0.Add(5*time.Second)); ok {
		t.Fatal("photo leaks across robots")
	}
	s.Put("e", []byte{2}, "en-US", t0.Add(10*time.Second))
	if _, ok := s.Fresh("e", t0.Add(10*time.Second)); ok {
		t.Fatal("a new capture keeps the old answer's freshness")
	}
}

func TestTakeTurnAndListen(t *testing.T) {
	var s Store
	s.Answered("e", time.Unix(1, 0))
	if s.TakeTurn("e") {
		t.Fatal("answer without a photo counts")
	}
	s.Put("e", []byte{1}, "et-EE", time.Unix(1, 0))
	s.Answered("e", time.Unix(2, 0))
	if !s.TakeTurn("e") || s.TakeTurn("e") {
		t.Fatal("TakeTurn is not take-once")
	}
	s.Listen("e", true)
	if !s.TakeListen("e") || s.TakeListen("e") {
		t.Fatal("TakeListen is not take-once")
	}
	s.Listen("e", true)
	s.Listen("e", false)
	if s.TakeListen("e") {
		t.Fatal("a non-photo arm keeps the photo listen")
	}
}

func TestPutFresh(t *testing.T) {
	var s Store
	t0 := time.Unix(1000, 0)
	s.PutFresh("e", []byte{3}, "et-EE", t0)
	if sh, ok := s.Fresh("e", t0.Add(Keep)); !ok || sh.JPEG[0] != 3 || sh.Lang != "et-EE" {
		t.Fatalf("fresh at Keep = %v %v", sh, ok)
	}
	if s.TakeTurn("e") {
		t.Fatal("PutFresh counts as this turn's photo answer")
	}
}
