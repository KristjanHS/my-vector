// Package conf writes the fork's settings file (myvectorConfig.json), which several packages share key by key.
package conf

import (
	"encoding/json"
	"errors"
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
	tmp, err := os.CreateTemp(filepath.Dir(path), ".myvectorConfig-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Chmod(0644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	return err
}
