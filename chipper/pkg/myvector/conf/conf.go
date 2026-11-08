// Package conf writes the fork's settings file (myvectorConfig.json), which several packages share key by key,
// and finds the repo's robot/ scripts (FindUp).
package conf

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var mu sync.Mutex

// Update sets kv in the JSON object at path, keeps every other key, and replaces the file atomically.
// A missing or unparsable file starts from an empty object; any other read error returns without writing.
func Update(path string, kv map[string]any) error {
	mu.Lock()
	defer mu.Unlock()
	obj := map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil && json.Unmarshal(b, &obj) != nil {
		obj = map[string]json.RawMessage{}
	}
	if obj == nil { // the file held JSON null
		obj = map[string]json.RawMessage{}
	}
	for k, v := range kv {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj[k] = b
	}
	b, _ = json.MarshalIndent(obj, "", "  ")
	return WriteFile(path, ".myvectorConfig-*", append(b, '\n'), 0644)
}

var rename = os.Rename // tests make it fail

// WriteFile replaces path with b via a tmpPattern temp file in path's dir + rename; a failure leaves the old file and no temp.
func WriteFile(path, tmpPattern string, b []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPattern)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Chmod(perm)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = rename(tmp.Name(), path)
	}
	return err
}

// FindUp is rel (a file) in the nearest ancestor of the working directory (wire-pod's is chipper/), e.g. a robot/ script.
func FindUp(rel string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s above the working directory", rel)
		}
		dir = parent
	}
}
