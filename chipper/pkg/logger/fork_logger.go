package logger

import "sync"

// forkLogMu guards LogArray/LogList and LogTrayArray/LogTrayList: every goroutine logs through LogTray.
var forkLogMu sync.Mutex

// ForkLogList is LogList read under the lock (web UI /api/get_logs).
func ForkLogList() string {
	forkLogMu.Lock()
	defer forkLogMu.Unlock()
	return LogList
}

// ForkLogTrayList is LogTrayList read under the lock (web UI /api/get_debug_logs).
func ForkLogTrayList() string {
	forkLogMu.Lock()
	defer forkLogMu.Unlock()
	return LogTrayList
}
