package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"block/internal/files"
	"block/internal/model"
	"block/internal/theme"
)

const (
	maxToolNoteBytes   = 256 * 1024
	maxNotebookEntries = 2048
	maxToolPages       = 200
	maxTurnTools       = 32
	maxToolArgsBytes   = 2 * 1024 * 1024
)

type notebookScope struct {
	root         string
	page         string
	originPage   string
	pageSwitched bool
	info         os.FileInfo
}

type assistantTurn struct {
	ctx    context.Context
	cancel context.CancelFunc
	notebookScope
	convID           uint64
	serial           uint64
	cfg              model.AIProviderConfig
	thinking         string
	nativeTools      bool
	attachmentImages []map[string]any
	cliPath          string
	userIndex        int
	historyEnd       int
	questionIndex    int
	nextStep         int
	toolCalls        atomic.Uint32
}

func (turn *assistantTurn) allowTool() bool {
	return turn.ctx.Err() == nil && turn.toolCalls.Add(1) <= maxTurnTools
}

func normalizeNotebookRoot(root string) string {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return ""
	}
	if canonical, err := canonicalNotebook(trimmed); err == nil && canonical != "" {
		return filepath.Clean(canonical)
	}
	return filepath.Clean(trimmed)
}

func canonicalNotebook(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New(theme.Tr("请选择一个已登记的笔记本", "Select a registered notebook"))
	}
	return filepath.EvalSymlinks(root)
}

// AuthorizedNotebookRoot resolves a notebook against the workspace's registered roots.
func AuthorizedNotebookRoot(host model.WorkspaceHost, requested string) (string, error) {
	root, _, err := resolveNotebook(host, requested)
	return root, err
}

// Keep the registered path for workspace tabs, and use the canonical root for file access.
func resolveNotebook(host model.WorkspaceHost, requested string) (string, string, error) {
	root, err := canonicalNotebook(requested)
	if err != nil || host == nil {
		return "", "", errors.New(theme.Tr("请选择一个已登记的笔记本", "Select a registered notebook"))
	}
	active := filepath.Clean(host.ActiveNotebook())
	var workspaceRoot string
	for _, notebook := range host.Notebooks() {
		if registered, err := canonicalNotebook(notebook); err == nil && registered == root {
			path := filepath.Clean(notebook)
			if path == active {
				return root, path, nil
			}
			if workspaceRoot == "" {
				workspaceRoot = path
			}
		}
	}
	if workspaceRoot == "" {
		return "", "", errors.New(theme.Tr("笔记本未授权", "Notebook is not authorized"))
	}
	return root, workspaceRoot, nil
}

func captureNotebook(host model.WorkspaceHost) notebookScope {
	if host == nil {
		return notebookScope{}
	}
	return scopeForNotebook(host, host.ActiveNotebook())
}

func scopeForNotebook(host model.WorkspaceHost, rootPath string) notebookScope {
	if host == nil || strings.TrimSpace(rootPath) == "" {
		return notebookScope{}
	}
	root, err := AuthorizedNotebookRoot(host, rootPath)
	if err != nil {
		return notebookScope{}
	}
	dir, err := openNotebook(root)
	if err != nil {
		return notebookScope{}
	}
	defer dir.Close()
	info, err := dir.Stat(".")
	if err != nil {
		return notebookScope{}
	}
	page, _ := notePageID(root, host.ActiveNotePath())
	if name, err := noteFilename(page); err == nil {
		if _, err := inspectNote(dir, name); err != nil {
			page = ""
		}
	}
	return notebookScope{root: root, page: page, info: info}
}

func conversationPageSwitched(conv *model.Conversation, currentPage string) bool {
	if conv == nil {
		return false
	}
	origin := strings.TrimSpace(conv.OriginPage)
	if strings.TrimSpace(currentPage) != origin {
		return true
	}
	for _, m := range conv.Messages {
		if m.Role != "user" {
			continue
		}
		if strings.TrimSpace(m.ActivePage) != origin {
			return true
		}
	}
	return false
}

func missingPageIDError(scope notebookScope) string {
	if scope.pageSwitched {
		return theme.Tr(
			"会话中切换过打开页面，请明确指定 pageId，避免误改其他页面",
			"The active page changed during this conversation; pass an explicit pageId to avoid editing the wrong note",
		)
	}
	return theme.Tr(
		"当前没有默认打开的页面，请明确指定 pageId",
		"No default page is open; pass an explicit pageId",
	)
}

func formatTurnContextBlock(scope notebookScope) string {
	notebookName := "（无）"
	if scope.root != "" {
		notebookName = filepath.Base(scope.root)
	}
	originLabel := "（无当前打开页面）"
	if scope.originPage != "" {
		originLabel = scope.originPage
	}
	curLabel := "（无当前打开页面）"
	if scope.page != "" {
		curLabel = scope.page
	}
	if scope.pageSwitched || (scope.originPage != "" && scope.originPage != scope.page) {
		return fmt.Sprintf("# 当前上下文\n\n- 当前笔记本：%s\n- 会话起点页：%s\n- 本条发送时打开的页面：%s",
			notebookName, originLabel, curLabel)
	}
	return fmt.Sprintf("# 当前上下文\n\n- 当前笔记本：%s\n- 会话起点页 / 发送时打开的页面：%s",
		notebookName, curLabel)
}

func formatHistoricalUserMessage(m model.ChatMessage) string {
	text := userMessageModelText(m, false)
	if m.ActivePage != "" {
		return fmt.Sprintf("[发送时打开页面: %s]\n%s", m.ActivePage, text)
	}
	return text
}

func (scope notebookScope) open(host model.WorkspaceHost) (*os.Root, error) {
	if _, err := AuthorizedNotebookRoot(host, scope.root); err != nil {
		return nil, err
	}
	dir, err := openNotebook(scope.root)
	if err != nil {
		return nil, err
	}
	info, err := dir.Stat(".")
	if err != nil || scope.info == nil || !os.SameFile(scope.info, info) {
		dir.Close()
		return nil, errors.New(theme.Tr("笔记本位置已变化", "The notebook location has changed"))
	}
	return dir, nil
}

func referencePageID(root, path string) string {
	if root == "" || !filepath.IsAbs(path) {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !files.IsMarkdown(rel) || validateRelativePagePath(filepath.ToSlash(rel)) != nil {
		return ""
	}
	return displayPageID(rel)
}

func notePageID(root, abs string) (string, error) {
	if root == "" || !filepath.IsAbs(abs) {
		return "", errors.New(theme.Tr("页面不在授权笔记本中", "The page is not in an authorized notebook"))
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, filepath.Join(parent, filepath.Base(abs)))
	if err != nil || !files.IsMarkdown(rel) {
		return "", errors.New(theme.Tr("页面不在授权笔记本中", "The page is not in an authorized notebook"))
	}
	if err := validateRelativePagePath(filepath.ToSlash(rel)); err != nil {
		return "", err
	}
	return displayPageID(rel), nil
}

func displayPageID(path string) string {
	name := filepath.ToSlash(path)
	stem := strings.TrimSuffix(name, ".md")
	if files.IsMarkdown(stem) {
		return name
	}
	return stem
}

func noteFilename(page string) (string, error) {
	if err := validateRelativePagePath(page); err != nil {
		return "", err
	}
	name := filepath.FromSlash(page)
	if !files.IsMarkdown(name) {
		name += ".md"
	}
	return name, nil
}

func openNotebook(root string) (*os.Root, error) {
	canonical, err := canonicalNotebook(root)
	if err != nil || canonical != root {
		return nil, errors.New(theme.Tr("笔记本位置已变化", "The notebook location has changed"))
	}
	return os.OpenRoot(root)
}

// inspectNote checks every component, including the final filename, before a read or write.
func inspectNote(root *os.Root, name string) (os.FileInfo, error) {
	parts := strings.Split(filepath.ToSlash(name), "/")
	for i := range parts {
		info, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if i == len(parts)-1 && info.Mode().IsRegular() {
			return info, nil
		}
		if i == len(parts)-1 || !info.IsDir() {
			return nil, errors.New(theme.Tr("笔记路径包含链接或不受支持的文件", "The note path contains a link or an unsupported file"))
		}
	}
	return nil, os.ErrNotExist
}

func readNotebookNote(ctx context.Context, root *os.Root, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := inspectNote(root, name)
	if err != nil {
		return "", err
	}
	if info.Size() > maxToolNoteBytes {
		return "", errors.New(theme.Tr("笔记超过 AI 读取上限（256 KiB）", "The note exceeds the AI read limit (256 KiB)"))
	}
	f, err := openNoteForRead(root, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return "", errors.New(theme.Tr("笔记在读取时已变化", "The note changed while it was being read"))
	}
	data, err := io.ReadAll(io.LimitReader(f, maxToolNoteBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxToolNoteBytes || !utf8.Valid(data) {
		return "", errors.New(theme.Tr("笔记超过读取上限或不是 UTF-8", "The note exceeds the read limit or is not UTF-8"))
	}
	return string(data), ctx.Err()
}

func notebookPages(ctx context.Context, root *os.Root) (pages []string, limited bool) {
	// Read directories in small batches. WalkDir would read and sort a whole directory first.
	remaining := maxNotebookEntries
	var visit func(string, int)
	visit = func(dir string, depth int) {
		if depth > 32 || remaining == 0 || len(pages) >= maxToolPages || ctx.Err() != nil {
			limited = true
			return
		}
		d, err := openNoteForRead(root, dir)
		if err != nil {
			return
		}
		defer d.Close()
		for remaining > 0 && len(pages) < maxToolPages && ctx.Err() == nil {
			entries, err := d.ReadDir(min(remaining, 32))
			for _, entry := range entries {
				if remaining == 0 || len(pages) >= maxToolPages || ctx.Err() != nil {
					break
				}
				remaining--
				if strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				if entry.IsDir() {
					visit(path, depth+1)
				} else if files.IsMarkdown(path) && entry.Type().IsRegular() {
					if _, err := inspectNote(root, path); err == nil {
						pages = append(pages, path)
					}
				}
			}
			if err != nil {
				return
			}
		}
		limited = true
	}
	visit(".", 0)
	return pages, limited
}

func snapshotNotebookNote(ctx context.Context, root *os.Root, name string) (string, bool, error) {
	text, err := readNotebookNote(ctx, root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	return text, err == nil, err
}
