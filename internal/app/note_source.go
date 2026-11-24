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

// noteSourceView is the read-only Markdown source view of one note tab,
// toggled from the titlebar "more" menu.
type noteSourceView struct {
	tab string
	st  *codeed.State
}

func (w *Workspace) noteSourceActive(tab string) bool {
	return tab != "" && w.noteSource.st != nil && w.noteSource.tab == tab
}

func (w *Workspace) openNoteSourceFind() bool {
	if w.noteSource.st == nil {
		return false
	}
	return w.noteSource.st.Run(codeed.Find)
}

func (w *Workspace) toggleNoteSource(tab string, sess *editor.Session) {
	if w.noteSourceActive(tab) || sess == nil {
		w.noteSource = noteSourceView{}
		return
	}
	text := sess.Markdown()
	buf := codeed.NewBuffer(text)
	buf.SetLanguage(codeed.LanguageForText(filepath.Base(tab), text))
	w.noteSource = noteSourceView{
		tab: tab,
		st:  &codeed.State{Buffer: buf, Label: filepath.Base(tab), ReadOnly: true},
	}
}

func (w *Workspace) renderNoteSource(c *ui.Context) {
	pal := theme.Ocean()
	st := w.noteSource.st
	_, codeFont, _ := theme.Typography()
	if strings.TrimSpace(codeFont) == "" {
		codeFont = "monospace"
	}
	z := theme.Zoom()
	if z <= 0 {
		z = 1.0
	}
	st.Font = ui.Font{Family: codeFont, Size: 13 * z}
	st.LineHeight = 1.54
	st.SoftWrap = true
	st.Theme = sourceEditorTheme(pal)
	st.Bindings = sourceFindBindings(st.Bindings)
	st.RenderFindBar = renderSourceFindBar

	c.Key("note-source:" + w.noteSource.tab)
	outer := ui.Column(c).Fill().Background(pal.Paper)
	blockui.Probe(outer, "note-source")
	outer.Children(func() {
		c.Key("note-source-editor:" + w.noteSource.tab)
		codeed.View(c, st).Fill()
	})
}
