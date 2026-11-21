package app

import (
	"path/filepath"
	"strings"

	"block/internal/files"
	"block/internal/plugins"
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

func resolveSourceLanguage(filePath, text string) (*codeed.Language, string) {
	if lang, tag := plugins.ResolveCodeLanguageForFile(filePath); lang != nil {
		if tag == "plist" {
			lang = plistSourceLanguage(lang, text)
		}
		return lang, tag
	}
	lang := codeed.LanguageForText(filepath.Base(filePath), text)
	tag := files.LanguageTagForPath(filePath)
	if tag == "plaintext" && lang != nil {
		tag = sourceTagForLanguage(lang.Name)
	}
	return lang, tag
}

// plistSourceLanguage preserves the highlighter and uses the source format's comment delimiters.
func plistSourceLanguage(lang *codeed.Language, text string) *codeed.Language {
	line := "//"
	block := [2]string{"/*", "*/"}
	if isXMLPlistSource(text) {
		line = ""
		block = [2]string{"<!--", "-->"}
	}
	if lang.LineComment == line && lang.BlockComment == block {
		return lang
	}
	configured := *lang
	configured.LineComment = line
	configured.BlockComment = block
	return &configured
}

func isXMLPlistSource(text string) bool {
	markup, ok := strings.CutPrefix(strings.TrimSpace(text), "<")
	if !ok {
		return false
	}
	if strings.HasPrefix(markup, "?") || strings.HasPrefix(markup, "!") {
		return true
	}
	if end := strings.IndexAny(markup, " \t\r\n/>"); end >= 0 {
		markup = markup[:end]
	}
	switch markup {
	case "plist", "array", "dict", "key", "string", "integer", "real", "date", "data", "true", "false":
		return true
	default:
		return false
	}
}

// sourceTagForLanguage turns a MyGo language name into the fence tag Markdown
// code blocks use, e.g. "C++" -> "cpp".
func sourceTagForLanguage(name string) string {
	switch n := strings.ToLower(name); n {
	case "c++":
		return "cpp"
	case "c#":
		return "csharp"
	case "shell":
		return "bash"
	default:
		return n
	}
}

// ensureCodeState creates the editor state from the surface's source text once
// and refreshes its syntax language whenever enabled language plugins change.
func (s *TabSurface) ensureCodeState() *codeed.State {
	langGen := plugins.LanguageGeneration()
	if s.code != nil {
		if s.codeLangGen != langGen {
			lang, tag := resolveSourceLanguage(s.Path, s.Source.Text)
			s.code.Buffer.SetLanguage(lang)
			if tag != "" {
				s.Source.Language = tag
			}
			s.codeLangGen = langGen
		}
		return s.code
	}
	buf := codeed.NewBuffer(s.Source.Text)
	lang, tag := resolveSourceLanguage(s.Path, s.Source.Text)
	buf.SetLanguage(lang)
	if tag != "" {
		s.Source.Language = tag
	}
	s.code = &codeed.State{Buffer: buf, Label: filepath.Base(s.Path)}
	s.codeVersion = buf.Version()
	s.codeLangGen = langGen
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
		if s.Source.Language == "plist" {
			st.Buffer.SetLanguage(plistSourceLanguage(st.Buffer.Language(), s.Source.Text))
		}
		if s.IsSVG {
			s.reparseSVG()
		}
		_ = s.saveSource()
	}
}

func (s *TabSurface) saveSource() error {
	if !s.IsDirty() {
		s.StatusMsg = ""
		return nil
	}
	expectedFP := s.DiskFingerprint
	if expectedFP == 0 {
		expectedFP = files.TextFingerprint(s.SavedText, s.Source.BOM)
	}
	if s.Source.BinaryPlist {
		encoded, err := files.EncodeBinaryPlist(s.Source.Text)
		if err != nil {
			msg := err.Error()
			if s.StatusMsg != msg {
				files.LogErrorf("binary plist encode failed: path=%s err=%v", s.Path, err)
				s.StatusMsg = msg
			}
			return err
		}
		if err := files.CheckTextVersion(s.Path, expectedFP); err != nil {
			files.LogErrorf("source save failed: path=%s err=%v", s.Path, err)
			s.StatusMsg = err.Error()
			return err
		}
		newFP, err := files.WriteBytesAtomic(s.Path, encoded)
		if err != nil {
			files.LogErrorf("source save failed: path=%s err=%v", s.Path, err)
			s.StatusMsg = err.Error()
			return err
		}
		s.SavedText = s.Source.Text
		s.DiskFingerprint = newFP
		s.Source.DiskFingerprint = newFP
		s.FileSize = int64(len(encoded))
		if s.code != nil {
			s.code.Buffer.MarkSaved()
		}
		s.StatusMsg = ""
		return nil
	}
	err := files.CheckTextVersion(s.Path, expectedFP)
	var newFP int64
	if err == nil {
		newFP, err = files.WriteRawFileAtomic(s.Path, s.Source.Text, s.Source.BOM)
	}
	if err != nil {
		files.LogErrorf("source save failed: path=%s err=%v", s.Path, err)
		s.StatusMsg = err.Error()
		return err
	}
	s.SavedText = s.Source.Text
	s.DiskFingerprint = newFP
	s.Source.DiskFingerprint = newFP
	size := int64(len(s.Source.Text))
	if s.Source.BOM {
		size += 3
	}
	s.FileSize = size
	if s.code != nil {
		s.code.Buffer.MarkSaved()
	}
	s.StatusMsg = ""
	return nil
}
