// Package lang is the fork's one language switch (Estonian plan E1, E5), saved as "lang" in myvectorConfig.json.
package lang

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

const (
	English  = "en-US"
	Estonian = "et-EE"
	Default  = English
)

// Supported are the switch's languages, Default first.
var Supported = []string{English, Estonian}

// Path is the settings file; vars/fork_lang.go points it beside apiConfig.json (lang must not import vars).
var Path = func() string { return "myvectorConfig.json" }

// cached is the language load read from path; a Path change (vars.Init after package init) invalidates it.
type cached struct{ path, lang string }

var (
	cur    atomic.Pointer[cached]
	saveMu sync.Mutex // holds across the file write and cur.Store so both end on the same Save
)

func Valid(l string) bool {
	for _, s := range Supported {
		if l == s {
			return true
		}
	}
	return false
}

// Current is the saved language, cached per Path(); a missing, unreadable or invalid one gives Default.
func Current() string {
	path := Path()
	for {
		old := cur.Load()
		if old != nil && old.path == path {
			return old.lang
		}
		l := load(path)
		if cur.CompareAndSwap(old, &cached{path, l}) { // a lost race retries so a concurrent Save wins
			return l
		}
	}
}

func load(path string) string {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default
	}
	var f struct {
		Lang *string `json:"lang"`
	}
	if err == nil {
		err = json.Unmarshal(b, &f)
	}
	if err == nil && f.Lang == nil {
		return Default // a file from before the switch
	}
	if err == nil && !Valid(*f.Lang) {
		err = fmt.Errorf("lang %q: one of %v", *f.Lang, Supported)
	}
	if err != nil {
		logger.Println("myvector lang: using " + Default + ": " + err.Error())
		return Default
	}
	return *f.Lang
}

// Save validates l, writes it to the settings file and makes it Current.
func Save(l string) error {
	if !Valid(l) {
		return fmt.Errorf("lang: one of %v", Supported)
	}
	saveMu.Lock()
	defer saveMu.Unlock()
	path := Path()
	if err := conf.Update(path, map[string]any{"lang": l}); err != nil {
		return err
	}
	cur.Store(&cached{path, l})
	return nil
}
