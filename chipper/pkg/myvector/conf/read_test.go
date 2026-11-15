package conf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func captureLog(t *testing.T) *[]string {
	t.Helper()
	var logs []string
	old := logf
	logf = func(s string) { logs = append(logs, s) }
	t.Cleanup(func() { logf = old })
	return &logs
}

// A corrupt file logs once across two reads and again once the file changes; missing and valid files never log.
func TestReadLogsBadFileOncePerMtime(t *testing.T) {
	logs := captureLog(t)
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	var v struct{ Lang string }
	if err := Read(path, &v); !errors.Is(err, fs.ErrNotExist) || len(*logs) != 0 {
		t.Fatalf("missing: err %v, logs %q", err, *logs)
	}
	os.WriteFile(path, []byte(`{`), 0644)
	for i := 0; i < 2; i++ {
		if Read(path, &v) == nil {
			t.Fatal("corrupt file read without error")
		}
	}
	if len(*logs) != 1 || !strings.HasPrefix((*logs)[0], "myvector: bad config: "+path+": ") {
		t.Fatalf("after two corrupt reads: logs %q, want one bad-config line", *logs)
	}
	os.WriteFile(path, []byte(`{"lang":`), 0644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
	Read(path, &v)
	if len(*logs) != 2 {
		t.Fatalf("after the file changed: logs %q, want a second line", *logs)
	}
	os.WriteFile(path, []byte(`{"lang":"et-EE"}`), 0644)
	if err := Read(path, &v); err != nil || v.Lang != "et-EE" || len(*logs) != 2 {
		t.Fatalf("valid: err %v, v %+v, logs %q", err, v, *logs)
	}
}
