package app

import (
	"path/filepath"
	"strings"

	"block/internal/editor"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// noteSourceView holds the editable Markdown source state of one note tab,
// toggled from the titlebar "more" menu.
type noteSourceView struct {
	tab          string
	st           *codeed.State
	codeVersion  uint64
	text         string
	savedText    string
	dirty        bool
	recordedUndo bool
	docRev       uint64
	focusPending bool
	scrollbars   blockui.EditorScrollbars
	tabArrows    blockui.EditorTabWhitespace
	findOpen     bool
}

func (w *Workspace) noteSourceActiveLocked(tab string) bool {
	return tab != "" && w.noteSource.st != nil && w.noteSource.tab == tab
}

func (w *Workspace) noteSourceActive(tab string) bool {
	w.mu.RLock()
	active := w.noteSourceActiveLocked(tab)
	w.mu.RUnlock()
	return active
}

func (w *Workspace) noteSourceDirtyLocked(tab string) bool {
	return w.noteSourceActiveLocked(tab) && w.noteSource.dirty && !w.virtualPreview[tab]
}

func (w *Workspace) noteSourceDirty(tab string) bool {
	w.mu.RLock()
	dirty := w.noteSourceDirtyLocked(tab)
	w.mu.RUnlock()
	return dirty
}

func (w *Workspace) noteSourceCurrentText() string {
	w.mu.RLock()
	st := w.noteSource.st
	text := w.noteSource.text
	ver := w.noteSource.codeVersion
	w.mu.RUnlock()
	if st != nil && st.Buffer != nil && st.Buffer.Version() != ver {
		var sb strings.Builder
		_, _ = st.Buffer.WriteTo(&sb)
		return sb.String()
	}
	return text
}

func (w *Workspace) syncNoteSourceBuffer() {
	w.mu.RLock()
	tab := w.noteSource.tab
	st := w.noteSource.st
	ver := w.noteSource.codeVersion
	savedText := w.noteSource.savedText
	recordedUndo := w.noteSource.recordedUndo
	isVirtual := w.virtualPreview[tab]
	sess := w.sessions[tab]
	w.mu.RUnlock()

	if tab == "" || st == nil || st.Buffer == nil {
		return
	}
	curVer := st.Buffer.Version()
	if curVer == ver {
		return
	}
	var sb strings.Builder
	_, _ = st.Buffer.WriteTo(&sb)
	nextText := sb.String()
	dirty := !isVirtual && nextText != savedText

	var nextDocRev uint64
	if sess != nil {
		doc := w.codec.Parse(nextText)
		sess.ApplySourceDoc(doc, !recordedUndo, dirty)
		nextDocRev = sess.DocRev()
	}

	w.mu.Lock()
	if w.noteSourceActiveLocked(tab) && w.noteSource.st == st {
		w.noteSource.codeVersion = curVer
		w.noteSource.text = nextText
		w.noteSource.dirty = dirty
		if sess != nil {
			w.noteSource.recordedUndo = true
			w.noteSource.docRev = nextDocRev
		}
	}
	w.mu.Unlock()
}

func (w *Workspace) markNoteSourceSaved(tab, savedText string) {
	w.mu.Lock()
	if w.noteSourceActiveLocked(tab) {
		w.noteSource.savedText = savedText
		w.noteSource.dirty = !w.virtualPreview[tab] && w.noteSource.text != savedText
		if !w.noteSource.dirty && w.noteSource.st != nil && w.noteSource.st.Buffer != nil &&
			w.noteSource.st.Buffer.Version() == w.noteSource.codeVersion {
			w.noteSource.st.Buffer.MarkSaved()
		}
	}
	w.mu.Unlock()
}

func (w *Workspace) openNoteSourceFind(replace bool) bool {
	w.mu.RLock()
	st := w.noteSource.st
	w.mu.RUnlock()
	if st == nil {
		return false
	}
	opened := false
	if replace && !st.ReadOnly {
		opened = st.Run(codeed.FindReplace)
	} else {
		opened = st.Run(codeed.Find)
	}
	if opened {
		w.mu.Lock()
		if w.noteSource.st == st {
			w.noteSource.findOpen = true
		}
		w.mu.Unlock()
	}
	return opened
}

func (w *Workspace) toggleNoteSource(tab string, sess *editor.Session) {
	if w.noteSourceActive(tab) || sess == nil {
		if w.noteSourceActive(tab) {
			w.syncNoteSourceBuffer()
			if err := w.flushSingleSession(tab); err != nil {
				w.setNotice(err.Error())
			} else {
				w.mu.Lock()
				delete(w.lastDiskMarkdown, filepath.Clean(tab))
				w.mu.Unlock()
			}
		}
		w.mu.Lock()
		w.noteSource = noteSourceView{}
		w.mu.Unlock()
		if sess != nil {
			sess.FocusBlock(0)
		}
		w.RequestRedraw()
		return
	}

	w.mu.RLock()
	prevSourceTab := w.noteSource.tab
	hasPrevSource := w.noteSource.st != nil && prevSourceTab != "" && prevSourceTab != tab
	isVirtual := w.virtualPreview[tab]
	lastDiskMD, hasLastDisk := w.lastDiskMarkdown[filepath.Clean(tab)]
	w.mu.RUnlock()

	if hasPrevSource {
		w.syncNoteSourceBuffer()
		if err := w.flushSingleSession(prevSourceTab); err != nil {
			w.setNotice(err.Error())
		} else {
			w.mu.Lock()
			delete(w.lastDiskMarkdown, filepath.Clean(prevSourceTab))
			w.mu.Unlock()
		}
	}

	text := sess.Markdown()
	savedText := text
	sessDirty := sess.Dirty()
	if !sessDirty && !isVirtual {
		if diskText, _, err := w.readTabDiskNote(tab); err == nil {
			text = diskText
			savedText = diskText
		}
	} else if sessDirty && !isVirtual {
		if hasLastDisk {
			savedText = lastDiskMD
		} else if diskText, _, err := w.readTabDiskNote(tab); err == nil {
			savedText = diskText
		} else {
			savedText = ""
		}
	}

	buf := codeed.NewBuffer(text)
	buf.SetLanguage(codeed.LanguageForText(filepath.Base(tab), text))
	st := &codeed.State{
		Buffer:   buf,
		Label:    filepath.Base(tab),
		ReadOnly: isVirtual,
	}
	sess.ClearFocus()
	w.mu.Lock()
	w.noteSource = noteSourceView{
		tab:          tab,
		st:           st,
		codeVersion:  buf.Version(),
		text:         text,
		savedText:    savedText,
		dirty:        !isVirtual && (sessDirty || text != savedText),
		docRev:       sess.DocRev(),
		focusPending: true,
	}
	w.mu.Unlock()
	w.RequestRedraw()
}

func (w *Workspace) renderNoteSource(c *ui.Context, sess *editor.Session) {
	pal := theme.Ocean()
	w.mu.RLock()
	tab := w.noteSource.tab
	st := w.noteSource.st
	nsDocRev := w.noteSource.docRev
	nsText := w.noteSource.text
	nsSavedText := w.noteSource.savedText
	findOpen := w.noteSource.findOpen
	isVirtual := w.virtualPreview[tab]
	w.mu.RUnlock()
	if st == nil {
		return
	}

	if sess != nil && sess.DocRev() != nsDocRev {
		newText := sess.Markdown()
		sessDirty := sess.Dirty()
		if !sessDirty && !isVirtual {
			if diskText, _, err := w.readTabDiskNote(tab); err == nil {
				newText = diskText
			}
		}
		if newText != nsText || st.Buffer.String() != newText {
			st.Buffer.Reset(newText)
			st.Buffer.SetLanguage(codeed.LanguageForText(filepath.Base(tab), newText))
		}
		savedText := newText
		if sessDirty {
			savedText = nsSavedText
		}
		w.mu.Lock()
		if w.noteSourceActiveLocked(tab) && w.noteSource.st == st {
			w.noteSource.codeVersion = st.Buffer.Version()
			w.noteSource.text = newText
			w.noteSource.savedText = savedText
			w.noteSource.dirty = !isVirtual && sessDirty
			w.noteSource.recordedUndo = false
			w.noteSource.docRev = sess.DocRev()
		}
		w.mu.Unlock()
	}

	z := theme.Zoom()
	if z <= 0 {
		z = 1.0
	}
	st.ReadOnly = isVirtual
	st.Font = ui.Font{Family: sourceCodeFont(), Size: 13 * z}
	st.LineHeight = 1.54
	st.SoftWrap = true
	st.Theme = sourceEditorTheme(pal)
	findRendered := false
	var findBarBounds ui.Rect
	st.Bindings = sourceFindBindings(st.Bindings)
	st.RenderFindBar = func(ctx *ui.Context, bar ui.Element, f *codeed.FindBar) {
		findRendered = true
		findBarBounds = bar.Bounds()
		renderSourceFindBar(ctx, bar, f)
	}

	right, bottom := w.noteSource.scrollbars.Prepare(c, st, findOpen)
	w.noteSource.tabArrows.Prepare(c, st)
	c.Key("note-source:" + tab)
	outer := ui.Column(c).Fill().Background(pal.Paper).Padding(0, right, bottom, 0)
	blockui.Probe(outer, "note-source")
	var ed ui.Element
	outer.Children(func() {
		c.Key("note-source-editor-wrap:" + tab)
		edWrap := ui.Box(c).Fill()
		blockui.Probe(edWrap, "note-source-editor")
		edWrap.Children(func() {
			c.Key("note-source-editor:" + tab)
			ed = codeed.View(c, st).Fill()
		})
		w.mu.Lock()
		shouldFocus := w.noteSourceActiveLocked(tab) && w.noteSource.st == st && w.noteSource.focusPending
		if shouldFocus {
			w.noteSource.focusPending = false
		}
		w.mu.Unlock()
		if shouldFocus {
			ed.Focus()
		}
	})
	if findRendered != findOpen {
		w.mu.Lock()
		if w.noteSourceActiveLocked(tab) && w.noteSource.st == st {
			w.noteSource.findOpen = findRendered
		}
		w.mu.Unlock()
		c.Invalidate()
	}
	w.noteSource.scrollbars.Observe(c, outer, ed, st)
	w.noteSource.tabArrows.Observe(c, ed, st, findBarBounds)

	w.mu.RLock()
	prevVer := w.noteSource.codeVersion
	stillActive := w.noteSourceActiveLocked(tab) && w.noteSource.st == st
	w.mu.RUnlock()
	if stillActive && st.Buffer.Version() != prevVer {
		w.syncNoteSourceBuffer()
		w.flushOrScheduleDirtySessions()
	}
}
