package files

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const maxLogBytes int64 = 2 * 1024 * 1024

// logRetention is how long a log line is kept on this device.
const logRetention = 7 * 24 * time.Hour

var (
	dataDirMu       sync.RWMutex
	dataDirOverride string
	migrateOnce     sync.Once
)

// SetDataDirOverride overrides the application data directory (primarily for tests).
// Passing an empty string clears the override and restores normal resolution.
func SetDataDirOverride(dir string) {
	cleaned := ""
	if strings.TrimSpace(dir) != "" {
		cleaned = filepath.Clean(dir)
	}
	dataDirMu.Lock()
	dataDirOverride = cleaned
	dataDirMu.Unlock()

	logger.reset()
}

func hasDataDirOverride() bool {
	dataDirMu.RLock()
	has := dataDirOverride != ""
	dataDirMu.RUnlock()
	if has {
		return true
	}
	return strings.TrimSpace(os.Getenv("BLOCK_DATA_DIR")) != ""
}

// AppDataDir returns the per-user application data directory for Block.
// Priority: SetDataDirOverride > BLOCK_DATA_DIR > platform default
// (~/Library/Application Support/Block on macOS, %APPDATA%\Block on Windows,
// $XDG_DATA_HOME/Block or ~/.local/share/Block on Linux).
func AppDataDir() string {
	dataDirMu.RLock()
	override := dataDirOverride
	dataDirMu.RUnlock()
	if override != "" {
		return override
	}
	if env := strings.TrimSpace(os.Getenv("BLOCK_DATA_DIR")); env != "" {
		return filepath.Clean(env)
	}

	base := platformDataBaseDir()
	dir := filepath.Join(base, "Block")
	migrateOnce.Do(func() {
		migrateLegacyData(base, dir)
	})
	return dir
}

func platformDataBaseDir() string {
	switch runtime.GOOS {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "Library", "Application Support")
		}
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return appData
		}
		if cfg, err := os.UserConfigDir(); err == nil && cfg != "" {
			return cfg
		}
	default:
		if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
			return xdg
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "share")
		}
	}
	return os.TempDir()
}

func migrateLegacyData(base, target string) {
	for _, name := range []string{"thinkly-fast", "Thinkly Fast"} {
		legacy := filepath.Join(base, name)
		entries, err := os.ReadDir(legacy)
		if err != nil {
			continue
		}
		_ = os.MkdirAll(target, 0o755)
		for _, entry := range entries {
			dest := filepath.Join(target, entry.Name())
			if _, err := os.Lstat(dest); os.IsNotExist(err) {
				_ = os.Rename(filepath.Join(legacy, entry.Name()), dest)
			}
		}
	}
}

type fileLogger struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	context string
}

var logger = &fileLogger{context: "startup"}

// SetLogContext sets the optional context label included in subsequent log lines.
func SetLogContext(ctx string) {
	logger.mu.Lock()
	logger.context = ctx
	logger.mu.Unlock()
}

func (l *fileLogger) reset() {
	l.mu.Lock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	l.path = ""
	l.mu.Unlock()
}

func resolveLogPath() string {
	if custom := strings.TrimSpace(os.Getenv("BLOCK_LOG_FILE")); custom != "" {
		return filepath.Clean(custom)
	}
	return filepath.Join(AppDataDir(), "logs", "block.log")
}

// LogFilePath returns the resolved path to Block's diagnostic log file.
func LogFilePath() string {
	return resolveLogPath()
}

func openLogFile(path string) *os.File {
	if parent := filepath.Dir(path); parent != "" {
		_ = os.MkdirAll(parent, 0o755)
	}
	prev := filepath.Join(filepath.Dir(path), "block.prev.log")
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Rename(path, prev)
	}
	pruneLogs(path, prev, time.Now().Add(-logRetention))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}

func (l *fileLogger) writeLine(level, msg string, flushDisk bool) {
	now := time.Now()
	secs := now.Unix()
	millis := now.Nanosecond() / int(time.Millisecond)
	ts := fmt.Sprintf("%d.%03d", secs, millis)

	targetPath := resolveLogPath()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil && l.path != targetPath {
		_ = l.file.Close()
		l.file = nil
		l.path = ""
	}
	if l.file == nil {
		l.path = targetPath
		l.file = openLogFile(targetPath)
	}

	msg = Redact(strings.ReplaceAll(msg, "\n", " | "))

	var line string
	if l.context == "" {
		line = fmt.Sprintf("[%s] [%s] %s\n", ts, level, msg)
	} else {
		line = fmt.Sprintf("[%s] [%s] (%s) %s\n", ts, level, l.context, msg)
	}

	_, _ = os.Stderr.WriteString(line)
	if l.file != nil {
		_, _ = l.file.WriteString(line)
		if flushDisk {
			_ = l.file.Sync()
		}
	}
}

// LogInfof writes an INFO diagnostic entry to logs/block.log.
//
// Every Log*f line is masked before it reaches the disk (see redact.go):
// pass paths, names and errors as arguments, never note content.
func LogInfof(format string, args ...any) {
	logger.writeLine("INFO", fmt.Sprintf(format, redactArgs(args)...), false)
}

// LogWarnf writes a WARN diagnostic entry to logs/block.log.
func LogWarnf(format string, args ...any) {
	logger.writeLine("WARN", fmt.Sprintf(format, redactArgs(args)...), false)
}

// LogErrorf writes an ERROR diagnostic entry to logs/block.log and flushes to disk.
func LogErrorf(format string, args ...any) {
	logger.writeLine("ERROR", fmt.Sprintf(format, redactArgs(args)...), true)
}
