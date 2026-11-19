package app

import (
	"path/filepath"
	"strings"

	"block/internal/files"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
	codeed "github.com/egoist/mygo/ui/editor"
)

// sourceSoftWrap mirrors AppState.SourceSoftWrap (persisted) for the source editor
// (titlebar "Wrap" button). Off by default: long lines scroll sideways.
var sourceSoftWrap bool

// SourceSoftWrap reports whether the source editor wraps long lines.
func SourceSoftWrap() bool { return sourceSoftWrap }

// ToggleSourceSoftWrap flips the source editor's soft wrap.
func ToggleSourceSoftWrap() { sourceSoftWrap = !sourceSoftWrap }

var (
	sourceThemeCache     *codeed.Theme
	sourceThemeCacheDark bool
	sourceThemeCacheSet  bool
)

// sourceEditorTheme maps the Ocean Paper palette onto the code editor theme.
func sourceEditorTheme(pal theme.Palette) *codeed.Theme {
	if sourceThemeCacheSet && sourceThemeCacheDark == pal.Dark && sourceThemeCache != nil {
		return sourceThemeCache
	}
	ct := &codeed.Theme{
		Background:        pal.Paper,
		Text:              pal.Text,
		LineNumber:        pal.GutterText,
		ActiveLineNumber:  pal.Text,
		ActiveLine:        pal.CodeLine,
		Selection:         pal.SelectionBG,
		InactiveSelection: pal.SelectionBG.Alpha(0.55),
		Cursor:            pal.Accent,
		Match:             pal.FindMatch,
		CurrentMatch:      pal.FindActive,
		IndentGuide:       pal.Hairline,
		ActiveIndentGuide: pal.BtnBorder,
		WrapGuide:         pal.Hairline,
		Error:             pal.Danger,
		Warning:           pal.Warning,
		Info:              pal.Accent,
		Added:             pal.Success,
		Deleted:           pal.Danger,
		Modified:          pal.Accent,
	}
	set := func(c ui.Color, kinds ...codeed.TokenKind) {
		for _, k := range kinds {
			ct.Tokens[k].Color = c
		}
	}
	set(pal.SyntaxKeyword, codeed.TokenKeyword, codeed.TokenOperator)
	set(pal.SyntaxType, codeed.TokenType)
	set(pal.SyntaxFunction, codeed.TokenFunction, codeed.TokenMeta)
	set(pal.SyntaxKey, codeed.TokenProperty, codeed.TokenAttribute, codeed.TokenHeading)
	set(pal.SyntaxLiteral, codeed.TokenConstant)
	set(pal.SyntaxNumber, codeed.TokenNumber)
	set(pal.SyntaxString, codeed.TokenString, codeed.TokenEscape, codeed.TokenRegexp, codeed.TokenLink, codeed.TokenCode)
	set(pal.SyntaxTag, codeed.TokenTag)
	set(pal.SyntaxComment, codeed.TokenComment)
	set(pal.DiffAddFG, codeed.TokenInserted)
	set(pal.DiffDelFG, codeed.TokenDeleted)
	set(pal.Danger, codeed.TokenInvalid)
	ct.Tokens[codeed.TokenComment].Italic = false
	ct.Tokens[codeed.TokenHeading].Bold = true
	ct.Tokens[codeed.TokenStrong].Bold = true
	ct.Tokens[codeed.TokenEmphasis].Italic = true
	sourceThemeCache, sourceThemeCacheDark, sourceThemeCacheSet = ct, pal.Dark, true
	return ct
}

// ensureCodeState creates the editor state from the surface's source text once.
func (s *TabSurface) ensureCodeState() *codeed.State {
	if s.code != nil {
		return s.code
	}
	buf := codeed.NewBuffer(s.Source.Text)
	buf.SetLanguage(codeed.LanguageForText(filepath.Base(s.Path), s.Source.Text))
	s.code = &codeed.State{Buffer: buf, Label: filepath.Base(s.Path)}
	s.codeVersion = buf.Version()
	return s.code
}

// renderSourceEditor shows non-Markdown text as a full-bleed code editor (Zed/VS Code
// style): gutter flush left, current-line band, syntax colors, no paper margins.
func (s *TabSurface) renderSourceEditor(c *ui.Context) {
	pal := theme.Ocean()
	st := s.ensureCodeState()

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
	st.SoftWrap = sourceSoftWrap
	st.Theme = sourceEditorTheme(pal)

	c.Key("source-surface:" + s.Path)
	outer := ui.Column(c).Fill().Background(pal.Paper)
	blockui.Probe(outer, "source-surface")
	outer.Children(func() {
		c.Key("source-editor:" + s.Path)
		ed := codeed.View(c, st).Fill()
		blockui.Probe(ed, "source-editor")
	})

	if v := st.Buffer.Version(); v != s.codeVersion {
		s.codeVersion = v
		var sb strings.Builder
		_, _ = st.Buffer.WriteTo(&sb)
		s.Source.Text = sb.String()
		if s.IsSVG {
			s.reparseSVG()
		}
		_ = s.saveSource()
	}
}

func (s *TabSurface) saveSource() error {
	if !s.IsDirty() {
		return nil
	}
	err := files.CheckTextVersion(s.Path, files.TextFingerprint(s.SavedText, s.Source.BOM))
	if err == nil {
		_, err = files.WriteRawFileAtomic(s.Path, s.Source.Text, s.Source.BOM)
	}
	if err != nil {
		s.StatusMsg = err.Error()
		return err
	}
	s.SavedText = s.Source.Text
	if s.code != nil {
		s.code.Buffer.MarkSaved()
	}
	s.StatusMsg = ""
	return nil
}
