package tarot

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// Room identifies the active surface inside Tarot Mode.
// Writing is "home"; Table, Daily, and Deck are ritual rooms that always return to Writing.
type Room uint8

const (
	RoomWriting Room = iota
	RoomTable
	RoomDaily
	RoomDeck
)

// Host decouples Tarot UI & rituals from Workspace internals.
type Host struct {
	// DeferMenus, when set, queues sidebar menu overlays so the host builds
	// them after the editor (above its floating toolbars, below modals).
	DeferMenus     func(fn func())
	OpenNote       func(absPath string)
	ToggleAI       func()
	OpenAIWithMsg  func(prompt string)
	ExitTarot      func()
	ToggleSidebar  func()
	NewNote        func()
	OpenSearch     func()
	OpenSettings   func()
	Notebooks      func() []string
	SwitchNotebook func(root string)
	ChooseNotebook func()
	RequestRedraw  func()
	ReadNoteText   func(absPath string) string
	CompleteAI     func(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	IsAIReady      func() bool
	IsAIOpen       func() bool
	IsAIEnabled    func() bool
	IsAIStreaming  func() bool
}

// TableState holds the 3-card spread ritual and its animation timeline.
type TableState struct {
	Spread          Spread
	Initialized     bool
	Dealt           [3]bool
	Flipped         [3]bool
	DealAt          [3]time.Time
	CollectAt       [3]time.Time
	FlipAt          [3]time.Time
	GlintAt         [3]time.Time
	ShuffleStart    time.Time
	ShuffleEnd      time.Time
	BusyUntil       time.Time
	ReadingVisible  bool
	ReadingAt       time.Time
	AltIndex        int
	AIReading       string
	AILoading       bool
	PendingDealAt   time.Time
	PendingDealDone bool
	HoverCard       int
	PrevPaths       [3]string
	seq             uint64
}

// DailyState holds the single-card daily ritual and Devil's Advocate panel.
type DailyState struct {
	Date           string
	Card           DailyCard
	Initialized    bool
	Flipped        bool
	FlipAt         time.Time
	GlintAt        time.Time
	DoubtOpen      bool
	DoubtAt        time.Time
	AIDoubtLoading bool
	Hover          bool
}

// DeckState holds the 22 Major Arcana collection gallery state.
type DeckState struct {
	Filter      string // "all", "got", "missing"
	SelectedID  int
	TurnAt      time.Time
	GridScroll  ui.ScrollState
	DetailHover bool
}

// Controller manages Tarot Mode rooms, deck synchronization, persistence, and animations.
type Controller struct {
	mu    sync.RWMutex
	store *Store
	host  Host
	rng   *rand.Rand

	room Room

	notes      []NoteCard
	activePath string
	activeNote *NoteCard

	Table TableState
	Daily DailyState
	Deck  DeckState

	// Writing skin animations
	SaveFlashAt   time.Time
	PageSwitchAt  time.Time
	SidebarScroll ui.ScrollState

	NotebookMenuOpen   bool
	NotebookMenuAnchor ui.Rect
	NotebookMenuCursor int

	// QuickNote skin animations
	QuickSaveFlashAt time.Time
	QuickDealAt      time.Time
	QuickLastSlot    int

	// AI developing card completion glint
	AIWasStreaming bool
	AIGlintAt      time.Time

	timers []*time.Timer
}

// NewController constructs a Tarot Controller persisted under dataDir.
func NewController(dataDir string, host Host) *Controller {
	st := NewStore(dataDir)
	selected := 9 // default to Hermit if collected, else first collected or Magician
	snap := st.Snapshot()
	if _, ok := snap.Collected[selected]; !ok {
		selected = 1
		for id := 0; id < len(MajorArcana); id++ {
			if _, ok := snap.Collected[id]; ok {
				selected = id
				break
			}
		}
	}
	return &Controller{
		store: st,
		host:  host,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		room:  RoomWriting,
		Table: TableState{
			Dealt:          [3]bool{true, true, true},
			Flipped:        [3]bool{true, true, true},
			ReadingVisible: true,
			HoverCard:      -1,
		},
		Deck: DeckState{
			Filter:     "all",
			SelectedID: selected,
		},
	}
}

func (ctrl *Controller) Host() Host {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	return ctrl.host
}

func (ctrl *Controller) Store() *Store {
	return ctrl.store
}

func (ctrl *Controller) Room() Room {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	return ctrl.room
}

// SetRoom transitions between Writing, Table, Daily, and Deck rooms.
func (ctrl *Controller) SetRoom(r Room) {
	ctrl.mu.Lock()
	ctrl.room = r
	now := time.Now()
	switch r {
	case RoomTable:
		if !ctrl.Table.Initialized {
			ctrl.initSpreadLocked(now)
		} else {
			ctrl.refreshPresentCardLocked()
		}
	case RoomDaily:
		ctrl.ensureDailyLocked(now)
	case RoomDeck:
		ctrl.Deck.TurnAt = now
	}
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// BackToWriting returns from any ritual room to Writing without altering the open note.
func (ctrl *Controller) BackToWriting() {
	ctrl.SetRoom(RoomWriting)
}

// Enter activates Tarot Mode on the Writing room ("写作是家，光标不动").
func (ctrl *Controller) Enter() {
	ctrl.mu.Lock()
	ctrl.room = RoomWriting
	ctrl.ensureDailyLocked(time.Now())
	ctrl.mu.Unlock()
}

// SyncNotes refreshes the Tarot note deck from the Workspace's live FileEntries.
func (ctrl *Controller) SyncNotes(entries []model.FileEntry, activePath string, activeDirty bool) {
	snap := ctrl.store.Snapshot()
	now := time.Now()
	readText := ctrl.host.ReadNoteText
	notes := BuildDeckNotes(entries, activePath, activeDirty, snap.NoteArcana, readText, now)

	var activeCard *NoteCard
	cleanActive := ""
	if activePath != "" {
		cleanActive = filepath.Clean(activePath)
		for i := range notes {
			if notes[i].Path == cleanActive {
				cp := notes[i]
				activeCard = &cp
				break
			}
		}
	}

	ctrl.mu.Lock()
	ctrl.notes = notes
	ctrl.activePath = cleanActive
	ctrl.activeNote = activeCard
	if ctrl.Table.Initialized {
		ctrl.refreshPresentCardLocked()
	}
	today := now.Format("2006-01-02")
	if !ctrl.Daily.Initialized || ctrl.Daily.Date != today || (len(notes) > 0 && ctrl.Daily.Card.Note.Path == "") {
		ctrl.ensureDailyLocked(now)
	}
	ctrl.mu.Unlock()
}

func (ctrl *Controller) Notes() []NoteCard {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	out := make([]NoteCard, len(ctrl.notes))
	copy(out, ctrl.notes)
	return out
}

func (ctrl *Controller) ActiveNote() (NoteCard, bool) {
	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	if ctrl.activeNote == nil {
		return NoteCard{ArcanaID: 1, Title: theme.Tr("未打开笔记", "No Note Open")}, false
	}
	return *ctrl.activeNote, true
}

// OnNoteSaved implements "保存即收牌": flashes the golden corners & collects the note's Arcana.
func (ctrl *Controller) OnNoteSaved(absPath string) {
	if absPath == "" {
		return
	}
	clean := filepath.Clean(absPath)
	now := time.Now()

	ctrl.mu.Lock()
	ctrl.SaveFlashAt = now
	var card NoteCard
	found := false
	for _, n := range ctrl.notes {
		if n.Path == clean {
			card = n
			found = true
			break
		}
	}
	if !found {
		base := filepath.Base(clean)
		title := strings.TrimSuffix(base, filepath.Ext(base))
		text := ""
		if ctrl.host.ReadNoteText != nil {
			text = ctrl.host.ReadNoteText(clean)
		}
		card = NoteCard{
			Path:     clean,
			Title:    title,
			ArcanaID: AssignArcana(clean, title, text, now.Unix(), int64(len(text)), now),
		}
	}
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()

	ctrl.store.RecordCollected(card.ArcanaID, card.Path, card.Title, false, now)
	if redraw != nil {
		redraw()
	}
}

// TriggerSaveFlash flashes the writing sheet's corners even when saving an already-clean note.
func (ctrl *Controller) TriggerSaveFlash() {
	now := time.Now()
	ctrl.mu.Lock()
	ctrl.SaveFlashAt = now
	active := ctrl.activeNote
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if active != nil && active.Path != "" {
		ctrl.store.RecordCollected(active.ArcanaID, active.Path, active.Title, false, now)
	}
	if redraw != nil {
		redraw()
	}
}

// TriggerNoteSwitch starts the card-turn transition when switching notes in Tarot sidebar.
func (ctrl *Controller) TriggerNoteSwitch() {
	ctrl.mu.Lock()
	ctrl.PageSwitchAt = time.Now()
	ctrl.mu.Unlock()
}

// TriggerQuickSaveFlash flashes QuickNote's top-right gold corner and inner ring.
func (ctrl *Controller) TriggerQuickSaveFlash() {
	ctrl.mu.Lock()
	ctrl.QuickSaveFlashAt = time.Now()
	ctrl.mu.Unlock()
}

// NotifyQuickSlot triggers QuickNote's subtle deal animation when the slot changes.
func (ctrl *Controller) NotifyQuickSlot(slot int) {
	ctrl.mu.Lock()
	if ctrl.QuickLastSlot != slot {
		ctrl.QuickLastSlot = slot
		ctrl.QuickDealAt = time.Now()
	}
	ctrl.mu.Unlock()
}

// UpdateAIStreaming tracks AI streaming state transitions to trigger the completion glint.
func (ctrl *Controller) UpdateAIStreaming(streaming bool, now time.Time) (justFinishedGlint float32) {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if ctrl.AIWasStreaming && !streaming {
		ctrl.AIGlintAt = now
	}
	ctrl.AIWasStreaming = streaming
	if streaming || ctrl.AIGlintAt.IsZero() {
		return 0
	}
	el := now.Sub(ctrl.AIGlintAt).Seconds()
	if el < 0 || el > 1.3 {
		return 0
	}
	return float32(el / 1.3)
}

func (ctrl *Controller) initSpreadLocked(now time.Time) {
	sp := DrawThreeSpread(ctrl.notes, ctrl.activePath, ctrl.Table.PrevPaths, ctrl.rng)
	ctrl.Table.Spread = sp
	ctrl.Table.Initialized = true
	ctrl.Table.Dealt = [3]bool{sp.HasCards[0], sp.HasCards[1], sp.HasCards[2]}
	ctrl.Table.Flipped = [3]bool{sp.HasCards[0], sp.HasCards[1], sp.HasCards[2]}
	ctrl.Table.ReadingVisible = sp.HasCards[0] && sp.HasCards[1] && sp.HasCards[2]
	ctrl.Table.ReadingAt = now
	ctrl.Table.AltIndex = 0
	ctrl.Table.AIReading = ""
	ctrl.Table.PrevPaths = [3]string{sp.Cards[0].Note.Path, sp.Cards[1].Note.Path, sp.Cards[2].Note.Path}
	for i := 0; i < 3; i++ {
		if sp.HasCards[i] {
			ctrl.Table.DealAt[i] = now.Add(-2 * time.Second)
			ctrl.Table.FlipAt[i] = now.Add(-2 * time.Second)
		}
	}
}

func (ctrl *Controller) refreshPresentCardLocked() {
	if ctrl.activeNote == nil || !ctrl.Table.Spread.HasCards[1] {
		return
	}
	cur := ctrl.Table.Spread.Cards[1].Note
	if cur.Path == ctrl.activeNote.Path && cur.IsDirty == ctrl.activeNote.IsDirty && cur.Title == ctrl.activeNote.Title {
		return
	}
	n := *ctrl.activeNote
	ctrl.Table.Spread.Cards[1].Note = n
	orient := theme.Tr("正位", "Upright")
	if ctrl.Table.Spread.Cards[1].Reversed {
		orient = theme.Tr("逆位", "Reversed")
	}
	ctrl.Table.Spread.Cards[1].MetaText = n.When + " · " + orient
	ctrl.Table.Spread.CTATitle = n.Title
	ctrl.Table.Spread.CTAPath = n.Path
	ctrl.Table.Spread.Lines = BuildSpreadReadings(ctrl.Table.Spread)
}

func (ctrl *Controller) ensureDailyLocked(now time.Time) {
	today := now.Format("2006-01-02")
	snap := ctrl.store.Snapshot()
	dc := PickDailyCard(ctrl.notes, today, snap.Daily, ctrl.host.ReadNoteText, now)
	freshDay := !ctrl.Daily.Initialized || ctrl.Daily.Date != today
	ctrl.Daily.Date = today
	ctrl.Daily.Card = dc
	ctrl.Daily.Initialized = true
	if freshDay {
		ctrl.Daily.Flipped = snap.Daily.Date == today && snap.Daily.Flipped
		if ctrl.Daily.Flipped {
			ctrl.Daily.FlipAt = now.Add(-2 * time.Second)
		} else {
			ctrl.Daily.FlipAt = time.Time{}
		}
		ctrl.Daily.GlintAt = time.Time{}
		ctrl.Daily.DoubtOpen = false
		ctrl.Daily.DoubtAt = time.Time{}
	}
}

func (ctrl *Controller) scheduleLocked(d time.Duration, seq uint64, fn func()) {
	t := time.AfterFunc(d, func() {
		ctrl.mu.Lock()
		if ctrl.Table.seq != seq {
			ctrl.mu.Unlock()
			return
		}
		fn()
		redraw := ctrl.host.RequestRedraw
		ctrl.mu.Unlock()
		if redraw != nil {
			redraw()
		}
	})
	ctrl.timers = append(ctrl.timers, t)
}

// DrawSpread collects any dealt cards, performs the riffle shuffle, deals 3 new cards
// sequentially with 3D flip + glint sweep, and reveals the reading.
func (ctrl *Controller) DrawSpread() {
	now := time.Now()
	ctrl.mu.Lock()
	if now.Before(ctrl.Table.BusyUntil) {
		ctrl.mu.Unlock()
		return
	}
	ctrl.Table.seq++
	seq := ctrl.Table.seq
	anyDealt := ctrl.Table.Dealt[0] || ctrl.Table.Dealt[1] || ctrl.Table.Dealt[2]
	anyFlipped := ctrl.Table.Flipped[0] || ctrl.Table.Flipped[1] || ctrl.Table.Flipped[2]

	offset := time.Duration(0)
	if anyDealt {
		ctrl.Table.ReadingVisible = false
		ctrl.Table.HoverCard = -1
		if anyFlipped {
			for i := 0; i < 3; i++ {
				if ctrl.Table.Flipped[i] {
					ctrl.Table.Flipped[i] = false
					ctrl.Table.FlipAt[i] = now
				}
			}
			offset = 420 * time.Millisecond
		}
		for k, idx := range [3]int{2, 1, 0} {
			i := idx
			ctrl.scheduleLocked(offset+time.Duration(k)*100*time.Millisecond, seq, func() {
				ctrl.Table.Dealt[i] = false
				ctrl.Table.CollectAt[i] = time.Now()
			})
		}
		offset += 680 * time.Millisecond
	}

	// Riffle shuffle phase (900ms).
	shuffleAt := offset
	ctrl.scheduleLocked(shuffleAt, seq, func() {
		t := time.Now()
		ctrl.Table.ShuffleStart = t
		ctrl.Table.ShuffleEnd = t.Add(900 * time.Millisecond)
	})
	dealBase := shuffleAt + 960*time.Millisecond

	// Pick the new 3-card spread right before dealing.
	ctrl.scheduleLocked(dealBase, seq, func() {
		sp := DrawThreeSpread(ctrl.notes, ctrl.activePath, ctrl.Table.PrevPaths, ctrl.rng)
		ctrl.Table.Spread = sp
		ctrl.Table.Initialized = true
		ctrl.Table.AltIndex = 0
		ctrl.Table.AIReading = ""
		ctrl.Table.PrevPaths = [3]string{sp.Cards[0].Note.Path, sp.Cards[1].Note.Path, sp.Cards[2].Note.Path}
	})

	for i := 0; i < 3; i++ {
		idx := i
		ti := dealBase + 60*time.Millisecond + time.Duration(i)*260*time.Millisecond
		ctrl.scheduleLocked(ti, seq, func() {
			if ctrl.Table.Spread.HasCards[idx] {
				ctrl.Table.Dealt[idx] = true
				ctrl.Table.DealAt[idx] = time.Now()
			}
		})
		ctrl.scheduleLocked(ti+520*time.Millisecond, seq, func() {
			if ctrl.Table.Spread.HasCards[idx] {
				t := time.Now()
				ctrl.Table.Flipped[idx] = true
				ctrl.Table.FlipAt[idx] = t
				ctrl.Table.GlintAt[idx] = t.Add(260 * time.Millisecond)
				c := ctrl.Table.Spread.Cards[idx]
				if c.Note.Path != "" {
					ctrl.store.RecordCollected(c.Note.ArcanaID, c.Note.Path, c.Note.Title, c.Reversed, t)
				}
			}
		})
	}

	endDur := dealBase + 60*time.Millisecond + 2*260*time.Millisecond + 520*time.Millisecond + 620*time.Millisecond
	ctrl.Table.BusyUntil = now.Add(endDur)
	ctrl.scheduleLocked(endDur, seq, func() {
		ctrl.Table.ReadingVisible = true
		ctrl.Table.ReadingAt = time.Now()
		ctrl.Table.BusyUntil = time.Time{}
	})
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// ShuffleOnly collects dealt cards and performs the riffle animation leaving slots empty.
func (ctrl *Controller) ShuffleOnly() {
	now := time.Now()
	ctrl.mu.Lock()
	if now.Before(ctrl.Table.BusyUntil) {
		ctrl.mu.Unlock()
		return
	}
	ctrl.Table.seq++
	seq := ctrl.Table.seq
	anyDealt := ctrl.Table.Dealt[0] || ctrl.Table.Dealt[1] || ctrl.Table.Dealt[2]
	anyFlipped := ctrl.Table.Flipped[0] || ctrl.Table.Flipped[1] || ctrl.Table.Flipped[2]

	offset := time.Duration(0)
	if anyDealt {
		ctrl.Table.ReadingVisible = false
		ctrl.Table.HoverCard = -1
		if anyFlipped {
			for i := 0; i < 3; i++ {
				if ctrl.Table.Flipped[i] {
					ctrl.Table.Flipped[i] = false
					ctrl.Table.FlipAt[i] = now
				}
			}
			offset = 420 * time.Millisecond
		}
		for k, idx := range [3]int{2, 1, 0} {
			i := idx
			ctrl.scheduleLocked(offset+time.Duration(k)*100*time.Millisecond, seq, func() {
				ctrl.Table.Dealt[i] = false
				ctrl.Table.CollectAt[i] = time.Now()
			})
		}
		offset += 680 * time.Millisecond
	}
	ctrl.scheduleLocked(offset, seq, func() {
		t := time.Now()
		ctrl.Table.ShuffleStart = t
		ctrl.Table.ShuffleEnd = t.Add(900 * time.Millisecond)
	})
	endDur := offset + 950*time.Millisecond
	ctrl.Table.BusyUntil = now.Add(endDur)
	ctrl.scheduleLocked(endDur, seq, func() {
		ctrl.Table.BusyUntil = time.Time{}
	})
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// ToggleTableCard flips a single dealt card between face-up and face-down.
func (ctrl *Controller) ToggleTableCard(i int) {
	if i < 0 || i >= 3 {
		return
	}
	now := time.Now()
	ctrl.mu.Lock()
	if now.Before(ctrl.Table.BusyUntil) || !ctrl.Table.Dealt[i] {
		ctrl.mu.Unlock()
		return
	}
	willFlip := !ctrl.Table.Flipped[i]
	ctrl.Table.Flipped[i] = willFlip
	ctrl.Table.FlipAt[i] = now
	if willFlip {
		ctrl.Table.GlintAt[i] = now.Add(260 * time.Millisecond)
		c := ctrl.Table.Spread.Cards[i]
		if c.Note.Path != "" {
			ctrl.store.RecordCollected(c.Note.ArcanaID, c.Note.Path, c.Note.Title, c.Reversed, now)
		}
	}
	ctrl.Table.ReadingVisible = ctrl.Table.Flipped[0] && ctrl.Table.Flipped[1] && ctrl.Table.Flipped[2]
	if ctrl.Table.ReadingVisible {
		ctrl.Table.ReadingAt = now
	}
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// NextReading cycles the spread interpretation or triggers a fresh AI reading when available.
func (ctrl *Controller) NextReading() {
	now := time.Now()
	ctrl.mu.Lock()
	ctrl.Table.AltIndex++
	ctrl.Table.ReadingAt = now
	ctrl.Table.AIReading = ""
	canAI := ctrl.host.IsAIReady != nil && ctrl.host.IsAIReady() && ctrl.host.CompleteAI != nil && !ctrl.Table.AILoading
	sp := ctrl.Table.Spread
	redraw := ctrl.host.RequestRedraw
	if canAI {
		ctrl.Table.AILoading = true
	}
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
	if canAI {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			sys := "你是克制而敏锐的中文写作向导，用 2 句简练、有洞见的话把用户笔记里的三张塔罗牌（过去/现在/未来）串起来，不要使用神秘学套话，直接给出写作与取舍启发。"
			if theme.IsEn() {
				sys = "You are a restrained, insightful writing guide. In 2 concise sentences, connect the user's Past, Present, and Future note cards into one clear editorial insight."
			}
			usr := fmt.Sprintf("过去：《%s》（%s）\n现在：《%s》（%s）\n未来：《%s》（%s）",
				sp.Cards[0].Note.Title, sp.Cards[0].Note.Arcana().Name(),
				sp.Cards[1].Note.Title, sp.Cards[1].Note.Arcana().Name(),
				sp.Cards[2].Note.Title, sp.Cards[2].Note.Arcana().Name(),
			)
			ans, err := ctrl.host.CompleteAI(ctx, sys, usr)
			ctrl.mu.Lock()
			ctrl.Table.AILoading = false
			if err == nil && strings.TrimSpace(ans) != "" {
				ctrl.Table.AIReading = strings.TrimSpace(ans)
				ctrl.Table.ReadingAt = time.Now()
			}
			rd := ctrl.host.RequestRedraw
			ctrl.mu.Unlock()
			if rd != nil {
				rd()
			}
		}()
	}
}

// FlipDaily flips today's single card and records it in tarot.json.
func (ctrl *Controller) FlipDaily() {
	now := time.Now()
	ctrl.mu.Lock()
	ctrl.ensureDailyLocked(now)
	willFlip := !ctrl.Daily.Flipped
	ctrl.Daily.Flipped = willFlip
	ctrl.Daily.FlipAt = now
	if willFlip {
		ctrl.Daily.GlintAt = now.Add(300 * time.Millisecond)
		if !ctrl.store.DailyDrawnToday(ctrl.Daily.Date) {
			dc := ctrl.Daily.Card
			ctrl.store.MarkDailyFlipped(ctrl.Daily.Date, dc.Note, dc.Reversed, dc.DoubtQuestions, now)
		}
	} else {
		ctrl.Daily.DoubtOpen = false
	}
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// ToggleDailyDoubt toggles the right-hand "站在反面" Devil's Advocate panel on the Daily room.
func (ctrl *Controller) ToggleDailyDoubt() {
	now := time.Now()
	ctrl.mu.Lock()
	if !ctrl.Daily.Flipped {
		ctrl.mu.Unlock()
		return
	}
	ctrl.Daily.DoubtOpen = !ctrl.Daily.DoubtOpen
	opening := ctrl.Daily.DoubtOpen
	if opening {
		ctrl.Daily.DoubtAt = now
	}
	canAI := opening && ctrl.host.IsAIReady != nil && ctrl.host.IsAIReady() && ctrl.host.CompleteAI != nil && !ctrl.Daily.AIDoubtLoading
	dc := ctrl.Daily.Card
	dateStr := ctrl.Daily.Date
	readText := ctrl.host.ReadNoteText
	if canAI {
		ctrl.Daily.AIDoubtLoading = true
	}
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
	if canAI && dc.Note.Path != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			body := ""
			if readText != nil {
				body = readText(dc.Note.Path)
			}
			if len([]rune(body)) > 1200 {
				body = string([]rune(body)[:1200])
			}
			sys := "你是锋利而克制的批评者。针对用户的笔记，站在反面提出恰好 3 个简短具体的反问，每行一个问题，不要序号、不要寒暄。"
			if theme.IsEn() {
				sys = "You are a sharp, restrained devil's advocate. Return exactly 3 concise counter-questions about the user's note, one per line, no numbering or preamble."
			}
			ans, err := ctrl.host.CompleteAI(ctx, sys, fmt.Sprintf("标题：%s\n内容：\n%s", dc.Note.Title, body))
			var qs []string
			if err == nil {
				for _, line := range strings.Split(ans, "\n") {
					s := strings.TrimSpace(strings.TrimLeft(line, "0123456789.-•、I "))
					if s != "" {
						qs = append(qs, s)
					}
					if len(qs) == 3 {
						break
					}
				}
			}
			ctrl.mu.Lock()
			ctrl.Daily.AIDoubtLoading = false
			if len(qs) == 3 && ctrl.Daily.Date == dateStr {
				ctrl.Daily.Card.DoubtQuestions = qs
				ctrl.store.SetDailyDoubtQuestions(dateStr, qs)
			}
			rd := ctrl.host.RequestRedraw
			ctrl.mu.Unlock()
			if rd != nil {
				rd()
			}
		}()
	}
}

// OpenNoteFromRitual opens absPath in Block's editor and returns to Writing room.
func (ctrl *Controller) OpenNoteFromRitual(absPath string, doubtPrompt bool) {
	ctrl.mu.Lock()
	openNote := ctrl.host.OpenNote
	openAI := ctrl.host.OpenAIWithMsg
	dc := ctrl.Daily.Card
	ctrl.room = RoomWriting
	ctrl.PageSwitchAt = time.Now()
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()

	if absPath != "" && openNote != nil {
		openNote(absPath)
	}
	if doubtPrompt && openAI != nil {
		var b strings.Builder
		if theme.IsEn() {
			fmt.Fprintf(&b, "Play devil's advocate on \"%s\" around these 3 questions and help me challenge or sharpen my argument:\n", dc.Note.Title)
		} else {
			fmt.Fprintf(&b, "请站在反面帮我审视《%s》，围绕这三个反问和我展开讨论并给出可写进笔记的补充观点：\n", dc.Note.Title)
		}
		for i, q := range dc.DoubtQuestions {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
		openAI(strings.TrimSpace(b.String()))
	}
	if redraw != nil {
		redraw()
	}
}

// SelectDeckCard updates the selected card in the 22-card Deck gallery with a turn animation.
func (ctrl *Controller) SelectDeckCard(id int) {
	if id < 0 || id >= len(MajorArcana) {
		return
	}
	ctrl.mu.Lock()
	ctrl.Deck.SelectedID = id
	ctrl.Deck.TurnAt = time.Now()
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// SetDeckFilter switches between "all", "got", and "missing" in the Deck room.
func (ctrl *Controller) SetDeckFilter(f string) {
	ctrl.mu.Lock()
	ctrl.Deck.Filter = f
	redraw := ctrl.host.RequestRedraw
	ctrl.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// DailySummary returns today's card and whether it has been revealed yet.
func (ctrl *Controller) DailySummary() (DailyCard, bool) {
	now := time.Now()
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	today := now.Format("2006-01-02")
	if !ctrl.Daily.Initialized || ctrl.Daily.Date != today {
		ctrl.ensureDailyLocked(now)
	}
	return ctrl.Daily.Card, ctrl.Daily.Flipped || ctrl.store.DailyDrawnToday(today)
}
