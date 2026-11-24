package editor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"block/internal/codepreview"
	"block/internal/files"
	"block/internal/mindmap"
	"block/internal/model"
	"block/internal/theme"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxMindMapImportBytes = 32 * 1024 * 1024 // 32 MiB hard read cap for imported files

// SetMindMapConfig updates the native MindMap renderer configuration synced from
// the official block-mindmap plugin.
func (s *Session) SetMindMapConfig(cfg mindmap.Config) {
	if s == nil {
		return
	}
	s.mindMapCfg = cfg
	s.mindMapCfgSet = true
}

// SetNotifyFunc registers a host toast callback used for MindMap import/export feedback.
func (s *Session) SetNotifyFunc(fn func(msg, tone string)) {
	if s == nil {
		return
	}
	s.onNotify = fn
}

func (s *Session) effectiveMindMapConfig() mindmap.Config {
	if s != nil && s.mindMapCfgSet {
		return s.mindMapCfg
	}
	cfg := mindmap.DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func (s *Session) mindMapState(id model.BlockID) *mindmap.BlockState {
	if s.mindMapStates == nil {
		s.mindMapStates = make(map[model.BlockID]*mindmap.BlockState)
	}
	if st, ok := s.mindMapStates[id]; ok && st != nil {
		return st
	}
	st := mindmap.NewBlockState(s.effectiveMindMapConfig())
	s.mindMapStates[id] = st
	return st
}

// HasMindMapFullscreen reports whether any live MindMap block in this session is open in fullscreen.
// Any stale Fullscreen flag belonging to a deleted or converted block is cleared automatically.
func (s *Session) HasMindMapFullscreen() bool {
	if s == nil || len(s.mindMapStates) == 0 {
		return false
	}
	live := make(map[model.BlockID]bool)
	if s.doc != nil {
		for i := range s.doc.Blocks {
			b := &s.doc.Blocks[i]
			if b.Kind == model.BlockCode && codepreview.CanonicalLanguage(b.Language) == "mindmap" {
				live[b.ID] = true
			}
		}
	}
	active := false
	for id, st := range s.mindMapStates {
		if st == nil {
			continue
		}
		if !live[id] {
			st.CancelFullscreen()
			continue
		}
		if st.Fullscreen {
			active = true
		}
	}
	return active
}

// HasMindMapFocus reports whether any live MindMap block currently owns keyboard focus.
func (s *Session) HasMindMapFocus(c *ui.Context) bool {
	if s == nil || c == nil || s.doc == nil || len(s.mindMapStates) == 0 {
		return false
	}
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if b.Kind != model.BlockCode || codepreview.CanonicalLanguage(b.Language) != "mindmap" {
			continue
		}
		if st := s.mindMapStates[b.ID]; st != nil && st.HasFocus(c) {
			return true
		}
	}
	return false
}

// CloseMindMapFullscreen exits fullscreen mode for any active MindMap block.
func (s *Session) CloseMindMapFullscreen() {
	if s == nil {
		return
	}
	for id, st := range s.mindMapStates {
		if st != nil && st.IsFullscreenOverlayActive() {
			st.ExitFullscreen(s.mindMapCallbacks(nil, id, s.readOnly))
		}
	}
}

// HandleMindMapZoom handles Cmd+0 / Cmd+= / Cmd+- from the native menu bar when
// a MindMap block is open in fullscreen or currently focused in Map view.
// mode: 0 = fit, >0 = zoom in, <0 = zoom out.
func (s *Session) HandleMindMapZoom(mode int) bool {
	if s == nil || s.doc == nil || len(s.mindMapStates) == 0 {
		return false
	}
	var target *mindmap.BlockState
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if b.Kind != model.BlockCode || codepreview.CanonicalLanguage(b.Language) != "mindmap" {
			continue
		}
		st := s.mindMapStates[b.ID]
		if st == nil || st.View != mindmap.ViewMap {
			continue
		}
		if st.Fullscreen {
			target = st
			break
		}
		if st.LastCanvasFocused {
			target = st
		}
	}
	if target == nil {
		return false
	}
	switch {
	case mode == 0:
		target.FitCanvas()
	case mode > 0:
		target.ZoomBy(1.15)
	default:
		target.ZoomBy(1.0 / 1.15)
	}
	return true
}

// OpenMindMapFind opens the in-canvas find bar when a MindMap block is open in
// fullscreen or currently holds focus.
func (s *Session) OpenMindMapFind() bool {
	return s.openMindMapFind(nil)
}

func (s *Session) openMindMapFind(c *ui.Context) bool {
	if s == nil || s.doc == nil || len(s.mindMapStates) == 0 {
		return false
	}
	var target *mindmap.BlockState
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if b.Kind != model.BlockCode || codepreview.CanonicalLanguage(b.Language) != "mindmap" {
			continue
		}
		st := s.mindMapStates[b.ID]
		if st == nil {
			continue
		}
		if st.Fullscreen {
			target = st
			break
		}
		if st.LastCanvasFocused || (c != nil && st.HasFocus(c)) || (c == nil && i == s.focusedBlock && (st.EditingID != 0 || st.LinkNodeID != 0 || st.FindOpen)) {
			target = st
		}
	}
	if target == nil {
		return false
	}
	target.OpenFind()
	return true
}

func (s *Session) mindMapCallbacks(c *ui.Context, blockID model.BlockID, readOnly bool) mindmap.Callbacks {
	return mindmap.Callbacks{
		ReadOnly: readOnly,
		OnChange: func(newMarkdown string) {
			if readOnly || s.doc == nil {
				return
			}
			idx := s.blockIndexByID(blockID)
			if idx < 0 || idx >= len(s.doc.Blocks) {
				return
			}
			b := &s.doc.Blocks[idx]
			if b.Kind != model.BlockCode || codepreview.CanonicalLanguage(b.Language) != "mindmap" {
				return
			}
			if b.Code == newMarkdown {
				return
			}
			s.pushUndo()
			b.Code = newMarkdown
			b.Dirty = true
			b.CleanSource = ""
			s.markDirty()
		},
		OnUndo: func() {
			if !readOnly {
				s.Undo()
			}
		},
		OnRedo: func() {
			if !readOnly {
				s.Redo()
			}
		},
		CanUndo: s.CanUndo,
		CanRedo: s.CanRedo,
		OnOpenLink: func(target string) {
			if c != nil {
				s.openBlockLink(c.Services(), target)
			}
		},
		OnCheckLinkStatus: func(target string) mindmap.LinkStatus {
			if strings.TrimSpace(target) == "" {
				return mindmap.LinkStatusNone
			}
			if mindmap.ClassifyLink(target) == mindmap.LinkWeb {
				return mindmap.LinkStatusExternal
			}
			known, exists := s.CheckLocalLinkExists(target)
			if !known || exists {
				return mindmap.LinkStatusValid
			}
			return mindmap.LinkStatusBroken
		},
		OnListNotes: func() []mindmap.NoteCandidate {
			if s.host == nil {
				return nil
			}
			nb := s.host.ActiveNotebook()
			if nb == "" {
				return nil
			}
			entries, err := s.host.ListNotebookFiles(nb)
			if err != nil {
				return nil
			}
			noteDir := ""
			if strings.TrimSpace(s.notePath) != "" {
				noteDir = s.mediaDir()
			}
			projRoot := s.projectRootDir()
			if projRoot == "" {
				projRoot = nb
			}
			anchor := s.pathScopeSnapshot(noteDir, projRoot)
			sameTarget := func(rel, targetAbs string) bool {
				res := resolveMarkdownLocalTarget(rel, noteDir, projRoot, anchor)
				if !res.exists || res.isDir {
					return false
				}
				if filepath.Clean(res.absPath) == filepath.Clean(targetAbs) {
					return true
				}
				fiA, errA := os.Stat(res.absPath)
				fiB, errB := os.Stat(targetAbs)
				return errA == nil && errB == nil && os.SameFile(fiA, fiB)
			}
			var out []mindmap.NoteCandidate
			var walk func(list []model.FileEntry)
			walk = func(list []model.FileEntry) {
				for _, e := range list {
					if e.IsDir {
						walk(e.Children)
						continue
					}
					if !e.IsMarkdown {
						continue
					}
					nbRel, rErr := filepath.Rel(nb, e.Path)
					if rErr != nil {
						nbRel = e.RawName
					}
					displayRel := filepath.ToSlash(nbRel)
					linkRel := displayRel
					if noteDir != "" {
						if noteRel, nErr := filepath.Rel(noteDir, e.Path); nErr == nil && noteRel != "" {
							cand := filepath.ToSlash(noteRel)
							if sameTarget(cand, e.Path) {
								linkRel = cand
							}
						}
					}
					out = append(out, mindmap.NoteCandidate{
						Title:       e.Name,
						RelPath:     linkRel,
						DisplayPath: displayRel,
					})
				}
			}
			walk(entries)
			return out
		},
		OnPickImportFile: func(onImported func(root *mindmap.Node, report mindmap.ImportReport)) {
			if onImported == nil || s.doc == nil {
				return
			}
			idx := s.blockIndexByID(blockID)
			if idx < 0 || idx >= len(s.doc.Blocks) {
				return
			}
			expectedNote := s.notePath
			expectedRev := s.docRev
			expectedCode := s.doc.Blocks[idx].Code
			defaultDir := ""
			if expectedNote != "" {
				defaultDir = filepath.Dir(expectedNote)
			}
			go func() {
				paths, err := files.OpenDialog(nil, mygo.OpenDialogOptions{
					Title:       theme.Tr("导入思维导图", "Import Mind Map"),
					DefaultPath: defaultDir,
					Filters: []mygo.FileFilter{
						{
							Name:       theme.Tr("思维导图文件", "Mind Map Files"),
							Extensions: []string{"xmind", "mm", "opml", "md", "markdown"},
						},
					},
				})
				if err != nil || len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
					return
				}
				picked := paths[0]
				data, readErr := readFileBounded(picked, maxMindMapImportBytes)
				var root *mindmap.Node
				var report mindmap.ImportReport
				var parseErr error
				if readErr == nil {
					root, report, parseErr = mindmap.ImportBytes(picked, data)
				}
				mygo.RunOnMain(func() {
					if readErr != nil {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("读取文件失败：", "Failed to read file: ")+readErr.Error(), "danger")
						}
						if s.host != nil {
							s.host.RequestRedraw()
						}
						return
					}
					if parseErr != nil {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("导入失败：", "Import failed: ")+parseErr.Error(), "danger")
						}
						if s.host != nil {
							s.host.RequestRedraw()
						}
						return
					}
					// Verify session, active note, block identity/kind, and document version before applying.
					if s.doc == nil || s.notePath != expectedNote || (s.host != nil && expectedNote != "" && s.host.ActiveNotePath() != expectedNote) {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("原笔记已切换或关闭，已取消导入", "Original note was switched or closed; import cancelled"), "warning")
						}
						return
					}
					curIdx := s.blockIndexByID(blockID)
					if curIdx < 0 || curIdx >= len(s.doc.Blocks) {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("目标思维导图块已删除，已取消导入", "Target mind map block was deleted; import cancelled"), "warning")
						}
						return
					}
					curBlock := &s.doc.Blocks[curIdx]
					if curBlock.Kind != model.BlockCode || codepreview.CanonicalLanguage(curBlock.Language) != "mindmap" {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("目标块类型已变更，已取消导入", "Target block kind changed; import cancelled"), "warning")
						}
						return
					}
					if s.docRev != expectedRev || curBlock.Code != expectedCode {
						if s.onNotify != nil {
							s.onNotify(theme.Tr("导入期间导图内容已被修改，请重新导入以避免覆盖", "Mind map changed during import; retry to avoid overwriting newer edits"), "warning")
						}
						return
					}
					onImported(root, report)
					if s.host != nil {
						s.host.RequestRedraw()
					}
				})
			}()
		},
		OnSaveExportFile: func(defaultName, ext string, data []byte, onDone func(err error)) {
			ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
			if ext == "" {
				ext = "md"
			}
			fileName := defaultName
			if !strings.HasSuffix(strings.ToLower(fileName), "."+ext) {
				fileName += "." + ext
			}
			defaultPath := fileName
			if s.notePath != "" {
				defaultPath = filepath.Join(filepath.Dir(s.notePath), fileName)
			}
			go func() {
				dest, err := files.SaveDialog(nil, mygo.SaveDialogOptions{
					Title:             theme.Tr("导出思维导图", "Export Mind Map"),
					DefaultPath:       defaultPath,
					CreateDirectories: true,
					Filters: []mygo.FileFilter{
						{
							Name:       strings.ToUpper(ext),
							Extensions: []string{ext},
						},
					},
				})
				if err != nil {
					mygo.RunOnMain(func() {
						if onDone != nil {
							onDone(err)
						}
						if s.host != nil {
							s.host.RequestRedraw()
						}
					})
					return
				}
				if strings.TrimSpace(dest) == "" {
					return
				}
				if !strings.HasSuffix(strings.ToLower(dest), "."+ext) {
					dest += "." + ext
				}
				writeErr := writeFileAtomic(dest, data, 0o644)
				mygo.RunOnMain(func() {
					if onDone != nil {
						onDone(writeErr)
					}
					if s.host != nil {
						s.host.RequestRedraw()
					}
				})
			}()
		},
		OnNotify: func(msg, tone string) {
			if s.onNotify != nil {
				s.onNotify(msg, tone)
			}
		},
		OnRequestFocus: func() {
			if idx := s.blockIndexByID(blockID); idx >= 0 {
				s.focusedBlock = idx
			}
		},
	}
}

func readFileBounded(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d MiB import size limit", maxBytes/(1024*1024))
	}
	return data, nil
}

// writeFileAtomic writes data to a sibling temporary file, flushes and closes it,
// then atomically renames it into place so an interrupted/ENOSPC write never truncates
// or corrupts an existing destination file.
func writeFileAtomic(dest string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".block-mindmap-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *Session) renderMindMapCodeBlock(c *ui.Context, idx int, b *model.Block, isFocused, _ bool, readOnly bool) {
	st := s.mindMapState(b.ID)
	st.SetEditorBounds(s.editorRect)
	cfg := s.effectiveMindMapConfig()
	id := fmt.Sprintf("block-%d", b.ID)
	cb := s.mindMapCallbacks(c, b.ID, readOnly)
	st.RenderInline(c, id, b.Code, cfg, isFocused, cb)
}

func (s *Session) renderMindMapFullscreen(c *ui.Context, readOnly bool) {
	if s == nil || s.doc == nil || len(s.mindMapStates) == 0 {
		return
	}
	_ = s.HasMindMapFullscreen()
	cfg := s.effectiveMindMapConfig()
	for i := range s.doc.Blocks {
		b := &s.doc.Blocks[i]
		if b.Kind != model.BlockCode || codepreview.CanonicalLanguage(b.Language) != "mindmap" {
			continue
		}
		st := s.mindMapStates[b.ID]
		if st == nil || !st.IsFullscreenOverlayActive() {
			continue
		}
		st.SetEditorBounds(s.editorRect)
		id := fmt.Sprintf("block-%d", b.ID)
		cb := s.mindMapCallbacks(c, b.ID, readOnly)
		st.RenderFullscreen(c, id, b.Code, cfg, cb)
		return
	}
}
