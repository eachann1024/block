package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"block/internal/files"
	codeed "github.com/egoist/mygo/ui/editor"
)

const (
	CurrentGrammarSchemaVersion = 1
	maxGrammarFileBytes         = 256 * 1024
	maxGrammarStates            = 63
	maxGrammarRulesPerState     = 128
	maxGrammarTotalRules        = 512
	maxDisplayFileExtensions    = 6
)

// LanguageContrib declares one syntax-highlighting language provided by a
// language extension plugin in manifest.contributes.languages.
type LanguageContrib struct {
	ID          string        `json:"id"`
	Name        LocalizedText `json:"name"`
	Extensions  []string      `json:"extensions,omitempty"`
	Filenames   []string      `json:"filenames,omitempty"`
	Aliases     []string      `json:"aliases,omitempty"`
	DisplayTags []string      `json:"display_tags,omitempty"`
	Grammar     string        `json:"grammar"`
}

// LanguageGrammar defines a declarative, RE2-based stateful line tokenizer
// loaded from a plugin's JSON grammar file.
type LanguageGrammar struct {
	SchemaVersion int                      `json:"schema_version"`
	LineComment   string                   `json:"line_comment,omitempty"`
	BlockComment  [2]string                `json:"block_comment,omitempty"`
	WordChars     string                   `json:"word_chars,omitempty"`
	TabSize       int                      `json:"tab_size,omitempty"`
	HardTabs      bool                     `json:"hard_tabs,omitempty"`
	Brackets      [][2]string              `json:"brackets,omitempty"`
	AutoClose     [][2]string              `json:"auto_close,omitempty"`
	InitialState  string                   `json:"initial_state,omitempty"`
	States        map[string][]GrammarRule `json:"states"`
	Sample        string                   `json:"sample,omitempty"`
}

// GrammarRule is a single anchored RE2 match rule inside a grammar state.
type GrammarRule struct {
	Match    string            `json:"match"`
	Token    string            `json:"token,omitempty"`
	Captures map[string]string `json:"captures,omitempty"`
	Next     string            `json:"next,omitempty"`
	Push     string            `json:"push,omitempty"`
	Pop      bool              `json:"pop,omitempty"`
}

// CodeRuneToken is a syntax-highlighted rune span returned for Markdown code
// blocks and live highlight previews.
type CodeRuneToken struct {
	Start int
	End   int
	Kind  codeed.TokenKind
}

// PluginLanguageInfo describes one compiled language owned by an installed plugin.
type PluginLanguageInfo struct {
	PluginID    string
	Contrib     LanguageContrib
	DisplayTags []string
	LineComment string
	BlockComment [2]string
	Sample      string
	CodeLang    *codeed.Language
	Tokenizer   codeed.Tokenizer
}

var (
	langExtRe      = regexp.MustCompile(`^\.[a-z0-9][a-z0-9._-]{0,23}$`)
	langAliasRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,31}$`)
	grammarStateRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)
)

var tokenKindNames = map[string]codeed.TokenKind{
	"text":        codeed.TokenText,
	"keyword":     codeed.TokenKeyword,
	"type":        codeed.TokenType,
	"function":    codeed.TokenFunction,
	"variable":    codeed.TokenVariable,
	"property":    codeed.TokenProperty,
	"constant":    codeed.TokenConstant,
	"number":      codeed.TokenNumber,
	"string":      codeed.TokenString,
	"escape":      codeed.TokenEscape,
	"regexp":      codeed.TokenRegexp,
	"comment":     codeed.TokenComment,
	"operator":    codeed.TokenOperator,
	"punctuation": codeed.TokenPunctuation,
	"tag":         codeed.TokenTag,
	"attribute":   codeed.TokenAttribute,
	"meta":        codeed.TokenMeta,
	"heading":     codeed.TokenHeading,
	"emphasis":    codeed.TokenEmphasis,
	"strong":      codeed.TokenStrong,
	"link":        codeed.TokenLink,
	"code":        codeed.TokenCode,
	"inserted":    codeed.TokenInserted,
	"deleted":     codeed.TokenDeleted,
	"invalid":     codeed.TokenInvalid,
}

func parseTokenKind(name string) (codeed.TokenKind, bool) {
	k, ok := tokenKindNames[strings.ToLower(strings.TrimSpace(name))]
	return k, ok
}

func validateLanguageGrammarPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("grammar path is required")
	}
	if strings.ContainsAny(p, "\\:") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("grammar path %q must be a relative slash-separated path", raw)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("grammar path %q escapes plugin directory", raw)
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".json") {
		return "", fmt.Errorf("grammar path %q must end with .json", raw)
	}
	return clean, nil
}

func validateLanguageContrib(l LanguageContrib) error {
	if err := validateContribText("contributes.languages.name", l.Name); err != nil {
		return err
	}
	if len(l.Extensions) == 0 && len(l.Filenames) == 0 {
		return fmt.Errorf("contributes.languages %q must declare at least one extension or filename", l.ID)
	}
	if len(l.Extensions) > 16 || len(l.Filenames) > 16 || len(l.Aliases) > 16 || len(l.DisplayTags) > 8 {
		return fmt.Errorf("contributes.languages %q exceeds matcher list limit", l.ID)
	}
	seenExt := make(map[string]bool, len(l.Extensions))
	for _, ext := range l.Extensions {
		if !langExtRe.MatchString(ext) {
			return fmt.Errorf("contributes.languages %q extension %q must be lowercase dot-prefixed (e.g. \".plist\")", l.ID, ext)
		}
		if seenExt[ext] {
			return fmt.Errorf("contributes.languages %q duplicates extension %q", l.ID, ext)
		}
		seenExt[ext] = true
	}
	seenFile := make(map[string]bool, len(l.Filenames))
	for _, fn := range l.Filenames {
		if fn == "" || strings.ContainsAny(fn, "/\\:") || fn == "." || fn == ".." || len(fn) > 64 {
			return fmt.Errorf("contributes.languages %q filename %q must be a plain file basename", l.ID, fn)
		}
		if seenFile[fn] {
			return fmt.Errorf("contributes.languages %q duplicates filename %q", l.ID, fn)
		}
		seenFile[fn] = true
	}
	seenAlias := make(map[string]bool, len(l.Aliases))
	for _, a := range l.Aliases {
		if !langAliasRe.MatchString(a) {
			return fmt.Errorf("contributes.languages %q alias %q must match %s", l.ID, a, langAliasRe.String())
		}
		if seenAlias[a] {
			return fmt.Errorf("contributes.languages %q duplicates alias %q", l.ID, a)
		}
		seenAlias[a] = true
	}
	for _, tag := range l.DisplayTags {
		t := strings.TrimSpace(tag)
		if t == "" || len(t) > 24 || ContainsEmoji(t) {
			return fmt.Errorf("contributes.languages %q invalid display_tag %q", l.ID, tag)
		}
	}
	if _, err := validateLanguageGrammarPath(l.Grammar); err != nil {
		return fmt.Errorf("contributes.languages %q: %w", l.ID, err)
	}
	return nil
}

// effectiveDisplayTags returns the short file/extension tags displayed on
// App Store rows and detail pages for a single LanguageContrib.
func (l LanguageContrib) effectiveDisplayTags() []string {
	if len(l.DisplayTags) > 0 {
		out := make([]string, 0, len(l.DisplayTags))
		for _, t := range l.DisplayTags {
			if s := strings.TrimSpace(t); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	out := make([]string, 0, len(l.Filenames)+len(l.Extensions))
	out = append(out, l.Filenames...)
	if len(out) == 0 {
		out = append(out, l.Extensions...)
	}
	return out
}

// DisplayFileExtensions returns deduplicated display tags across all languages
// contributed by m (capped at maxDisplayFileExtensions).
func (m *Manifest) DisplayFileExtensions() []string {
	if m == nil || m.Contributes == nil || len(m.Contributes.Languages) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, maxDisplayFileExtensions)
	for _, lang := range m.Contributes.Languages {
		for _, tag := range lang.effectiveDisplayTags() {
			key := strings.ToLower(tag)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, tag)
			if len(out) >= maxDisplayFileExtensions {
				return out
			}
		}
	}
	return out
}

// DecodeLanguageGrammarStrict parses and validates a JSON grammar file.
func DecodeLanguageGrammarStrict(raw []byte) (*LanguageGrammar, error) {
	if err := checkNoDuplicateJSONKeys(raw); err != nil {
		return nil, fmt.Errorf("decode grammar: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var g LanguageGrammar
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("decode grammar: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("grammar must contain exactly one JSON object")
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return &g, nil
}

// Validate checks grammar schema constraints without compiling regexes.
func (g *LanguageGrammar) Validate() error {
	if g.SchemaVersion != CurrentGrammarSchemaVersion {
		return fmt.Errorf("grammar.schema_version must be %d, got %d", CurrentGrammarSchemaVersion, g.SchemaVersion)
	}
	if len(g.States) == 0 {
		return errors.New("grammar.states must define at least one state")
	}
	if len(g.States) > maxGrammarStates {
		return fmt.Errorf("grammar.states allows at most %d states", maxGrammarStates)
	}
	initState := g.initialStateName()
	if _, ok := g.States[initState]; !ok {
		return fmt.Errorf("grammar initial state %q not found in states", initState)
	}
	if g.TabSize < 0 || g.TabSize > 16 {
		return fmt.Errorf("grammar.tab_size must be between 0 and 16, got %d", g.TabSize)
	}
	totalRules := 0
	for name, rules := range g.States {
		if !grammarStateRe.MatchString(name) {
			return fmt.Errorf("grammar state name %q must match %s", name, grammarStateRe.String())
		}
		if len(rules) > maxGrammarRulesPerState {
			return fmt.Errorf("grammar state %q allows at most %d rules", name, maxGrammarRulesPerState)
		}
		totalRules += len(rules)
		if totalRules > maxGrammarTotalRules {
			return fmt.Errorf("grammar allows at most %d total rules", maxGrammarTotalRules)
		}
		for i, r := range rules {
			if strings.TrimSpace(r.Match) == "" {
				return fmt.Errorf("grammar state %q rule[%d]: match is required", name, i)
			}
			if r.Token != "" {
				if _, ok := parseTokenKind(r.Token); !ok {
					return fmt.Errorf("grammar state %q rule[%d]: unknown token kind %q", name, i, r.Token)
				}
			}
			if r.Token == "" && len(r.Captures) == 0 && r.Next == "" && r.Push == "" && !r.Pop {
				return fmt.Errorf("grammar state %q rule[%d]: must specify token, captures, or a state transition", name, i)
			}
			for grp, tok := range r.Captures {
				n, err := strconv.Atoi(grp)
				if err != nil || n < 1 || n > 16 {
					return fmt.Errorf("grammar state %q rule[%d]: capture group %q must be an integer 1..16", name, i, grp)
				}
				if _, ok := parseTokenKind(tok); !ok {
					return fmt.Errorf("grammar state %q rule[%d]: capture %q unknown token kind %q", name, i, grp, tok)
				}
			}
			transitions := 0
			if r.Next != "" {
				transitions++
				if _, ok := g.States[r.Next]; !ok {
					return fmt.Errorf("grammar state %q rule[%d]: unknown next state %q", name, i, r.Next)
				}
			}
			if r.Push != "" {
				transitions++
				if _, ok := g.States[r.Push]; !ok {
					return fmt.Errorf("grammar state %q rule[%d]: unknown push state %q", name, i, r.Push)
				}
			}
			if r.Pop {
				transitions++
			}
			if transitions > 1 {
				return fmt.Errorf("grammar state %q rule[%d]: at most one of next, push, pop may be set", name, i)
			}
		}
	}
	return nil
}

func (g *LanguageGrammar) initialStateName() string {
	if s := strings.TrimSpace(g.InitialState); s != "" {
		return s
	}
	return "root"
}

type compiledCapture struct {
	group int
	kind  codeed.TokenKind
}

type compiledRule struct {
	re         *regexp.Regexp
	kind       codeed.TokenKind
	hasKind    bool
	captures   []compiledCapture
	transKind  uint8 // 0=none, 1=next, 2=push, 3=pop
	transState uint64
}

const (
	transNone uint8 = iota
	transNext
	transPush
	transPop
)

// grammarTokenizer implements codeed.Tokenizer for a declarative LanguageGrammar.
// State stack encoding inside uint64:
//   - bits 60..63: stack depth (0..9)
//   - bits 0..59: up to 10 frames x 6 bits each; low 6 bits is active state index
//   - state index 0 is always initial_state, so state == 0 at start of text.
type grammarTokenizer struct {
	states [][]compiledRule
}

const (
	stateFrameBits  = 6
	stateFrameMask  = (uint64(1) << stateFrameBits) - 1
	stateStackMask  = (uint64(1) << 60) - 1
	stateMaxDepth   = 9
	maxZeroStepLoop = 8
)

func (t *grammarTokenizer) transition(state uint64, kind uint8, target uint64) uint64 {
	switch kind {
	case transNext:
		return (state &^ stateFrameMask) | (target & stateFrameMask)
	case transPush:
		depth := state >> 60
		if depth >= stateMaxDepth {
			return (state &^ stateFrameMask) | (target & stateFrameMask)
		}
		frames := ((state & stateStackMask) << stateFrameBits) & stateStackMask
		return ((depth + 1) << 60) | frames | (target & stateFrameMask)
	case transPop:
		depth := state >> 60
		if depth == 0 {
			return 0
		}
		frames := (state & stateStackMask) >> stateFrameBits
		return ((depth - 1) << 60) | frames
	default:
		return state
	}
}

func appendMergedToken(tokens []codeed.Token, end int, kind codeed.TokenKind) []codeed.Token {
	if n := len(tokens); n > 0 {
		if end <= tokens[n-1].End {
			return tokens
		}
		if tokens[n-1].Kind == kind {
			tokens[n-1].End = end
			return tokens
		}
	}
	return append(tokens, codeed.Token{End: end, Kind: kind})
}

func (t *grammarTokenizer) Tokenize(line string, state uint64, tokens []codeed.Token) ([]codeed.Token, uint64) {
	n := len(line)
	if n == 0 {
		return tokens, state
	}
	pos := 0
	zeroSteps := 0
	for pos < n {
		stIdx := int(state & stateFrameMask)
		if stIdx < 0 || stIdx >= len(t.states) {
			stIdx = 0
			state = 0
		}
		rules := t.states[stIdx]
		matched := false
		rem := line[pos:]
		for i := range rules {
			rule := &rules[i]
			if len(rule.captures) == 0 {
				loc := rule.re.FindStringIndex(rem)
				if loc == nil {
					continue
				}
				mLen := loc[1]
				nextState := t.transition(state, rule.transKind, rule.transState)
				if mLen == 0 {
					if nextState == state || zeroSteps >= maxZeroStepLoop {
						continue
					}
					zeroSteps++
					state = nextState
					matched = true
					break
				}
				zeroSteps = 0
				kind := codeed.TokenText
				if rule.hasKind {
					kind = rule.kind
				}
				pos += mLen
				tokens = appendMergedToken(tokens, pos, kind)
				state = nextState
				matched = true
				break
			}

			sub := rule.re.FindStringSubmatchIndex(rem)
			if sub == nil {
				continue
			}
			mLen := sub[1]
			nextState := t.transition(state, rule.transKind, rule.transState)
			if mLen == 0 {
				if nextState == state || zeroSteps >= maxZeroStepLoop {
					continue
				}
				zeroSteps++
				state = nextState
				matched = true
				break
			}
			zeroSteps = 0
			fallback := codeed.TokenText
			if rule.hasKind {
				fallback = rule.kind
			}
			cursor := 0
			for _, capSpec := range rule.captures {
				idx := capSpec.group * 2
				if idx+1 >= len(sub) {
					continue
				}
				cs, ce := sub[idx], sub[idx+1]
				if cs < 0 || ce <= cs || cs < cursor || ce > mLen {
					continue
				}
				if cs > cursor {
					tokens = appendMergedToken(tokens, pos+cs, fallback)
				}
				tokens = appendMergedToken(tokens, pos+ce, capSpec.kind)
				cursor = ce
			}
			if cursor < mLen {
				tokens = appendMergedToken(tokens, pos+mLen, fallback)
			}
			pos += mLen
			state = nextState
			matched = true
			break
		}
		if !matched {
			_, size := utf8.DecodeRuneInString(rem)
			if size <= 0 {
				size = 1
			}
			pos += size
			zeroSteps = 0
			tokens = appendMergedToken(tokens, pos, codeed.TokenText)
		}
	}
	return tokens, state
}

func compileLanguageGrammar(contrib LanguageContrib, g *LanguageGrammar) (*codeed.Language, codeed.Tokenizer, error) {
	if err := g.Validate(); err != nil {
		return nil, nil, err
	}
	initName := g.initialStateName()
	names := make([]string, 0, len(g.States))
	names = append(names, initName)
	other := make([]string, 0, len(g.States)-1)
	for name := range g.States {
		if name != initName {
			other = append(other, name)
		}
	}
	sort.Strings(other)
	names = append(names, other...)

	stateIndex := make(map[string]uint64, len(names))
	for i, name := range names {
		stateIndex[name] = uint64(i)
	}

	compiledStates := make([][]compiledRule, len(names))
	for i, name := range names {
		srcRules := g.States[name]
		dstRules := make([]compiledRule, 0, len(srcRules))
		for ri, r := range srcRules {
			re, err := regexp.Compile(`\A(?:` + r.Match + `)`)
			if err != nil {
				return nil, nil, fmt.Errorf("grammar state %q rule[%d] regex: %w", name, ri, err)
			}
			cr := compiledRule{re: re}
			if r.Token != "" {
				k, _ := parseTokenKind(r.Token)
				cr.kind = k
				cr.hasKind = true
			}
			if len(r.Captures) > 0 {
				numSub := re.NumSubexp()
				caps := make([]compiledCapture, 0, len(r.Captures))
				for grpStr, tokName := range r.Captures {
					grp, _ := strconv.Atoi(grpStr)
					if grp > numSub {
						return nil, nil, fmt.Errorf("grammar state %q rule[%d]: capture group %d exceeds subexpressions (%d)", name, ri, grp, numSub)
					}
					k, _ := parseTokenKind(tokName)
					caps = append(caps, compiledCapture{group: grp, kind: k})
				}
				sort.Slice(caps, func(a, b int) bool { return caps[a].group < caps[b].group })
				cr.captures = caps
			}
			switch {
			case r.Next != "":
				cr.transKind = transNext
				cr.transState = stateIndex[r.Next]
			case r.Push != "":
				cr.transKind = transPush
				cr.transState = stateIndex[r.Push]
			case r.Pop:
				cr.transKind = transPop
			}
			dstRules = append(dstRules, cr)
		}
		compiledStates[i] = dstRules
	}

	tok := &grammarTokenizer{states: compiledStates}
	brackets := make([]codeed.BracketPair, 0, len(g.Brackets))
	for _, pair := range g.Brackets {
		if pair[0] != "" && pair[1] != "" {
			brackets = append(brackets, codeed.BracketPair{Open: pair[0], Close: pair[1]})
		}
	}
	autoClose := make([]codeed.BracketPair, 0, len(g.AutoClose))
	for _, pair := range g.AutoClose {
		if pair[0] != "" && pair[1] != "" {
			autoClose = append(autoClose, codeed.BracketPair{Open: pair[0], Close: pair[1]})
		}
	}
	langName := strings.TrimSpace(contrib.Name.En)
	if langName == "" {
		langName = contrib.ID
	}
	codeLang := &codeed.Language{
		Name:         langName,
		Extensions:   append([]string(nil), contrib.Extensions...),
		Filenames:    append([]string(nil), contrib.Filenames...),
		LineComment:  g.LineComment,
		BlockComment: g.BlockComment,
		Brackets:     brackets,
		AutoClose:    autoClose,
		WordChars:    g.WordChars,
		TabSize:      g.TabSize,
		HardTabs:     g.HardTabs,
		Tokenizer:    tok,
	}
	return codeLang, tok, nil
}

func loadPluginLanguage(root *os.Root, pluginID string, contrib LanguageContrib) (*PluginLanguageInfo, error) {
	relPath, err := validateLanguageGrammarPath(contrib.Grammar)
	if err != nil {
		return nil, err
	}
	raw, err := readPluginFile(root, relPath, maxGrammarFileBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	grammar, err := DecodeLanguageGrammarStrict(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relPath, err)
	}
	codeLang, tok, err := compileLanguageGrammar(contrib, grammar)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relPath, err)
	}
	return &PluginLanguageInfo{
		PluginID:     pluginID,
		Contrib:      contrib,
		DisplayTags:  contrib.effectiveDisplayTags(),
		LineComment:  grammar.LineComment,
		BlockComment: grammar.BlockComment,
		Sample:       grammar.Sample,
		CodeLang:     codeLang,
		Tokenizer:    tok,
	}, nil
}

// ValidatePluginLanguages loads and compiles every grammar declared in m.Contributes.Languages.
func ValidatePluginLanguages(pluginDir string, m *Manifest) error {
	if m == nil || m.Contributes == nil || len(m.Contributes.Languages) == 0 {
		return nil
	}
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, lc := range m.Contributes.Languages {
		if _, err := loadPluginLanguage(root, m.ID, lc); err != nil {
			return fmt.Errorf("language %q: %w", lc.ID, err)
		}
	}
	return nil
}

// TokenizeTextWithTokenizer runs tok line-by-line across src and returns rune-offset
// syntax tokens suitable for Markdown code blocks and Settings highlight previews.
func TokenizeTextWithTokenizer(tok codeed.Tokenizer, src string) []CodeRuneToken {
	if tok == nil || src == "" {
		return nil
	}
	var (
		out      []CodeRuneToken
		buf      []codeed.Token
		state    uint64
		runeBase int
		start    int
	)
	for start <= len(src) {
		relEnd := strings.IndexByte(src[start:], '\n')
		var line string
		var nextStart int
		if relEnd < 0 {
			line = src[start:]
			nextStart = len(src) + 1
		} else {
			line = src[start : start+relEnd]
			nextStart = start + relEnd + 1
		}
		if len(line) > 0 {
			buf, state = tok.Tokenize(line, state, buf[:0])
			bytePos := 0
			runePos := runeBase
			for _, tk := range buf {
				endByte := min(max(tk.End, bytePos), len(line))
				if endByte <= bytePos {
					continue
				}
				rc := utf8.RuneCountInString(line[bytePos:endByte])
				if tk.Kind != codeed.TokenText && rc > 0 {
					out = append(out, CodeRuneToken{
						Start: runePos,
						End:   runePos + rc,
						Kind:  tk.Kind,
					})
				}
				bytePos = endByte
				runePos += rc
			}
			runeBase += utf8.RuneCountInString(line)
		}
		if relEnd >= 0 {
			runeBase++ // '\n'
		}
		start = nextStart
	}
	return out
}

// ---------------------------------------------------------------------------
// Active Language Registry (synced from Supervisor)

type languageSnapshot struct {
	byFilename  map[string]*PluginLanguageInfo
	byExt       map[string]*PluginLanguageInfo
	byTag       map[string]*PluginLanguageInfo
	byPlugin    map[string][]PluginLanguageInfo
	all         []PluginLanguageInfo
}

var (
	activeLangPublishMu    sync.Mutex
	activeLangSyncSeq      atomic.Uint64
	activeLangPublishedSeq uint64
	activeLangSnap         atomic.Pointer[languageSnapshot]
	activeLangGen          atomic.Uint64
)

// NextLanguageSyncSeq allocates a strictly monotonic sequence number for an
// installed-plugin state snapshot while the caller holds Supervisor.mu, so
// slower out-of-order grammar compilations can never overwrite a newer toggle.
func NextLanguageSyncSeq() uint64 {
	return activeLangSyncSeq.Add(1)
}

// SyncActiveLanguages compiles all languages from enabled installed plugins and
// publishes them atomically for the source editor and Markdown code blocks.
func SyncActiveLanguages(installed []InstalledApp) {
	SyncActiveLanguagesSeq(installed, 0)
}

// SyncActiveLanguagesSeq compiles and publishes installed languages only if seq
// is zero or at least as new as the last published sequence number.
func SyncActiveLanguagesSeq(installed []InstalledApp, seq uint64) {
	snap := newLanguageSnapshot()
	for _, app := range installed {
		if !app.Enabled || app.InstallDir == "" || app.Manifest.Contributes == nil || len(app.Manifest.Contributes.Languages) == 0 {
			continue
		}
		root, err := os.OpenRoot(app.InstallDir)
		if err != nil {
			continue
		}
		for _, lc := range app.Manifest.Contributes.Languages {
			info, err := loadPluginLanguage(root, app.Manifest.ID, lc)
			if err != nil {
				continue
			}
			snap.add(info)
		}
		root.Close()
	}
	activeLangPublishMu.Lock()
	if seq != 0 && seq < activeLangPublishedSeq {
		activeLangPublishMu.Unlock()
		return
	}
	if seq != 0 {
		activeLangPublishedSeq = seq
	}
	activeLangSnap.Store(snap)
	gen := activeLangGen.Add(1)
	scheduleFileAssociationSync(files.AppDataDir(), snap, gen)
	activeLangPublishMu.Unlock()
}

func newLanguageSnapshot() *languageSnapshot {
	return &languageSnapshot{
		byFilename: make(map[string]*PluginLanguageInfo),
		byExt:      make(map[string]*PluginLanguageInfo),
		byTag:      make(map[string]*PluginLanguageInfo),
		byPlugin:   make(map[string][]PluginLanguageInfo),
	}
}

// add indexes info by filename, extension and tag; earlier entries win.
func (snap *languageSnapshot) add(info *PluginLanguageInfo) {
	lc := info.Contrib
	snap.all = append(snap.all, *info)
	snap.byPlugin[info.PluginID] = append(snap.byPlugin[info.PluginID], *info)
	for _, fn := range lc.Filenames {
		key := strings.ToLower(fn)
		if _, exists := snap.byFilename[key]; !exists {
			snap.byFilename[key] = info
		}
	}
	for _, ext := range lc.Extensions {
		key := strings.ToLower(ext)
		if _, exists := snap.byExt[key]; !exists {
			snap.byExt[key] = info
		}
	}
	registerTag := func(tag string) {
		k := strings.ToLower(strings.TrimSpace(tag))
		if k == "" {
			return
		}
		if _, exists := snap.byTag[k]; !exists {
			snap.byTag[k] = info
		}
	}
	registerTag(lc.ID)
	registerTag(lc.Name.En)
	for _, a := range lc.Aliases {
		registerTag(a)
	}
	for _, ext := range lc.Extensions {
		registerTag(strings.TrimPrefix(ext, "."))
	}
}

// LanguageGeneration returns a monotonic counter incremented whenever enabled
// language extension plugins change.
func LanguageGeneration() uint64 {
	return activeLangGen.Load()
}

// ResolveCodeLanguageForFile matches filePath against active language plugins
// (exact filename first, then longest matching extension such as .d.ts before
// .ts). Returns (nil, "") when none match.
func ResolveCodeLanguageForFile(filePath string) (*codeed.Language, string) {
	base := strings.ToLower(filepath.Base(filePath))
	for _, snap := range languageSnapshots() {
		if info := snap.byFilename[base]; info != nil {
			return info.CodeLang, info.Contrib.ID
		}
		for i := strings.IndexByte(base, '.'); i >= 0 && i < len(base)-1; {
			if info := snap.byExt[base[i:]]; info != nil {
				return info.CodeLang, info.Contrib.ID
			}
			next := strings.IndexByte(base[i+1:], '.')
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	return nil, ""
}

// languageSnapshots returns enabled plugin languages first, then built-in
// grammars, so an installed plugin can override a built-in language.
func languageSnapshots() []*languageSnapshot {
	out := make([]*languageSnapshot, 0, 2)
	if snap := activeLangSnap.Load(); snap != nil {
		out = append(out, snap)
	}
	if b := builtinLanguages(); b != nil {
		out = append(out, b)
	}
	return out
}

func lookupLanguageTag(tag string) *PluginLanguageInfo {
	key := strings.ToLower(strings.TrimSpace(tag))
	for _, snap := range languageSnapshots() {
		if info := snap.byTag[key]; info != nil {
			return info
		}
	}
	return nil
}

// ResolveLanguageTagDisplay returns the localized/canonical display label for a
// code-block language tag or plugin language ID if provided by an active plugin.
func ResolveLanguageTagDisplay(tag, locale string) (string, bool) {
	info := lookupLanguageTag(tag)
	if info == nil {
		return "", false
	}
	return info.Contrib.Name.Resolve(locale), true
}

// TokenizeCodeByTag highlights src using an active language plugin matching tag.
func TokenizeCodeByTag(tag, src string) ([]CodeRuneToken, bool) {
	info := lookupLanguageTag(tag)
	if info == nil || info.Tokenizer == nil {
		return nil, false
	}
	return TokenizeTextWithTokenizer(info.Tokenizer, src), true
}

// ActiveLanguagesForPlugin returns compiled language details for pluginID.
func ActiveLanguagesForPlugin(pluginID string) []PluginLanguageInfo {
	snap := activeLangSnap.Load()
	if snap == nil {
		return nil
	}
	list := snap.byPlugin[pluginID]
	if len(list) == 0 {
		return nil
	}
	out := make([]PluginLanguageInfo, len(list))
	copy(out, list)
	return out
}

// LoadPluginLanguagesFromDir loads all compiled languages directly from an
// installed plugin directory (even when currently disabled, for settings view).
func LoadPluginLanguagesFromDir(pluginDir string, m *Manifest) []PluginLanguageInfo {
	if pluginDir == "" || m == nil || m.Contributes == nil || len(m.Contributes.Languages) == 0 {
		return nil
	}
	root, err := os.OpenRoot(pluginDir)
	if err != nil {
		return nil
	}
	defer root.Close()
	out := make([]PluginLanguageInfo, 0, len(m.Contributes.Languages))
	for _, lc := range m.Contributes.Languages {
		if info, err := loadPluginLanguage(root, m.ID, lc); err == nil && info != nil {
			out = append(out, *info)
		}
	}
	return out
}
