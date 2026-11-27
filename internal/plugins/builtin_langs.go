package plugins

import (
	"embed"
	"sync"
)

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
	{
		ID:         "astro",
		Name:       LocalizedText{Zh: "Astro", En: "Astro"},
		Extensions: []string{".astro"},
		Aliases:    []string{"astro"},
		Grammar:    "builtin_grammars/astro.json",
	},
	{
		ID:         "handlebars",
		Name:       LocalizedText{Zh: "Handlebars", En: "Handlebars"},
		Extensions: []string{".hbs", ".handlebars", ".mustache", ".hjs"},
		Aliases:    []string{"hbs", "handlebars", "mustache"},
		Grammar:    "builtin_grammars/handlebars.json",
	},
	{
		ID:         "ejs",
		Name:       LocalizedText{Zh: "EJS", En: "EJS"},
		Extensions: []string{".ejs"},
		Aliases:    []string{"ejs"},
		Grammar:    "builtin_grammars/ejs.json",
	},
	{
		ID:         "prisma",
		Name:       LocalizedText{Zh: "Prisma", En: "Prisma"},
		Extensions: []string{".prisma"},
		Filenames:  []string{"schema.prisma"},
		Aliases:    []string{"prisma"},
		Grammar:    "builtin_grammars/prisma.json",
	},
	{
		ID:         "http",
		Name:       LocalizedText{Zh: "HTTP 请求", En: "HTTP"},
		Extensions: []string{".http", ".rest"},
		Aliases:    []string{"http", "rest", "http-client"},
		Grammar:    "builtin_grammars/http.json",
	},
	{
		ID:         "gotmpl",
		Name:       LocalizedText{Zh: "Go / Helm 模板", En: "Go Template"},
		Extensions: []string{".tmpl", ".gotmpl", ".gohtml", ".tpl"},
		Aliases:    []string{"gotmpl", "gohtml", "go-template", "helm", "tmpl"},
		Grammar:    "builtin_grammars/gotmpl.json",
	},
	{
		ID:         "groovy",
		Name:       LocalizedText{Zh: "Groovy / Gradle", En: "Groovy"},
		Extensions: []string{".groovy", ".gradle", ".gvy", ".gy", ".gsh"},
		Filenames:  []string{"Jenkinsfile", "build.gradle", "settings.gradle"},
		Aliases:    []string{"groovy", "gradle", "jenkinsfile"},
		Grammar:    "builtin_grammars/groovy.json",
	},
	{
		ID:         "cmake",
		Name:       LocalizedText{Zh: "CMake", En: "CMake"},
		Extensions: []string{".cmake"},
		Filenames:  []string{"CMakeLists.txt"},
		Aliases:    []string{"cmake"},
		Grammar:    "builtin_grammars/cmake.json",
	},
	{
		ID:         "just",
		Name:       LocalizedText{Zh: "Justfile", En: "Justfile"},
		Extensions: []string{".just"},
		Filenames:  []string{"justfile", ".justfile"},
		Aliases:    []string{"just", "justfile"},
		Grammar:    "builtin_grammars/just.json",
	},
	{
		ID:         "elixir",
		Name:       LocalizedText{Zh: "Elixir", En: "Elixir"},
		Extensions: []string{".ex", ".exs"},
		Filenames:  []string{"mix.exs"},
		Aliases:    []string{"elixir", "ex", "exs"},
		Grammar:    "builtin_grammars/elixir.json",
	},
	{
		ID:         "heex",
		Name:       LocalizedText{Zh: "HEEx / EEx 模板", En: "HEEx / EEx"},
		Extensions: []string{".heex", ".leex", ".eex"},
		Aliases:    []string{"heex", "leex", "eex", "elixir-html"},
		Grammar:    "builtin_grammars/heex.json",
	},
	{
		ID:         "thrift",
		Name:       LocalizedText{Zh: "Thrift", En: "Thrift"},
		Extensions: []string{".thrift"},
		Aliases:    []string{"thrift"},
		Grammar:    "builtin_grammars/thrift.json",
	},
	{
		ID:         "apache",
		Name:       LocalizedText{Zh: "Apache 配置", En: "Apache"},
		Extensions: []string{".htaccess", ".apacheconf", ".vhost"},
		Filenames:  []string{".htaccess", "httpd.conf", "apache2.conf"},
		Aliases:    []string{"apache", "apacheconf", "htaccess", "httpd"},
		Grammar:    "builtin_grammars/apache.json",
	},
	{
		ID:         "haproxy",
		Name:       LocalizedText{Zh: "HAProxy 配置", En: "HAProxy"},
		Extensions: []string{".haproxy"},
		Filenames:  []string{"haproxy.cfg", "haproxy.conf"},
		Aliases:    []string{"haproxy"},
		Grammar:    "builtin_grammars/haproxy.json",
	},
	{
		ID:         "crontab",
		Name:       LocalizedText{Zh: "Crontab", En: "Crontab"},
		Extensions: []string{".cron", ".crontab"},
		Filenames:  []string{"crontab"},
		Aliases:    []string{"cron", "crontab"},
		Grammar:    "builtin_grammars/crontab.json",
	},
	{
		ID:         "procfile",
		Name:       LocalizedText{Zh: "Procfile", En: "Procfile"},
		Extensions: []string{".procfile"},
		Filenames:  []string{"Procfile", "Procfile.dev"},
		Aliases:    []string{"procfile"},
		Grammar:    "builtin_grammars/procfile.json",
	},
	{
		ID:         "powershell",
		Name:       LocalizedText{Zh: "PowerShell", En: "PowerShell"},
		Extensions: []string{".ps1", ".psm1", ".psd1"},
		Aliases:    []string{"powershell", "ps1", "pwsh", "posh"},
		Grammar:    "builtin_grammars/powershell.json",
	},
	{
		ID:         "batch",
		Name:       LocalizedText{Zh: "Windows 批处理", En: "Batch"},
		Extensions: []string{".bat", ".cmd"},
		Aliases:    []string{"batch", "bat", "cmd", "dosbatch"},
		Grammar:    "builtin_grammars/batch.json",
	},
	{
		ID:         "requirements",
		Name:       LocalizedText{Zh: "Python 依赖清单", En: "Pip Requirements"},
		Extensions: []string{".pip"},
		Filenames: []string{
			"requirements.txt", "requirements-dev.txt", "requirements-test.txt",
			"requirements-prod.txt", "requirements.in", "constraints.txt",
		},
		Aliases: []string{"requirements", "pip-requirements", "pip"},
		Grammar: "builtin_grammars/requirements.json",
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
