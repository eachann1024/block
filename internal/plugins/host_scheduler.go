package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"block/internal/files"
)

const (
	schedulerFile             = "scheduler.json"
	maxSchedulerFileBytes     = 256 * 1024
	maxScheduledJobsPerPlugin = 32
	minSchedulerIntervalSec   = 60
	maxSchedulerIntervalSec   = 86400
	defaultSchedulerEvent     = "scheduler.tick"
)

var (
	schedulerIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	schedulerEventPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	pluginSchedulerLocks  sync.Map // cleaned DataDir -> *sync.Mutex
)

// ScheduledJob defines one persisted plugin background schedule entry stored in scheduler.json.
type ScheduledJob struct {
	ID                  string `json:"id"`
	IntervalSec         int    `json:"interval_sec"`
	Event               string `json:"event"`
	RequireNetwork      bool   `json:"require_network,omitempty"`
	RunOnWake           bool   `json:"run_on_wake,omitempty"`
	RunOnNetworkRestore bool   `json:"run_on_network_restore,omitempty"`
	RunOnFocus          bool   `json:"run_on_focus,omitempty"`
	RegisteredAt        string `json:"registered_at,omitempty"`
	LastRunAt           string `json:"last_run_at,omitempty"`
}

func schedulerLock(dataDir string) *sync.Mutex {
	mu, _ := pluginSchedulerLocks.LoadOrStore(filepath.Clean(dataDir), &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func clampSchedulerInterval(sec int) int {
	if sec < minSchedulerIntervalSec {
		return minSchedulerIntervalSec
	}
	if sec > maxSchedulerIntervalSec {
		return maxSchedulerIntervalSec
	}
	return sec
}

func loadScheduledJobsFromRoot(root *os.Root) ([]ScheduledJob, error) {
	if root == nil {
		return []ScheduledJob{}, nil
	}
	b, err := readPluginFile(root, schedulerFile, maxSchedulerFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return []ScheduledJob{}, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs []ScheduledJob
	if err := json.Unmarshal(b, &jobs); err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []ScheduledJob{}
	}
	return jobs, nil
}

func saveScheduledJobsToRoot(root *os.Root, jobs []ScheduledJob) error {
	if jobs == nil {
		jobs = []ScheduledJob{}
	}
	raw, err := json.Marshal(jobs)
	if err != nil {
		return err
	}
	if len(raw) > maxSchedulerFileBytes {
		return errors.New("scheduler state exceeds size limit")
	}
	return files.WriteNoteAt(root, schedulerFile, string(raw))
}

func loadScheduledJobsDir(dataDir string) ([]ScheduledJob, error) {
	if strings.TrimSpace(dataDir) == "" {
		return []ScheduledJob{}, nil
	}
	root, err := os.OpenRoot(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return []ScheduledJob{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return loadScheduledJobsFromRoot(root)
}

func (rt *Runtime) dispatchSchedulerHostMethod(method string, params json.RawMessage) (any, *rpcError) {
	if !rt.manifest.HasPermission(PermEventsSystem) {
		return nil, missingPerm(PermEventsSystem)
	}
	if rt.dataRoot == nil || strings.TrimSpace(rt.cfg.DataDir) == "" {
		return nil, rpcErr(-32004, "plugin data directory unavailable")
	}

	switch method {
	case "host.scheduler.register":
		var p struct {
			ID                  string `json:"id"`
			IntervalSec         int    `json:"interval_sec"`
			Event               string `json:"event"`
			RequireNetwork      bool   `json:"require_network"`
			RunOnWake           bool   `json:"run_on_wake"`
			RunOnNetworkRestore bool   `json:"run_on_network_restore"`
			RunOnFocus          bool   `json:"run_on_focus"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpcErr(-32602, "invalid host.scheduler.register params: "+err.Error())
		}
		id := strings.TrimSpace(p.ID)
		if !schedulerIDPattern.MatchString(id) {
			return nil, rpcErr(-32602, "invalid scheduler job id")
		}
		ev := strings.TrimSpace(p.Event)
		if ev == "" {
			ev = defaultSchedulerEvent
		} else if !schedulerEventPattern.MatchString(ev) {
			return nil, rpcErr(-32602, "invalid scheduler event name")
		}
		interval := clampSchedulerInterval(p.IntervalSec)
		nowStr := time.Now().UTC().Format(time.RFC3339Nano)

		mu := schedulerLock(rt.cfg.DataDir)
		mu.Lock()
		defer mu.Unlock()
		jobs, err := loadScheduledJobsFromRoot(rt.dataRoot)
		if err != nil {
			return nil, rpcErr(-32002, err.Error())
		}
		idx := slices.IndexFunc(jobs, func(j ScheduledJob) bool { return j.ID == id })
		entry := ScheduledJob{
			ID:                  id,
			IntervalSec:         interval,
			Event:               ev,
			RequireNetwork:      p.RequireNetwork,
			RunOnWake:           p.RunOnWake,
			RunOnNetworkRestore: p.RunOnNetworkRestore,
			RunOnFocus:          p.RunOnFocus,
			RegisteredAt:        nowStr,
		}
		if idx >= 0 {
			if jobs[idx].RegisteredAt != "" {
				entry.RegisteredAt = jobs[idx].RegisteredAt
			}
			entry.LastRunAt = jobs[idx].LastRunAt
			jobs[idx] = entry
		} else {
			if len(jobs) >= maxScheduledJobsPerPlugin {
				return nil, rpcErr(-32602, "too many scheduled jobs")
			}
			jobs = append(jobs, entry)
		}
		if err := saveScheduledJobsToRoot(rt.dataRoot, jobs); err != nil {
			return nil, rpcErr(-32002, err.Error())
		}
		return map[string]any{"ok": true, "id": id}, nil

	case "host.scheduler.unregister":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpcErr(-32602, "invalid host.scheduler.unregister params: "+err.Error())
		}
		id := strings.TrimSpace(p.ID)
		if !schedulerIDPattern.MatchString(id) {
			return nil, rpcErr(-32602, "invalid scheduler job id")
		}
		mu := schedulerLock(rt.cfg.DataDir)
		mu.Lock()
		defer mu.Unlock()
		jobs, err := loadScheduledJobsFromRoot(rt.dataRoot)
		if err != nil {
			return nil, rpcErr(-32002, err.Error())
		}
		next := slices.DeleteFunc(jobs, func(j ScheduledJob) bool { return j.ID == id })
		if len(next) != len(jobs) {
			if err := saveScheduledJobsToRoot(rt.dataRoot, next); err != nil {
				return nil, rpcErr(-32002, err.Error())
			}
		}
		return map[string]any{"ok": true}, nil

	case "host.scheduler.list":
		mu := schedulerLock(rt.cfg.DataDir)
		mu.Lock()
		defer mu.Unlock()
		jobs, err := loadScheduledJobsFromRoot(rt.dataRoot)
		if err != nil {
			return nil, rpcErr(-32002, err.Error())
		}
		return map[string]any{"jobs": jobs}, nil

	default:
		return nil, rpcErr(-32601, "unknown scheduler method: "+method)
	}
}

func parseScheduledTime(raw string) time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func (s *Supervisor) schedulerDataDir(pluginID string) string {
	if inst, ok := s.GetInstalled(pluginID); ok && strings.TrimSpace(inst.DataDir) != "" {
		return inst.DataDir
	}
	if s.store != nil && strings.TrimSpace(s.store.DataRoot) != "" {
		return filepath.Join(s.store.DataRoot, pluginID)
	}
	return ""
}

func (s *Supervisor) clearPluginScheduler(pluginID string) {
	if s == nil || strings.TrimSpace(pluginID) == "" {
		return
	}
	if dataDir := s.schedulerDataDir(pluginID); dataDir != "" {
		mu := schedulerLock(dataDir)
		mu.Lock()
		_ = os.Remove(filepath.Join(dataDir, schedulerFile))
		mu.Unlock()
	}
	s.system.mu.Lock()
	if q := s.system.events[pluginID]; q != nil {
		for k := range q.pending {
			if strings.HasPrefix(k, "job:") {
				delete(q.pending, k)
			}
		}
	}
	s.system.mu.Unlock()
}

func (s *Supervisor) isSystemOnline() bool {
	if s.cfg.IsOnline != nil {
		return s.cfg.IsOnline()
	}
	s.system.mu.Lock()
	known := s.system.onlineKnown
	online := s.system.online
	s.system.mu.Unlock()
	if !known {
		return true
	}
	return online
}

// TickScheduledJobs evaluates all registered scheduler jobs across enabled
// plugins and queues plugin.event deliveries for any job whose interval has elapsed.
func (s *Supervisor) TickScheduledJobs(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	online := s.isSystemOnline()
	for _, installed := range s.Installed() {
		manifest := installed.Manifest
		if !installed.Enabled || manifest.IsDeclarativeOnly() || !manifest.HasPermission(PermEventsSystem) {
			continue
		}
		if strings.TrimSpace(installed.DataDir) == "" {
			continue
		}
		var due []ScheduledJob
		mu := schedulerLock(installed.DataDir)
		mu.Lock()
		root, err := os.OpenRoot(installed.DataDir)
		if err == nil {
			jobs, loadErr := loadScheduledJobsFromRoot(root)
			if loadErr == nil && len(jobs) > 0 {
				dirty := false
				for i := range jobs {
					job := &jobs[i]
					if job.RequireNetwork && !online {
						continue
					}
					interval := time.Duration(clampSchedulerInterval(job.IntervalSec)) * time.Second
					anchor := parseScheduledTime(job.LastRunAt)
					if anchor.IsZero() {
						anchor = parseScheduledTime(job.RegisteredAt)
					}
					if !anchor.IsZero() && now.Sub(anchor) < interval {
						continue
					}
					job.LastRunAt = now.UTC().Format(time.RFC3339Nano)
					dirty = true
					due = append(due, *job)
				}
				if dirty {
					_ = saveScheduledJobsToRoot(root, jobs)
				}
			}
			_ = root.Close()
		}
		mu.Unlock()

		for _, job := range due {
			ev := job.Event
			if ev == "" {
				ev = defaultSchedulerEvent
			}
			s.queueSystemEvent(manifest.ID, "job:"+job.ID+":interval", map[string]any{
				"event":     ev,
				"job_id":    job.ID,
				"trigger":   "interval",
				"timestamp": now.UTC().Format(time.RFC3339),
				"online":    online,
			})
		}
	}
}

func (s *Supervisor) dispatchScheduledJobsForTrigger(eventName string, online bool) {
	var trigger string
	switch eventName {
	case "system.wake":
		trigger = "wake"
	case "network.changed":
		if !online {
			return
		}
		trigger = "network"
	case "app.focus":
		trigger = "focus"
	default:
		return
	}
	effectiveOnline := online
	if eventName == "app.focus" {
		effectiveOnline = s.isSystemOnline()
	}
	now := time.Now().UTC()
	for _, installed := range s.Installed() {
		manifest := installed.Manifest
		if !installed.Enabled || manifest.IsDeclarativeOnly() || !manifest.HasPermission(PermEventsSystem) {
			continue
		}
		if strings.TrimSpace(installed.DataDir) == "" {
			continue
		}
		mu := schedulerLock(installed.DataDir)
		mu.Lock()
		jobs, err := loadScheduledJobsDir(installed.DataDir)
		mu.Unlock()
		if err != nil || len(jobs) == 0 {
			continue
		}
		for _, job := range jobs {
			switch trigger {
			case "wake":
				if !job.RunOnWake || (job.RequireNetwork && !effectiveOnline) {
					continue
				}
			case "network":
				if !job.RunOnNetworkRestore {
					continue
				}
			case "focus":
				if !job.RunOnFocus || (job.RequireNetwork && !effectiveOnline) {
					continue
				}
			}
			ev := job.Event
			if ev == "" {
				ev = defaultSchedulerEvent
			}
			s.queueSystemEvent(manifest.ID, "job:"+job.ID+":"+trigger, map[string]any{
				"event":     ev,
				"job_id":    job.ID,
				"trigger":   trigger,
				"timestamp": now.Format(time.RFC3339),
				"online":    effectiveOnline,
			})
		}
	}
}
