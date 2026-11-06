// Package lang is the fork's one language switch (Estonian plan E1, E5), saved as "lang" in myvectorConfig.json.
package lang

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

const (
	English  = "en-US"
	Estonian = "et-EE"
	Default  = English
	// DefaultETModel is the et-EE Ollama model (plan E11, E25) when "et_model" is unset.
	DefaultETModel = "hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M"
	// ETModelMax is the longest "et_model" SaveETModel takes, in characters.
	ETModelMax = 200
)

// Supported are the switch's languages, Default first.
var Supported = []string{English, Estonian}

// Path is the settings file; vars/fork_lang.go points it beside apiConfig.json (lang must not import vars).
var Path = func() string { return "myvectorConfig.json" }

// cached is the language load read from path; a Path change (vars.Init after package init) invalidates it.
type cached struct{ path, lang, etModel string }

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
func Current() string { return settings().lang }

// ETModel is the saved "et_model", cached like Current; missing, empty or not a string gives DefaultETModel.
func ETModel() string { return settings().etModel }

func settings() *cached {
	path := Path()
	for {
		old := cur.Load()
		if old != nil && old.path == path {
			return old
		}
		c := &cached{path, load(path), loadETModel(path)}
		if cur.CompareAndSwap(old, c) { // a lost race retries so a concurrent Save wins
			return c
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

func loadETModel(path string) string {
	var f struct {
		ETModel any `json:"et_model"`
	}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &f) == nil {
		if m, ok := f.ETModel.(string); ok && m != "" {
			return m
		}
	}
	return DefaultETModel
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
	cur.Store(&cached{path, l, loadETModel(path)})
	return nil
}

// ValidETModel checks an "et_model" for SaveETModel: after trimming, no whitespace and at most ETModelMax characters (empty is fine).
func ValidETModel(m string) error {
	m = strings.TrimSpace(m)
	if strings.IndexFunc(m, unicode.IsSpace) >= 0 {
		return errors.New("et_model: no whitespace")
	}
	if len([]rune(m)) > ETModelMax {
		return fmt.Errorf("et_model: at most %d characters", ETModelMax)
	}
	return nil
}

// SaveETModel validates and trims m, writes it as "et_model" and makes it ETModel; empty gives DefaultETModel.
func SaveETModel(m string) error {
	if err := ValidETModel(m); err != nil {
		return err
	}
	m = strings.TrimSpace(m)
	saveMu.Lock()
	defer saveMu.Unlock()
	path := Path()
	if err := conf.Update(path, map[string]any{"et_model": m}); err != nil {
		return err
	}
	c := &cached{path, load(path), m}
	if m == "" {
		c.etModel = DefaultETModel
	}
	cur.Store(c)
	return nil
}
