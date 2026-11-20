package files

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DiagnosticLine is one masked log entry as the feedback dialog shows it.
type DiagnosticLine struct {
	Time    string // local HH:MM:SS
	Level   string // INFO, WARN, ERROR
	Message string
}

// DiagnosticLog is the masked tail of the app log attached to a feedback
// report: exactly the bytes that are uploaded, and the lines behind them.
type DiagnosticLog struct {
	Lines []DiagnosticLine
	Text  string
}

// Bytes is the upload size of the log.
func (d DiagnosticLog) Bytes() int64 { return int64(len(d.Text)) }

// logLineTime parses the "[unix.millis]" prefix of a log line.
func logLineTime(line string) (time.Time, string, bool) {
	if !strings.HasPrefix(line, "[") {
		return time.Time{}, line, false
	}
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return time.Time{}, line, false
	}
	stamp := line[1:end]
	secsText, millisText, _ := strings.Cut(stamp, ".")
	secs, err := strconv.ParseInt(secsText, 10, 64)
	if err != nil {
		return time.Time{}, line, false
	}
	millis, _ := strconv.ParseInt(millisText, 10, 64)
	return time.Unix(secs, millis*int64(time.Millisecond)), strings.TrimSpace(line[end+1:]), true
}

// pruneLogs enforces the retention window: the rotated log is deleted once
// it is older than the cutoff, and expired lines are dropped from the head
// of the live log. Runs when the log file is opened.
func pruneLogs(path, prev string, cutoff time.Time) {
	if info, err := os.Stat(prev); err == nil && info.ModTime().Before(cutoff) {
		_ = os.Remove(prev)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	keepFrom := 0
	offset := 0
	for offset < len(raw) {
		nl := bytes.IndexByte(raw[offset:], '\n')
		lineEnd := len(raw)
		if nl >= 0 {
			lineEnd = offset + nl + 1
		}
		at, _, ok := logLineTime(string(raw[offset:lineEnd]))
		if ok && !at.Before(cutoff) {
			break
		}
		keepFrom = lineEnd
		offset = lineEnd
	}
	if keepFrom == 0 {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw[keepFrom:], 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// ReadDiagnosticLog returns the newest maxLines log entries inside the
// retention window, masked again line by line so that entries written by an
// older version of the app are covered too.
func ReadDiagnosticLog(maxLines int) DiagnosticLog {
	path := resolveLogPath()
	logger.mu.Lock()
	if logger.file != nil {
		_ = logger.file.Sync()
	}
	logger.mu.Unlock()

	cutoff := time.Now().Add(-logRetention)
	var lines []DiagnosticLine
	for _, name := range []string{filepath.Join(filepath.Dir(path), "block.prev.log"), path} {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			at, rest, ok := logLineTime(sc.Text())
			if !ok || at.Before(cutoff) {
				continue
			}
			level := "INFO"
			if strings.HasPrefix(rest, "[") {
				if end := strings.IndexByte(rest, ']'); end > 0 {
					level = rest[1:end]
					rest = strings.TrimSpace(rest[end+1:])
				}
			}
			lines = append(lines, DiagnosticLine{
				Time:    at.Local().Format("15:04:05"),
				Level:   level,
				Message: Redact(rest),
			})
		}
		_ = f.Close()
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Time)
		b.WriteByte(' ')
		b.WriteString(l.Level)
		b.WriteByte(' ')
		b.WriteString(l.Message)
		b.WriteByte('\n')
	}
	return DiagnosticLog{Lines: lines, Text: b.String()}
}
