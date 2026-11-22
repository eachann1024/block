package icons

import (
	"path"
	"strings"
)

var exactFileIcons = map[string]Icon{
	// License
	"license": FileLicense,
	"licence": FileLicense,
	"copying": FileLicense,
	"notice":  FileLicense,

	// Docker
	"dockerfile":    FileDocker,
	".dockerignore": FileDocker,
	"compose.yml":   FileDocker,
	"compose.yaml":  FileDocker,

	// Build scripts
	"makefile":       Wrench,
	"gnumakefile":    Wrench,
	"justfile":       Wrench,
	".justfile":      Wrench,
	"cmakelists.txt": Wrench,
	"rakefile":       Wrench,
	"jenkinsfile":    Wrench,
	"procfile":       Wrench,

	// Git configuration
	".gitignore":     FileGit,
	".gitattributes": FileGit,
	".gitmodules":    FileGit,
	".gitkeep":       FileGit,
	".gitconfig":     FileGit,

	// Package manifests
	"package.json":     Package,
	"go.mod":           Package,
	"go.work":          Package,
	"cargo.toml":       Package,
	"pyproject.toml":   Package,
	"requirements.txt": Package,
	"requirements.in":  Package,
	"constraints.txt":  Package,
	"composer.json":    Package,
	"gemfile":          Package,
	"pom.xml":          Package,
	"build.gradle":     Package,
	"settings.gradle":  Package,
	"mix.exs":          Package,
	"pubspec.yaml":     Package,
	"deno.json":        Package,
	"bunfig.toml":      Package,

	// Environment & config
	".env":            FileEnv,
	".editorconfig":   FileConfig,
	".npmignore":      FileConfig,
	".prettierignore": FileConfig,
	".eslintignore":   FileConfig,
	".cursorignore":   FileConfig,
	".ignore":         FileConfig,
	".htaccess":       FileConfig,
	"httpd.conf":      FileConfig,
	"apache2.conf":    FileConfig,
	"haproxy.cfg":     FileConfig,
	"haproxy.conf":    FileConfig,
	"crontab":         FileConfig,
	"caddyfile":       FileConfig,
	"nginx.conf":      FileConfig,
	"ssh_config":      FileConfig,
	"sshd_config":     FileConfig,
	"hosts":           FileConfig,
	"uv.toml":         FileConfig,
	"starship.toml":   FileConfig,
	"mise.toml":       FileConfig,
	"wrangler.toml":   FileConfig,
	"alacritty.toml":  FileConfig,
	"schema.prisma":   LangSql,

	// Lockfiles
	"go.sum":            FileLock,
	"package-lock.json": FileLock,
	"pnpm-lock.yaml":    FileLock,
	"yarn.lock":         FileLock,
	"poetry.lock":       FileLock,
	"gemfile.lock":      FileLock,
	"composer.lock":     FileLock,
}

var extFileIcons = map[string]Icon{
	// Documents
	".md":       Note,
	".markdown": Note,
	".mdown":    Note,
	".mkd":      Note,
	".mkdn":     Note,
	".mdwn":     Note,
	".mdtxt":    Note,
	".mdtext":   Note,
	".mdx":      Note,
	".txt":      LangPlain,
	".text":     LangPlain,
	".pdf":      FilePdf,
	".doc":      FileWord,
	".docx":     FileWord,
	".rtf":      FileWord,
	".pages":    FileWord,
	".odt":      FileWord,
	".csv":      FileSheet,
	".tsv":      FileSheet,
	".xls":      FileSheet,
	".xlsx":     FileSheet,
	".numbers":  FileSheet,
	".ods":      FileSheet,
	".ppt":      FileSlides,
	".pptx":     FileSlides,
	".key":      FileSlides,
	".odp":      FileSlides,
	".log":      FileLog,
	".out":      FileLog,
	".epub":     FileBook,
	".mobi":     FileBook,
	".azw3":     FileBook,

	// Code
	".js":      LangJs,
	".mjs":     LangJs,
	".cjs":     LangJs,
	".ts":      LangTs,
	".mts":     LangTs,
	".cts":     LangTs,
	".jsx":     LangReact,
	".tsx":     LangReact,
	".py":      LangPython,
	".pyi":     LangPython,
	".pyw":     LangPython,
	".ipynb":   LangPython,
	".go":      LangGo,
	".tmpl":    LangGo,
	".gotmpl":  LangGo,
	".tpl":     LangGo,
	".rs":      LangRust,
	".java":    LangJava,
	".groovy":  LangJava,
	".gvy":     LangJava,
	".gy":      LangJava,
	".gsh":     LangJava,
	".jar":     LangJava,
	".class":   LangJava,
	".kt":      LangKotlin,
	".kts":     LangKotlin,
	".swift":   LangSwift,
	".c":       LangC,
	".h":       LangC,
	".cpp":     LangCpp,
	".cc":      LangCpp,
	".cxx":     LangCpp,
	".hpp":     LangCpp,
	".hh":      LangCpp,
	".hxx":     LangCpp,
	".cs":      LangCsharp,
	".csx":     LangCsharp,
	".rb":      LangRuby,
	".gemspec": LangRuby,
	".rake":    LangRuby,
	".php":     LangPhp,
	".phtml":   LangPhp,
	".lua":     LangLua,
	".vue":     LangVue,
	".html":       LangHtml,
	".htm":        LangHtml,
	".xhtml":      LangHtml,
	".astro":      LangHtml,
	".hbs":        LangHtml,
	".handlebars": LangHtml,
	".mustache":   LangHtml,
	".hjs":        LangHtml,
	".ejs":        LangHtml,
	".gohtml":     LangHtml,
	".heex":       LangHtml,
	".leex":       LangHtml,
	".eex":        LangHtml,
	".ex":         Zap,
	".exs":        Zap,
	".http":       Send,
	".rest":       Send,
	".thrift":     Plug,
	".css":        LangCss,
	".scss":       LangCss,
	".sass":       LangCss,
	".less":       LangCss,
	".sh":         LangBash,
	".bash":       LangBash,
	".zsh":        LangBash,
	".fish":       LangBash,
	".ps1":        LangBash,
	".psm1":       LangBash,
	".psd1":       LangBash,
	".bat":        LangBash,
	".cmd":        LangBash,
	".sql":        LangSql,
	".prisma":     LangSql,
	".db":      LangSql,
	".sqlite":  LangSql,
	".sqlite3": LangSql,
	".lean":    LangLean,

	// Config & Data
	".json":         LangJson,
	".jsonc":        LangJson,
	".json5":        LangJson,
	".jsonl":        LangJson,
	".ndjson":       LangJson,
	".yaml":         LangYaml,
	".yml":          LangYaml,
	".stoverride":   LangYaml,
	".xml":          LangXml,
	".ps1xml":       LangXml,
	".plist":        LangXml,
	".entitlements": LangXml,
	".pbxproj":      LangXml,
	".strings":      LangXml,
	".xsd":          LangXml,
	".xsl":          LangXml,
	".xslt":         LangXml,
	".toml":         FileConfig,
	".ini":          FileConfig,
	".conf":         FileConfig,
	".cfg":          FileConfig,
	".cnf":          FileConfig,
	".properties":   FileConfig,
	".sgmodule":     FileConfig,
	".sgsnippet":    FileConfig,
	".dconf":        FileConfig,
	".lpx":          FileConfig,
	".plugin":       FileConfig,
	".module":       FileConfig,
	".qx":           FileConfig,
	".snippet":      FileConfig,
	".list":         FileConfig,
	".nginx":        FileConfig,
	".nginxconf":    FileConfig,
	".caddy":        FileConfig,
	".caddyfile":    FileConfig,
	".sshconfig":    FileConfig,
	".xcconfig":     FileConfig,
	".hosts":        FileConfig,
	".htaccess":     FileConfig,
	".apacheconf":   FileConfig,
	".vhost":        FileConfig,
	".haproxy":      FileConfig,
	".cron":         FileConfig,
	".crontab":      FileConfig,
	".pip":          Package,
	".service":      FileConfig,
	".socket":       FileConfig,
	".timer":        FileConfig,
	".target":       FileConfig,
	".mount":        FileConfig,
	".desktop":      FileConfig,
	".inf":          FileConfig,
	".env":          FileEnv,
	".lock":         FileLock,

	// Media
	".png":   Image,
	".jpg":   Image,
	".jpeg":  Image,
	".gif":   Image,
	".webp":  Image,
	".heic":  Image,
	".heif":  Image,
	".ico":   Image,
	".bmp":   Image,
	".avif":  Image,
	".tiff":  Image,
	".tif":   Image,
	".svg":   FileSvg,
	".mp4":   Video,
	".mov":   Video,
	".webm":  Video,
	".mkv":   Video,
	".avi":   Video,
	".m4v":   Video,
	".mp3":   FileAudio,
	".wav":   FileAudio,
	".flac":  FileAudio,
	".m4a":   FileAudio,
	".ogg":   FileAudio,
	".aac":   FileAudio,
	".opus":  FileAudio,
	".ttf":   FileFont,
	".otf":   FileFont,
	".woff":  FileFont,
	".woff2": FileFont,
	".eot":   FileFont,

	// Archive / Binary / Special extensions
	".zip":        FileArchive,
	".tar":        FileArchive,
	".gz":         FileArchive,
	".tgz":        FileArchive,
	".bz2":        FileArchive,
	".xz":         FileArchive,
	".7z":         FileArchive,
	".rar":        FileArchive,
	".exe":        FileBinary,
	".dmg":        FileBinary,
	".app":        FileBinary,
	".bin":        FileBinary,
	".iso":        FileBinary,
	".msi":        FileBinary,
	".pkg":        FileBinary,
	".deb":        FileBinary,
	".rpm":        FileBinary,
	".apk":        FileBinary,
	".dll":        FileBinary,
	".so":         FileBinary,
	".dylib":      FileBinary,
	".dockerfile": FileDocker,
	".mk":         Wrench,
	".cmake":      Wrench,
	".just":       Wrench,
	".gradle":     Wrench,
	".procfile":   Wrench,
}

var semanticFolderIcons = map[string]Icon{
	// Source
	"src":      FolderSrc,
	"lib":      FolderSrc,
	"libs":     FolderSrc,
	"app":      FolderSrc,
	"apps":     FolderSrc,
	"pkg":      FolderSrc,
	"cmd":      FolderSrc,
	"internal": FolderSrc,
	"packages": FolderSrc,
	"source":   FolderSrc,
	"core":     FolderSrc,

	// Docs
	"docs":          FolderDocs,
	"doc":           FolderDocs,
	"notes":         FolderDocs,
	"documentation": FolderDocs,
	"guides":        FolderDocs,
	"wiki":          FolderDocs,

	// Assets
	"assets":      FolderAssets,
	"asset":       FolderAssets,
	"images":      FolderAssets,
	"image":       FolderAssets,
	"img":         FolderAssets,
	"media":       FolderAssets,
	"public":      FolderAssets,
	"static":      FolderAssets,
	"resources":   FolderAssets,
	"res":         FolderAssets,
	"icons":       FolderAssets,
	"fonts":       FolderAssets,
	"attachments": FolderAssets,

	// Tests
	"test":      FolderTests,
	"tests":     FolderTests,
	"__tests__": FolderTests,
	"spec":      FolderTests,
	"specs":     FolderTests,
	"e2e":       FolderTests,
	"testdata":  FolderTests,
	"fixtures":  FolderTests,

	// Git
	".git":    FolderGit,
	".github": FolderGit,
	".gitlab": FolderGit,
	".gitea":  FolderGit,

	// Dependencies
	"node_modules": FolderDeps,
	"vendor":       FolderDeps,
	"third_party":  FolderDeps,
	"deps":         FolderDeps,
	".venv":        FolderDeps,
	"venv":         FolderDeps,
}

func baseName(name string) string {
	s := strings.ReplaceAll(name, "\\", "/")
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

// FileIcon returns the dedicated icon for a file name or path and whether a
// specific match was found; unmatched names fall back to (Code, false).
func FileIcon(name string) (Icon, bool) {
	base := baseName(name)
	if base == "" {
		return Code, false
	}

	if ico, ok := exactFileIcons[base]; ok {
		return ico, true
	}

	switch {
	case base == "readme" || strings.HasPrefix(base, "readme."):
		return FileReadme, true
	case strings.HasPrefix(base, "license.") || strings.HasPrefix(base, "licence.") ||
		strings.HasPrefix(base, "copying.") || strings.HasPrefix(base, "notice."):
		return FileLicense, true
	case strings.HasPrefix(base, "dockerfile.") || strings.HasPrefix(base, "docker-compose."):
		return FileDocker, true
	case strings.HasPrefix(base, "taskfile.") || strings.HasPrefix(base, "procfile.") || strings.HasPrefix(base, "jenkinsfile."):
		return Wrench, true
	case (strings.HasPrefix(base, "requirements-") || strings.HasPrefix(base, "requirements.") ||
		strings.HasPrefix(base, "constraints-") || strings.HasPrefix(base, "constraints.") ||
		base == "constraints.in") &&
		(strings.HasSuffix(base, ".txt") || strings.HasSuffix(base, ".in")):
		return Package, true
	case strings.HasPrefix(base, ".env."):
		return FileEnv, true
	}

	if ext := path.Ext(base); ext != "" {
		if ico, ok := extFileIcons[ext]; ok {
			return ico, true
		}
	}

	return Code, false
}

// FileIconOrDefault returns the icon for a file name or path, falling back to Code.
func FileIconOrDefault(name string) Icon {
	ico, _ := FileIcon(name)
	return ico
}

// FolderIcon returns a semantic folder icon if the directory basename matches
// a known category (regardless of expanded), or FolderOpen / Folder otherwise.
func FolderIcon(name string, expanded bool) Icon {
	base := baseName(name)
	if ico, ok := semanticFolderIcons[base]; ok {
		return ico
	}
	if expanded {
		return FolderOpen
	}
	return Folder
}
