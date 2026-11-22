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
		Extensions: []string{".plist", ".entitlements", ".strings", ".pbxproj"},
		Filenames:  []string{"Info.plist", "project.pbxproj"},
		Aliases:    []string{"property-list", "apple-plist"},
		Grammar:    "builtin_grammars/plist.json",
	},
	{
		ID:         "toml",
		Name:       LocalizedText{Zh: "TOML", En: "TOML"},
		Extensions: []string{".toml"},
		Filenames: []string{
			"Cargo.toml", "Cargo.lock", "pyproject.toml", "uv.toml", "bunfig.toml",
			"starship.toml", "poetry.lock", "pdm.lock", "Pipfile", "mise.toml",
			"wrangler.toml", "netlify.toml", "fly.toml", "alacritty.toml", "rust-toolchain.toml",
		},
		Aliases: []string{"toml"},
		Grammar: "builtin_grammars/toml.json",
	},
	{
		ID:   "ini",
		Name: LocalizedText{Zh: "INI 配置", En: "INI Config"},
		Extensions: []string{
			".ini", ".cfg", ".cnf", ".properties", ".editorconfig",
			".service", ".socket", ".timer", ".target", ".mount",
			".desktop", ".inf", ".xcconfig",
		},
		Filenames: []string{
			".gitconfig", ".gitmodules", ".npmrc", ".yarnrc", ".pypirc",
			".flake8", ".pylintrc", ".coveragerc", ".wgetrc", ".curlrc",
			"pip.conf", "php.ini", "my.cnf", "supervisord.conf", "frpc.ini", "rclone.conf",
		},
		Aliases: []string{
			"ini", "cfg", "conf", "properties", "dosini",
			"editorconfig", "gitconfig", "systemd", "xcconfig", "wireguard", "ghostty",
		},
		Grammar: "builtin_grammars/ini.json",
	},
	{
		ID:         "dotenv",
		Name:       LocalizedText{Zh: "环境变量", En: "Dotenv"},
		Extensions: []string{".env"},
		Filenames: []string{
			".env", ".env.local", ".env.development", ".env.production",
			".env.test", ".env.staging", ".env.example", ".env.sample",
			".env.template", ".env.defaults",
		},
		Aliases: []string{"env", "dotenv"},
		Grammar: "builtin_grammars/dotenv.json",
	},
	{
		ID:   "surge",
		Name: LocalizedText{Zh: "Surge / 代理规则", En: "Surge / Proxy Rules"},
		Extensions: []string{
			".sgmodule", ".sgsnippet", ".dconf", ".lpx", ".plugin",
			".module", ".qx", ".snippet", ".list",
		},
		Filenames: []string{"Surge.conf"},
		Aliases:   []string{"surge", "sgmodule", "loon", "quantumultx", "qx", "shadowrocket", "ruleset", "proxy-rules"},
		Grammar:   "builtin_grammars/surge.json",
	},
	{
		ID:         "nginx",
		Name:       LocalizedText{Zh: "Nginx 配置", En: "Nginx"},
		Extensions: []string{".nginx", ".nginxconf"},
		Filenames:  []string{"nginx.conf", "mime.types", "fastcgi_params", "uwsgi_params", "scgi_params", "proxy_params"},
		Aliases:    []string{"nginx", "nginxconf"},
		Grammar:    "builtin_grammars/nginx.json",
	},
	{
		ID:         "caddyfile",
		Name:       LocalizedText{Zh: "Caddyfile", En: "Caddyfile"},
		Extensions: []string{".caddy", ".caddyfile"},
		Filenames:  []string{"Caddyfile"},
		Aliases:    []string{"caddy", "caddyfile"},
		Grammar:    "builtin_grammars/caddyfile.json",
	},
	{
		ID:         "sshconfig",
		Name:       LocalizedText{Zh: "SSH 配置", En: "SSH Config"},
		Extensions: []string{".sshconfig"},
		Filenames:  []string{"ssh_config", "sshd_config"},
		Aliases:    []string{"ssh", "sshconfig", "ssh_config", "sshd_config"},
		Grammar:    "builtin_grammars/sshconfig.json",
	},
	{
		ID:   "ignore",
		Name: LocalizedText{Zh: "忽略规则", En: "Ignore File"},
		Extensions: []string{
			".gitignore", ".dockerignore", ".npmignore", ".prettierignore",
			".eslintignore", ".cursorignore", ".hgignore", ".ignore",
		},
		Filenames: []string{
			".gitignore", ".dockerignore", ".npmignore", ".prettierignore",
			".eslintignore", ".cursorignore", ".hgignore", ".ignore",
			".vscodeignore", ".helmignore", ".gcloudignore", ".cfignore",
		},
		Aliases: []string{"gitignore", "dockerignore", "ignore"},
		Grammar: "builtin_grammars/ignore.json",
	},
	{
		ID:         "hosts",
		Name:       LocalizedText{Zh: "Hosts 表", En: "Hosts"},
		Extensions: []string{".hosts"},
		Filenames:  []string{"hosts"},
		Aliases:    []string{"hosts"},
		Grammar:    "builtin_grammars/hosts.json",
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
