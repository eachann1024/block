package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/files"
)

const maxUpdaterLogBytes int64 = 1024 * 1024

type diagLogger struct {
	mu      sync.Mutex
	dataDir string
}

var diagLog = &diagLogger{}

// SetLogDataDir binds the updater flight-recorder log directory (<dataDir>/logs/updater.log).
func SetLogDataDir(dataDir string) {
	diagLog.mu.Lock()
	diagLog.dataDir = strings.TrimSpace(dataDir)
	diagLog.mu.Unlock()
}

// LogFilePath returns the path to updater.log for diagnostics and UI reveal.
func LogFilePath(dataDir string) string {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = files.AppDataDir()
	}
	return filepath.Join(base, "logs", "updater.log")
}

// InstallLogFilePath returns the path to updater-install.log written by detached OS swap scripts.
func InstallLogFilePath(dataDir string) string {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		base = files.AppDataDir()
	}
	return filepath.Join(base, "logs", "updater-install.log")
}

func (l *diagLogger) write(level, event string, kv ...any) {
	ts := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	var b strings.Builder
	b.WriteString(ts)
	b.WriteString(" [")
	b.WriteString(level)
	b.WriteString("] ")
	b.WriteString(event)

	for i := 0; i+1 < len(kv); i += 2 {
		k := fmt.Sprint(kv[i])
		v := strings.ReplaceAll(strings.ReplaceAll(fmt.Sprint(kv[i+1]), "\n", " "), "\r", " ")
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteByte('=')
		if strings.ContainsAny(v, " \t\"") {
			b.WriteString(fmt.Sprintf("%q", v))
		} else {
			b.WriteString(v)
		}
	}
	summary := b.String()
	line := summary + "\n"

	l.mu.Lock()
	dir := l.dataDir
	if dir == "" {
		dir = files.AppDataDir()
	}
	if dir != "" {
		logPath := filepath.Join(dir, "logs", "updater.log")
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		if info, err := os.Stat(logPath); err == nil && info.Size() > maxUpdaterLogBytes {
			_ = os.Rename(logPath, filepath.Join(filepath.Dir(logPath), "updater.prev.log"))
		}
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = f.WriteString(line)
			if level == "ERROR" {
				_ = f.Sync()
			}
			_ = f.Close()
		}
	}
	l.mu.Unlock()

	// Forward WARN and ERROR to Block's main logs/block.log (which strictly logs WARN/ERROR/PANIC).
	msg := strings.TrimSpace(summary[len(ts):])
	switch level {
	case "WARN":
		files.LogWarnf("[updater] %s", msg)
	case "ERROR":
		files.LogErrorf("[updater] %s", msg)
	}
}

func logInfo(event string, kv ...any) {
	diagLog.write("INFO", event, kv...)
}

func logWarn(event string, kv ...any) {
	diagLog.write("WARN", event, kv...)
}

func logError(event string, kv ...any) {
	diagLog.write("ERROR", event, kv...)
}
