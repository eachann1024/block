package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"block/internal/editor"
	"block/internal/files"
	"block/internal/model"
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
	if lang, tag := plugins.ResolvePluginCodeLanguageForFile(filePath); lang != nil {
		if tag == "plist" {
			lang = plistSourceLanguage(lang, text)
		}
		return lang, tag
	}
	if lang, tag := resolveBuiltinConfigByPathOrContent(filePath, text); lang != nil {
		return lang, tag
	}
	if lang, tag := plugins.ResolveBuiltinCodeLanguageForFile(filePath); lang != nil {
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

// resolveBuiltinConfigByPathOrContent handles ambiguous config filenames
// (such as .conf, ~/.ssh/config, ghostty/config, .git/config) and extra
// YAML/JSON/Shell config extensions that MyGo does not list directly.
func resolveBuiltinConfigByPathOrContent(filePath, text string) (*codeed.Language, string) {
	norm := strings.ToLower(filepath.ToSlash(filePath))
	base := strings.ToLower(filepath.Base(filePath))
	ext := strings.ToLower(filepath.Ext(base))

	switch base {
	case ".tmux.conf", "tmux.conf", ".zprofile", ".zshenv", ".zlogin", ".bash_aliases", ".bash_logout":
		if l := codeed.LanguageNamed("shell"); l != nil {
			return l, "bash"
		}
	case ".clang-tidy":
		if l := codeed.LanguageNamed("yaml"); l != nil {
			return l, "yaml"
		}
	case ".prettierrc", "jsconfig.json", "deno.jsonc":
		if l := codeed.LanguageNamed("json"); l != nil {
			return l, "json"
		}
	}

	switch ext {
	case ".stoverride":
		if l := codeed.LanguageNamed("yaml"); l != nil {
			return l, "yaml"
		}
	case ".jsonl", ".ndjson":
		if l := codeed.LanguageNamed("json"); l != nil {
			return l, "json"
		}
	case ".ps1xml":
		if l := codeed.LanguageNamed("xml"); l != nil {
			return l, "xml"
		}
	}

	switch {
	case strings.HasSuffix(norm, "/.ssh/config") || strings.Contains(norm, "/.ssh/config.d/"):
		return plugins.ResolveCodeLanguageByTag("sshconfig")
	case strings.HasSuffix(norm, "/ghostty/config") || strings.HasSuffix(norm, "/git/config") || strings.HasSuffix(norm, "/.git/config"):
		return plugins.ResolveCodeLanguageByTag("ini")
	case strings.HasSuffix(norm, "/git/ignore"):
		return plugins.ResolveCodeLanguageByTag("ignore")
	case strings.HasPrefix(base, "procfile."):
		return plugins.ResolveCodeLanguageByTag("procfile")
	case strings.HasPrefix(base, "jenkinsfile."):
		return plugins.ResolveCodeLanguageByTag("groovy")
	case isRequirementsFilename(base, ext):
		return plugins.ResolveCodeLanguageByTag("requirements")
	case (strings.Contains(norm, "/cron.d/") || strings.HasPrefix(norm, "cron.d/")) && ext == "":
		return plugins.ResolveCodeLanguageByTag("crontab")
	}

	if ext == ".conf" {
		// Exact built-in filenames (e.g. nginx.conf, httpd.conf, haproxy.conf, pip.conf, Surge.conf) take precedence.
		if lang, tag := plugins.ResolveBuiltinCodeLanguageForFile(filePath); lang != nil {
			return lang, tag
		}
		if isSurgeConfSource(text) {
			return plugins.ResolveCodeLanguageByTag("surge")
		}
		if isNginxConfSource(text) {
			return plugins.ResolveCodeLanguageByTag("nginx")
		}
		if isApacheConfSource(text) {
			return plugins.ResolveCodeLanguageByTag("apache")
		}
		if isHAProxyConfSource(text) {
			return plugins.ResolveCodeLanguageByTag("haproxy")
		}
		return plugins.ResolveCodeLanguageByTag("ini")
	}
	return nil, ""
}

func isRequirementsFilename(base, ext string) bool {
	if ext != ".txt" && ext != ".in" {
		return false
	}
	stem := strings.TrimSuffix(base, ext)
	if stem == "requirements" || stem == "constraints" {
		return true
	}
	for _, prefix := range []string{"requirements-", "requirements.", "constraints-", "constraints."} {
		if strings.HasPrefix(stem, prefix) {
			return true
		}
	}
	return false
}

func isSurgeConfSource(text string) bool {
	const maxScan = 8192
	sample := text
	if len(sample) > maxScan {
		sample = sample[:maxScan]
	}
	for line := range strings.SplitSeq(sample, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#!name=") || strings.HasPrefix(t, "#!desc=") || strings.HasPrefix(t, "#!category=") {
			return true
		}
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			sec := strings.ToLower(strings.TrimSpace(t[1 : len(t)-1]))
			switch sec {
			case "general", "proxy", "proxy group", "rule", "url rewrite", "header rewrite",
				"body rewrite", "mitm", "script", "host", "panel", "map local", "replica",
				"ssid setting", "rewrite", "filter_local", "rewrite_local", "server_local", "task_local":
				return true
			}
			if strings.HasPrefix(sec, "wireguard ") {
				return true
			}
		}
		for _, prefix := range []string{"DOMAIN-SUFFIX,", "DOMAIN-KEYWORD,", "DOMAIN,", "IP-CIDR,", "RULE-SET,", "GEOIP,", "FINAL,"} {
			if strings.HasPrefix(t, prefix) {
				return true
			}
		}
	}
	return false
}

func isNginxConfSource(text string) bool {
	const maxScan = 8192
	sample := text
	if len(sample) > maxScan {
		sample = sample[:maxScan]
	}
	for line := range strings.SplitSeq(sample, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasSuffix(t, "{") {
			for _, kw := range []string{"server", "http", "events", "upstream", "location", "stream"} {
				if t == kw+"{" || strings.HasPrefix(t, kw+" ") || strings.HasPrefix(t, kw+"\t") {
					return true
				}
			}
		}
		if strings.HasSuffix(t, ";") {
			for _, dir := range []string{"proxy_pass ", "server_name ", "listen ", "fastcgi_pass ", "try_files ", "ssl_certificate "} {
				if strings.HasPrefix(t, dir) {
					return true
				}
			}
		}
	}
	return false
}

func isApacheConfSource(text string) bool {
	const maxScan = 8192
	sample := text
	if len(sample) > maxScan {
		sample = sample[:maxScan]
	}
	for line := range strings.SplitSeq(sample, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		low := strings.ToLower(t)
		for _, tag := range []string{"<virtualhost", "<directory", "<location", "<files", "<ifmodule", "<proxy"} {
			if strings.HasPrefix(low, tag) {
				return true
			}
		}
		for _, dir := range []string{"rewriteengine ", "rewriterule ", "rewritecond ", "documentroot ", "proxypass ", "allowoverride "} {
			if strings.HasPrefix(low, dir) {
				return true
			}
		}
	}
	return false
}

func isHAProxyConfSource(text string) bool {
	const maxScan = 8192
	sample := text
	if len(sample) > maxScan {
		sample = sample[:maxScan]
	}
	sectionSeen := false
	for line := range strings.SplitSeq(sample, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		low := strings.ToLower(t)
		if low == "global" || low == "defaults" ||
			strings.HasPrefix(low, "frontend ") || strings.HasPrefix(low, "backend ") || strings.HasPrefix(low, "listen ") {
			sectionSeen = true
			continue
		}
		if sectionSeen {
			for _, dir := range []string{"bind ", "default_backend ", "use_backend ", "balance ", "server ", "option ", "mode http", "mode tcp"} {
				if strings.HasPrefix(low, dir) {
					return true
				}
			}
		}
	}
	return false
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

// JumpToLine moves the source editor caret to a 1-based line (and optional 1-based rune column)
// and schedules centering the viewport on that line.
func (s *TabSurface) JumpToLine(line, col int) {
	if s == nil || !s.HasSource || s.Kind != model.OpenKindSource || line <= 0 {
		return
	}
	st := s.ensureCodeState()
	if st == nil || st.Buffer == nil {
		return
	}
	snap := st.Buffer.Snapshot()
	lineCount := snap.LineCount()
	if lineCount <= 0 {
		return
	}
	line0 := line - 1
	if line0 >= lineCount {
		line0 = lineCount - 1
	}
	byteCol := 0
	if col > 1 {
		lineText := snap.Line(line0)
		runeIdx := 0
		found := false
		for off := range lineText {
			if runeIdx == col-1 {
				byteCol = off
				found = true
				break
			}
			runeIdx++
		}
		if !found {
			byteCol = len(lineText)
		}
	}
	st.GoTo(codeed.Pos{Line: line0, Col: byteCol})
}

func sourceFindBindings(bindings []codeed.Binding) []codeed.Binding {
	mods, key, ok := blockui.ParseShortcut(editor.FindShortcut())
	if !ok || (mods == ui.Cmd && key == ui.KeyF) {
		return bindings[:0]
	}
	return append(bindings[:0],
		codeed.Binding{Mods: mods, Key: key, Command: codeed.Find},
		codeed.Binding{Mods: ui.Cmd, Key: ui.KeyF, Command: codeed.NoCommand},
	)
}

// OpenFind opens the source editor's find (or find-and-replace) bar and focuses its query input.
// For editable SVG files currently in preview mode, it switches to source mode first.
func (s *TabSurface) OpenFind(replace bool) bool {
	if !s.IsEditableSource() {
		return false
	}
	if s.IsSVG && !s.SVGShowSource {
		s.SVGShowSource = true
	}
	st := s.ensureCodeState()
	if st == nil {
		return false
	}
	if replace && !st.ReadOnly {
		return st.Run(codeed.FindReplace)
	}
	return st.Run(codeed.Find)
}

// renderSourceEditor shows non-Markdown text as a full-bleed code editor (Zed/VS Code
// style): gutter flush left, current-line band, syntax colors, no paper margins.
func (s *TabSurface) renderSourceEditor(c *ui.Context) {
	pal := theme.Ocean()
	st := s.ensureCodeState()

	codeFont := theme.ResolvedCodeFont()
	z := theme.Zoom()
	if z <= 0 {
		z = 1.0
	}
	st.Font = ui.Font{Family: codeFont, Size: 13 * z}
	st.LineHeight = 1.54
	st.SoftWrap = sourceSoftWrap
	st.Theme = sourceEditorTheme(pal)
	st.Bindings = sourceFindBindings(st.Bindings)

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
		} else if strings.EqualFold(filepath.Ext(s.Path), ".conf") {
			if lang, tag := resolveSourceLanguage(s.Path, s.Source.Text); lang != nil && tag != s.Source.Language {
				st.Buffer.SetLanguage(lang)
				s.Source.Language = tag
			}
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
		expectedFP = files.SourceTextFingerprint(s.SavedText, s.Source.BOM, s.Source.Encoding)
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
		if files.ExceedsTextSizeLimit(int64(len(encoded)), int64(len(s.Source.Text))) {
			msg := theme.Tr(
				"文件超过 5 MB 大小上限，未保存",
				"File exceeds 5 MB size limit; changes were not saved",
			)
			if s.StatusMsg != msg {
				files.LogErrorf("source save blocked (size limit): path=%s raw=%d decoded=%d", s.Path, len(encoded), len(s.Source.Text))
				s.StatusMsg = msg
			}
			return errors.New(msg)
		}
		var (
			newFP   int64
			newInfo os.FileInfo
			newID   string
			binding model.VerifiedFileTarget
			hasBind bool
		)
		if s.GetBinding != nil {
			binding, hasBind = s.GetBinding()
		}
		if hasBind && binding.ConfineRoot != "" {
			newFP, newInfo, newID, err = files.WriteBytesConditionalFingerprintInRoot(
				binding.ConfineRoot, binding.ConfineRel, encoded, expectedFP, binding.ConfineRootInfo, binding.FileInfo,
			)
		} else if hasBind && binding.FileInfo != nil {
			newFP, newInfo, newID, err = files.WriteBytesConditionalFingerprintChecked(
				s.Path, encoded, expectedFP, binding.FileInfo,
			)
		} else {
			if err = files.CheckTextVersion(s.Path, expectedFP); err == nil {
				newFP, err = files.WriteBytesAtomic(s.Path, encoded)
				if err == nil {
					if st, stErr := os.Lstat(s.Path); stErr == nil {
						newInfo = st
						newID = files.FileIdentityToken(st, nil)
					}
				}
			}
		}
		if err != nil {
			files.LogErrorf("source save failed: path=%s err=%v", s.Path, err)
			s.StatusMsg = err.Error()
			return err
		}
		if newInfo != nil && s.OnSavedIdentity != nil {
			s.OnSavedIdentity(newInfo, newID)
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
	encodedSize := files.EncodedSourceByteLen(s.Source.Text, s.Source.BOM, s.Source.Encoding)
	decodedSize := int64(len(s.Source.Text))
	if files.ExceedsTextSizeLimit(encodedSize, decodedSize) {
		msg := theme.Tr(
			"文件超过 5 MB 大小上限，未保存",
			"File exceeds 5 MB size limit; changes were not saved",
		)
		if s.StatusMsg != msg {
			files.LogErrorf("source save blocked (size limit): path=%s raw=%d decoded=%d", s.Path, encodedSize, decodedSize)
			s.StatusMsg = msg
		}
		return errors.New(msg)
	}
	var (
		newFP       int64
		newInfo     os.FileInfo
		newID       string
		writtenSize int64
		err         error
		binding     model.VerifiedFileTarget
		hasBind     bool
	)
	if s.GetBinding != nil {
		binding, hasBind = s.GetBinding()
	}
	if hasBind && (binding.ConfineRoot != "" || binding.FileInfo != nil) {
		rawBytes := files.EncodeSourceText(s.Source.Text, s.Source.BOM, s.Source.Encoding)
		writtenSize = int64(len(rawBytes))
		if binding.ConfineRoot != "" {
			newFP, newInfo, newID, err = files.WriteBytesConditionalFingerprintInRoot(
				binding.ConfineRoot, binding.ConfineRel, rawBytes, expectedFP, binding.ConfineRootInfo, binding.FileInfo,
			)
		} else {
			newFP, newInfo, newID, err = files.WriteBytesConditionalFingerprintChecked(
				s.Path, rawBytes, expectedFP, binding.FileInfo,
			)
		}
	} else {
		err = files.CheckTextVersion(s.Path, expectedFP)
		if err == nil {
			newFP, writtenSize, err = files.WriteSourceFileAtomic(s.Path, s.Source.Text, s.Source.BOM, s.Source.Encoding)
			if err == nil {
				if st, stErr := os.Lstat(s.Path); stErr == nil {
					newInfo = st
					newID = files.FileIdentityToken(st, nil)
				}
			}
		}
	}
	if err != nil {
		files.LogErrorf("source save failed: path=%s err=%v", s.Path, err)
		s.StatusMsg = err.Error()
		return err
	}
	if newInfo != nil && s.OnSavedIdentity != nil {
		s.OnSavedIdentity(newInfo, newID)
	}
	s.SavedText = s.Source.Text
	s.DiskFingerprint = newFP
	s.Source.DiskFingerprint = newFP
	s.FileSize = writtenSize
	if s.code != nil {
		s.code.Buffer.MarkSaved()
	}
	s.StatusMsg = ""
	return nil
}
