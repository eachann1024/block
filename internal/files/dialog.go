package files

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
)

var fileDialogBusy atomic.Bool

func refocusWindow(win *mygo.Window) {
	if !mygo.App.IsReady() {
		return
	}
	mygo.RunOnMain(func() {
		mygo.App.Focus()
		if win != nil {
			win.Focus()
		}
	})
}

// OpenDialog opens a native file or directory picker without blocking the UI main thread.
// On macOS, it uses the in-process AppKit NSOpenPanel sheet (fast, non-blocking) whenever
// the running bundle is intact, and falls back to out-of-process osascript only if a hot
// in-place bundle update replaced the app on disk while this process stayed running.
// Concurrent calls while a picker is already open return (nil, nil) immediately.
func OpenDialog(win *mygo.Window, opts mygo.OpenDialogOptions) ([]string, error) {
	if !mygo.App.IsReady() {
		return nil, nil
	}
	if !fileDialogBusy.CompareAndSwap(false, true) {
		return nil, nil
	}
	defer fileDialogBusy.Store(false)

	if win == nil {
		win = mygo.FocusedWindow()
	}
	if opts.Directory {
		opts.DefaultPath = resolveExistingDir(opts.DefaultPath)
	}

	if runtime.GOOS == "darwin" && !darwinBundleIntact() {
		if _, err := os.Stat("/usr/bin/osascript"); err == nil {
			paths, osaErr := openDialogDarwinOsascript(opts)
			refocusWindow(win)
			return paths, osaErr
		}
	}

	opts.Parent = win
	paths, err := mygo.Dialog.Open(opts)
	refocusWindow(win)
	return paths, err
}

// SaveDialog opens a native save-file picker safely across hot bundle updates on macOS.
// Concurrent calls while a picker is already open return ("", nil) immediately.
func SaveDialog(win *mygo.Window, opts mygo.SaveDialogOptions) (string, error) {
	if !mygo.App.IsReady() {
		return "", nil
	}
	if !fileDialogBusy.CompareAndSwap(false, true) {
		return "", nil
	}
	defer fileDialogBusy.Store(false)

	if win == nil {
		win = mygo.FocusedWindow()
	}

	if runtime.GOOS == "darwin" && !darwinBundleIntact() {
		if _, err := os.Stat("/usr/bin/osascript"); err == nil {
			chosen, osaErr := saveDialogDarwinOsascript(opts)
			refocusWindow(win)
			return chosen, osaErr
		}
	}

	opts.Parent = win
	chosen, err := mygo.Dialog.Save(opts)
	refocusWindow(win)
	return chosen, err
}

func resolveExistingDir(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	cur := filepath.Clean(p)
	for {
		if st, err := os.Stat(cur); err == nil {
			if st.IsDir() {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func appleScriptUTIsForFilters(filters []mygo.FileFilter) []string {
	if len(filters) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	add := func(uti string) {
		if uti != "" && !seen[uti] {
			seen[uti] = true
		}
	}
	extUTI := map[string]string{
		"png":      "public.png",
		"jpg":      "public.jpeg",
		"jpeg":     "public.jpeg",
		"gif":      "com.compuserve.gif",
		"webp":     "org.webmproject.webp",
		"svg":      "public.svg-image",
		"bmp":      "com.microsoft.bmp",
		"ico":      "com.microsoft.ico",
		"avif":     "public.avif",
		"heic":     "public.heic",
		"heif":     "public.heif",
		"tif":      "public.tiff",
		"tiff":     "public.tiff",
		"pdf":      "com.adobe.pdf",
		"md":       "net.daringfireball.markdown",
		"markdown": "net.daringfireball.markdown",
		"txt":      "public.plain-text",
		"html":     "public.html",
		"json":     "public.json",
		"zip":      "public.zip-archive",
	}
	hasImage := false
	for _, f := range filters {
		for _, m := range f.MIMETypes {
			ml := strings.ToLower(strings.TrimSpace(m))
			if ml == "*/*" || ml == "*" {
				return nil
			}
			if strings.HasPrefix(ml, "image/") {
				hasImage = true
			}
		}
		for _, e := range f.Extensions {
			el := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
			if el == "*" {
				return nil
			}
			switch el {
			case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif", "heic", "heif", "tif", "tiff":
				hasImage = true
			}
			if uti, ok := extUTI[el]; ok {
				add(uti)
			}
		}
	}
	if hasImage {
		add("public.image")
		add("public.svg-image")
	}
	out := make([]string, 0, len(seen))
	for uti := range seen {
		out = append(out, uti)
	}
	return out
}

func openDialogDarwinOsascript(opts mygo.OpenDialogOptions) ([]string, error) {
	defaultDir := resolveExistingDir(opts.DefaultPath)
	prompt := strings.TrimSpace(opts.Title)

	var script string
	args := []string{"-e"}

	if opts.Directory {
		script = `on run argv
	set pPrompt to item 1 of argv
	set pDir to item 2 of argv
	set pHidden to (item 3 of argv) is "1"
	activate
	if pPrompt is "" and pDir is "" then
		set d to choose folder invisibles pHidden
	else if pDir is "" then
		set d to choose folder with prompt pPrompt invisibles pHidden
	else if pPrompt is "" then
		set d to choose folder default location (POSIX file pDir) invisibles pHidden
	else
		set d to choose folder with prompt pPrompt default location (POSIX file pDir) invisibles pHidden
	end if
	return POSIX path of d
end run`
		hiddenFlag := "0"
		if opts.ShowHiddenFiles {
			hiddenFlag = "1"
		}
		args = append(args, script, "--", prompt, defaultDir, hiddenFlag)
	} else {
		utis := appleScriptUTIsForFilters(opts.Filters)
		typeClause := ""
		if len(utis) > 0 {
			quoted := make([]string, len(utis))
			for i, u := range utis {
				quoted[i] = `"` + strings.ReplaceAll(u, `"`, `\"`) + `"`
			}
			typeClause = " of type {" + strings.Join(quoted, ", ") + "}"
		}
		multiClause := ""
		if opts.Multiple {
			multiClause = " with multiple selections allowed"
		}
		retBlock := `return POSIX path of chosen`
		if opts.Multiple {
			retBlock = `set out to ""
	repeat with f in chosen
		set out to out & POSIX path of f & linefeed
	end repeat
	return out`
		}
		script = `on run argv
	set pPrompt to item 1 of argv
	set pDir to item 2 of argv
	set pHidden to (item 3 of argv) is "1"
	activate
	if pPrompt is "" and pDir is "" then
		set chosen to choose file` + typeClause + ` invisibles pHidden` + multiClause + `
	else if pDir is "" then
		set chosen to choose file` + typeClause + ` with prompt pPrompt invisibles pHidden` + multiClause + `
	else if pPrompt is "" then
		set chosen to choose file` + typeClause + ` default location (POSIX file pDir) invisibles pHidden` + multiClause + `
	else
		set chosen to choose file` + typeClause + ` with prompt pPrompt default location (POSIX file pDir) invisibles pHidden` + multiClause + `
	end if
	` + retBlock + `
end run`
		hiddenFlag := "0"
		if opts.ShowHiddenFiles {
			hiddenFlag = "1"
		}
		args = append(args, script, "--", prompt, defaultDir, hiddenFlag)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("/usr/bin/osascript", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// User cancel throws AppleScript error -128.
		if strings.Contains(stderr.String(), "(-128)") {
			return nil, nil
		}
		return nil, err
	}

	var paths []string
	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\r\n"), "\n") {
		p := strings.TrimRight(line, "\r")
		if p == "" {
			continue
		}
		paths = append(paths, filepath.Clean(p))
	}
	return paths, nil
}

func saveDialogDarwinOsascript(opts mygo.SaveDialogOptions) (string, error) {
	defaultDir := ""
	defaultName := ""
	if strings.TrimSpace(opts.DefaultPath) != "" {
		clean := filepath.Clean(strings.TrimSpace(opts.DefaultPath))
		if st, err := os.Stat(clean); err == nil && st.IsDir() {
			defaultDir = clean
		} else {
			defaultDir = resolveExistingDir(filepath.Dir(clean))
			defaultName = filepath.Base(clean)
		}
	}
	if defaultName != "" && filepath.Ext(defaultName) == "" && len(opts.Filters) > 0 && len(opts.Filters[0].Extensions) > 0 {
		ext := strings.TrimPrefix(strings.TrimSpace(opts.Filters[0].Extensions[0]), ".")
		if ext != "" && ext != "*" {
			defaultName += "." + ext
		}
	}
	prompt := strings.TrimSpace(opts.Title)

	script := `on run argv
	set pPrompt to item 1 of argv
	set pDir to item 2 of argv
	set pName to item 3 of argv
	activate
	if pPrompt is "" and pDir is "" and pName is "" then
		set f to choose file name
	else if pDir is "" and pName is "" then
		set f to choose file name with prompt pPrompt
	else if pPrompt is "" and pDir is "" then
		set f to choose file name default name pName
	else if pPrompt is "" and pName is "" then
		set f to choose file name default location (POSIX file pDir)
	else if pDir is "" then
		set f to choose file name with prompt pPrompt default name pName
	else if pPrompt is "" then
		set f to choose file name default location (POSIX file pDir) default name pName
	else if pName is "" then
		set f to choose file name with prompt pPrompt default location (POSIX file pDir)
	else
		set f to choose file name with prompt pPrompt default location (POSIX file pDir) default name pName
	end if
	return POSIX path of f
end run`

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("/usr/bin/osascript", "-e", script, "--", prompt, defaultDir, defaultName)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "(-128)") {
			return "", nil
		}
		return "", err
	}
	chosen := strings.TrimRight(stdout.String(), "\r\n")
	if chosen == "" {
		return "", nil
	}
	return filepath.Clean(chosen), nil
}
