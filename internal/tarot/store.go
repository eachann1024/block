package tarot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CollectedEntry stores the latest note & timestamp recorded for a Major Arcana card (0..21).
type CollectedEntry struct {
	ArcanaID  int    `json:"arcana_id"`
	NotePath  string `json:"note_path"`
	NoteTitle string `json:"note_title"`
	DrawnUnix int64  `json:"drawn_unix"`
	Reversed  bool   `json:"reversed,omitempty"`
	Count     int    `json:"count,omitempty"`
}

// DailyRecord persists today's daily card so it stays stable until flipped/next day.
type DailyRecord struct {
	Date           string   `json:"date,omitempty"`
	NotePath       string   `json:"note_path,omitempty"`
	NoteTitle      string   `json:"note_title,omitempty"`
	ArcanaID       int      `json:"arcana_id"`
	Reversed       bool     `json:"reversed,omitempty"`
	Flipped        bool     `json:"flipped,omitempty"`
	DoubtQuestions []string `json:"doubt_questions,omitempty"`
}

// StoreData is the JSON schema written to <AppDataDir>/tarot.json.
type StoreData struct {
	Collected  map[int]CollectedEntry `json:"collected,omitempty"`
	Daily      DailyRecord            `json:"daily,omitzero"`
	NoteArcana map[string]int         `json:"note_arcana,omitempty"`
}

// Store provides thread-safe atomic persistence for Tarot collection & daily card state.
type Store struct {
	mu   sync.RWMutex
	path string
	data StoreData
}

// NewStore loads or initializes tarot.json inside dataDir.
func NewStore(dataDir string) *Store {
	p := ""
	if dataDir != "" {
		p = filepath.Join(dataDir, "tarot.json")
	}
	s := &Store{
		path: p,
		data: StoreData{
			Collected:  make(map[int]CollectedEntry),
			NoteArcana: make(map[string]int),
		},
	}
	s.load()
	return s
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var loaded StoreData
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return
	}
	if loaded.Collected == nil {
		loaded.Collected = make(map[int]CollectedEntry)
	}
	if loaded.NoteArcana == nil {
		loaded.NoteArcana = make(map[string]int)
	}
	s.mu.Lock()
	s.data = loaded
	s.mu.Unlock()
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmp)
	}
}

// Snapshot returns a defensive copy of the persisted Tarot store.
func (s *Store) Snapshot() StoreData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := StoreData{
		Collected:  make(map[int]CollectedEntry, len(s.data.Collected)),
		Daily:      s.data.Daily,
		NoteArcana: make(map[string]int, len(s.data.NoteArcana)),
	}
	for k, v := range s.data.Collected {
		cp.Collected[k] = v
	}
	for k, v := range s.data.NoteArcana {
		cp.NoteArcana[k] = v
	}
	if len(s.data.Daily.DoubtQuestions) > 0 {
		cp.Daily.DoubtQuestions = append([]string(nil), s.data.Daily.DoubtQuestions...)
	}
	return cp
}

// RecordCollected adds or refreshes an Arcana card in the Deck ("保存即收牌" / drawn card).
func (s *Store) RecordCollected(arcanaID int, notePath, noteTitle string, reversed bool, now time.Time) {
	if arcanaID < 0 || arcanaID >= len(MajorArcana) || notePath == "" {
		return
	}
	clean := filepath.Clean(notePath)
	s.mu.Lock()
	prev := s.data.Collected[arcanaID]
	s.data.Collected[arcanaID] = CollectedEntry{
		ArcanaID:  arcanaID,
		NotePath:  clean,
		NoteTitle: noteTitle,
		DrawnUnix: now.Unix(),
		Reversed:  reversed,
		Count:     prev.Count + 1,
	}
	s.data.NoteArcana[clean] = arcanaID
	s.saveLocked()
	s.mu.Unlock()
}

// MarkDailyFlipped records that today's daily card was revealed and collects it into the Deck.
func (s *Store) MarkDailyFlipped(dateStr string, note NoteCard, reversed bool, questions []string, now time.Time) {
	clean := ""
	if note.Path != "" {
		clean = filepath.Clean(note.Path)
	}
	s.mu.Lock()
	s.data.Daily = DailyRecord{
		Date:           dateStr,
		NotePath:       clean,
		NoteTitle:      note.Title,
		ArcanaID:       note.ArcanaID,
		Reversed:       reversed,
		Flipped:        true,
		DoubtQuestions: append([]string(nil), questions...),
	}
	if clean != "" {
		prev := s.data.Collected[note.ArcanaID]
		s.data.Collected[note.ArcanaID] = CollectedEntry{
			ArcanaID:  note.ArcanaID,
			NotePath:  clean,
			NoteTitle: note.Title,
			DrawnUnix: now.Unix(),
			Reversed:  reversed,
			Count:     prev.Count + 1,
		}
		s.data.NoteArcana[clean] = note.ArcanaID
	}
	s.saveLocked()
	s.mu.Unlock()
}

// SetDailyDoubtQuestions updates the cached devil's advocate questions for today's card.
func (s *Store) SetDailyDoubtQuestions(dateStr string, questions []string) {
	if len(questions) == 0 {
		return
	}
	s.mu.Lock()
	if s.data.Daily.Date == dateStr {
		s.data.Daily.DoubtQuestions = append([]string(nil), questions...)
		s.saveLocked()
	}
	s.mu.Unlock()
}

// DailyDrawnToday reports whether today's card has already been flipped.
func (s *Store) DailyDrawnToday(dateStr string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Daily.Date == dateStr && s.data.Daily.Flipped
}

// CollectedCount returns how many unique Major Arcana (0..22) are collected.
func (s *Store) CollectedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Collected)
}
