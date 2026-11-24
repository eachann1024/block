package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
)

var fileDialogBusy atomic.Bool

var ErrFileDialogBusy = errors.New("a native file dialog is already open")
var ErrFileDialogUnavailable = errors.New("native file picker unavailable")

type pluginPickerOptions struct {
	mygo.OpenDialogOptions
	save bool
}

func (opts pluginPickerOptions) defaultLocation() (dir, name string) {
	if opts.DefaultPath == "" {
		return "", ""
	}
	if info, err := os.Stat(opts.DefaultPath); err == nil && info.IsDir() {
		return opts.DefaultPath, ""
	}
	return filepath.Dir(opts.DefaultPath), filepath.Base(opts.DefaultPath)
}

func pickPluginFiles(ctx context.Context, opts pluginPickerOptions, allowed func() bool) ([]string, error) {
	if opts.Parent == nil || ctx.Err() != nil {
		return nil, context.Canceled
	}
	paths, err := showPluginPicker(ctx, opts, allowed)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err == nil && !opts.save && !opts.Directory && !dialogPathsMatchExtensions(paths, opts.Filters) {
		return nil, errors.New("selected file does not match the requested extensions")
	}
	return paths, err
}

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
// On macOS, it reuses a pre-warmed in-process AppKit NSOpenPanel sheet whenever the
// running bundle is intact, uses a pre-warmed out-of-process helper if a hot in-place
// bundle update replaced the app on disk, and falls back to osascript only as a last resort.
// Concurrent calls while a picker is already open return (nil, nil) immediately.
func OpenDialog(win *mygo.Window, opts mygo.OpenDialogOptions) ([]string, error) {
	return OpenDialogGuarded(context.Background(), win, opts, nil)
}

func OpenDialogGuarded(ctx context.Context, win *mygo.Window, opts mygo.OpenDialogOptions, allowed func() bool) ([]string, error) {
	if !mygo.App.IsReady() {
		return nil, nil
	}
	if !fileDialogBusy.CompareAndSwap(false, true) {
		if allowed != nil {
			return nil, ErrFileDialogBusy
		}
		return nil, nil
	}
	defer fileDialogBusy.Store(false)

	if win == nil {
		win = mygo.FocusedWindow()
	}
	if opts.Directory {
		opts.DefaultPath = resolveExistingDir(opts.DefaultPath)
	}

	if runtime.GOOS == "darwin" {
		return openDialogDarwin(ctx, win, opts, allowed)
	}

	opts.Parent = win
	if allowed != nil {
		return pickPluginFiles(ctx, pluginPickerOptions{OpenDialogOptions: opts}, allowed)
	}
	if ctx.Err() != nil {
		return nil, context.Canceled
	}
	paths, err := mygo.Dialog.Open(opts)
	refocusWindow(win)
	return paths, err
}

func dialogPathsMatchExtensions(paths []string, filters []mygo.FileFilter) bool {
	extensions := make(map[string]bool)
	for _, filter := range filters {
		if len(filter.MIMETypes) > 0 || len(filter.Extensions) == 0 {
			return true
		}
		for _, extension := range filter.Extensions {
			extension = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")
			if extension == "*" {
				return true
			}
			extensions[extension] = true
		}
	}
	if len(extensions) == 0 {
		return true
	}
	for _, path := range paths {
		name := strings.ToLower(filepath.Base(path))
		matched := false
		for extension := range extensions {
			if strings.HasSuffix(name, "."+extension) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// SaveDialog opens a native save-file picker safely across hot bundle updates on macOS.
// Concurrent calls while a picker is already open return ("", nil) immediately.
func SaveDialog(win *mygo.Window, opts mygo.SaveDialogOptions) (string, error) {
	return SaveDialogGuarded(context.Background(), win, opts, nil)
}

func SaveDialogGuarded(ctx context.Context, win *mygo.Window, opts mygo.SaveDialogOptions, allowed func() bool) (string, error) {
	if !mygo.App.IsReady() {
		return "", nil
	}
	if !fileDialogBusy.CompareAndSwap(false, true) {
		if allowed != nil {
			return "", ErrFileDialogBusy
		}
		return "", nil
	}
	defer fileDialogBusy.Store(false)

	if win == nil {
		win = mygo.FocusedWindow()
	}

	if runtime.GOOS == "darwin" {
		return saveDialogDarwin(ctx, win, opts, allowed)
	}

	opts.Parent = win
	if allowed != nil {
		paths, err := pickPluginFiles(ctx, pluginPickerOptions{OpenDialogOptions: mygo.OpenDialogOptions{
			Parent: win, Title: opts.Title, DefaultPath: opts.DefaultPath, Filters: opts.Filters,
			CreateDirectories: opts.CreateDirectories,
		}, save: true}, allowed)
		if len(paths) != 0 {
			return paths[0], err
		}
		return "", err
	}
	if ctx.Err() != nil {
		return "", context.Canceled
	}
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
			} else {
				return nil
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

func openDialogDarwinOsascript(ctx context.Context, opts mygo.OpenDialogOptions) ([]string, error) {
	defaultDir := resolveExistingDir(opts.DefaultPath)
	prompt := strings.TrimSpace(opts.Title)

	var script string
	args := []string{"-e"}
	multiClause := ""
	retBlock := `return POSIX path of chosen`
	if opts.Multiple {
		multiClause = " with multiple selections allowed"
		retBlock = `set out to ""
	repeat with entry in chosen
		if out is not "" then set out to out & (character id 0)
		set out to out & POSIX path of entry
	end repeat
	return out`
	}

	if opts.Directory {
		script = `on run argv
	set pPrompt to item 1 of argv
	set pDir to item 2 of argv
	set pHidden to (item 3 of argv) is "1"
	activate
	if pPrompt is "" and pDir is "" then
		set chosen to choose folder invisibles pHidden` + multiClause + `
	else if pDir is "" then
		set chosen to choose folder with prompt pPrompt invisibles pHidden` + multiClause + `
	else if pPrompt is "" then
		set chosen to choose folder default location (POSIX file pDir) invisibles pHidden` + multiClause + `
	else
		set chosen to choose folder with prompt pPrompt default location (POSIX file pDir) invisibles pHidden` + multiClause + `
	end if
	` + retBlock + `
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
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// User cancel throws AppleScript error -128.
		if strings.Contains(stderr.String(), "(-128)") {
			return nil, nil
		}
		return nil, err
	}

	paths := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\x00")
	if len(paths) != 1 && !opts.Multiple {
		return nil, errors.New("native file picker returned multiple paths")
	}
	for i, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, errors.New("native file picker returned an invalid path")
		}
		paths[i] = filepath.Clean(path)
	}
	return paths, nil
}

func saveDialogDarwinOsascript(ctx context.Context, opts mygo.SaveDialogOptions) (string, error) {
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
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script, "--", prompt, defaultDir, defaultName)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "(-128)") {
			return "", nil
		}
		return "", err
	}
	chosen := strings.TrimSuffix(stdout.String(), "\n")
	if !filepath.IsAbs(chosen) || strings.ContainsRune(chosen, 0) {
		return "", errors.New("native save picker returned an invalid path")
	}
	return filepath.Clean(chosen), nil
}
