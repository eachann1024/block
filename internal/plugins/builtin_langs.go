package plugins

import (
	"embed"
	"sync"
)

// SupersededLanguagePluginIDs lists former official language extensions whose
// grammars now ship built in; the store hides them.
var SupersededLanguagePluginIDs = map[string]bool{
	"block-lang-gomod": true,
	"block-lang-plist": true,
}

//go:embed builtin_grammars/*.json
var builtinGrammarFS embed.FS

const builtinLanguageOwner = "builtin"

var builtinLanguageContribs = []LanguageContrib{
	{
		ID:         "gomod",
		Name:       LocalizedText{Zh: "Go Module", En: "Go Module"},
		Filenames:  []string{"go.mod", "go.work"},
		Extensions: []string{".mod", ".work"},
		Aliases:    []string{"go.mod", "go.work", "gowork", "mod"},
		Grammar:    "builtin_grammars/gomod.json",
	},
	{
		ID:         "gosum",
		Name:       LocalizedText{Zh: "Go Checksum", En: "Go Checksum"},
		Filenames:  []string{"go.sum", "go.work.sum"},
		Extensions: []string{".sum"},
		Aliases:    []string{"go.sum", "sum"},
		Grammar:    "builtin_grammars/gosum.json",
	},
	{
		ID:         "plist",
		Name:       LocalizedText{Zh: "Property List", En: "Property List"},
		Extensions: []string{".plist"},
		Filenames:  []string{"Info.plist"},
		Aliases:    []string{"property-list", "apple-plist"},
		Grammar:    "builtin_grammars/plist.json",
	},
}

var (
	builtinLangOnce sync.Once
	builtinLangSnap *languageSnapshot
)

// builtinLanguages compiles the embedded grammars once; they rank below any
// enabled language plugin.
func builtinLanguages() *languageSnapshot {
	builtinLangOnce.Do(func() {
		snap := newLanguageSnapshot()
		for _, lc := range builtinLanguageContribs {
			raw, err := builtinGrammarFS.ReadFile(lc.Grammar)
			if err != nil {
				continue
			}
			g, err := DecodeLanguageGrammarStrict(raw)
			if err != nil {
				continue
			}
			codeLang, tok, err := compileLanguageGrammar(lc, g)
			if err != nil {
				continue
			}
			snap.add(&PluginLanguageInfo{
				PluginID:     builtinLanguageOwner,
				Contrib:      lc,
				LineComment:  g.LineComment,
				BlockComment: g.BlockComment,
				Sample:       g.Sample,
				CodeLang:     codeLang,
				Tokenizer:    tok,
			})
		}
		builtinLangSnap = snap
	})
	return builtinLangSnap
}
