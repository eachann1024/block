package smoke

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"block/internal/app"
	"block/internal/modals"
	"block/internal/model"
	"block/internal/theme"
	blockui "block/internal/ui"
	"block/internal/updater"
	"github.com/egoist/mygo/ui"
)

const defaultSceneShotOutDir = "/Users/eachann/Work/block/tmp/shots"

// ShotSize describes an offscreen render viewport in logical pixels.
type ShotSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ProbeAnomaly records a blockui probe with non-positive dimensions or
// coordinates extending outside the rendered window viewport.
type ProbeAnomaly struct {
	ID     string  `json:"id"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	W      float32 `json:"w"`
	H      float32 `json:"h"`
	Reason string  `json:"reason"`
}

// SceneShotResult summarizes one rendered (scene, theme, size) snapshot.
type SceneShotResult struct {
	Scene      string         `json:"scene"`
	Theme      string         `json:"theme"`
	Size       string         `json:"size"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	ImagePath  string         `json:"image_path"`
	ProbeCount int            `json:"probe_count"`
	Anomalies  []ProbeAnomaly `json:"anomalies,omitempty"`
	DurationMs int64          `json:"duration_ms"`
}

// SceneShotSummary is written to summary.json in the output directory.
type SceneShotSummary struct {
	GeneratedAt  string            `json:"generated_at"`
	OutputDir    string            `json:"output_dir"`
	TotalShots   int               `json:"total_shots"`
	AnomalyCount int               `json:"anomaly_count"`
	Shots        []SceneShotResult `json:"shots"`
}

// SceneShotConfig controls offscreen scene snapshot generation.
type SceneShotConfig struct {
	Scenes []string
	Themes []string
	Sizes  []ShotSize
	OutDir string
}

// ParseSceneShotEnv reads BLOCK_SHOT_SCENES, BLOCK_SHOT_THEMES, BLOCK_SHOT_SIZES,
// and BLOCK_SHOT_OUT. If BLOCK_SHOT_SCENES is empty, ok is false.
func ParseSceneShotEnv() (SceneShotConfig, bool, error) {
	rawScenes := strings.TrimSpace(os.Getenv("BLOCK_SHOT_SCENES"))
	if rawScenes == "" {
		return SceneShotConfig{}, false, nil
	}
	scenes := splitCSVLower(rawScenes)
	if len(scenes) == 0 {
		return SceneShotConfig{}, false, nil
	}

	rawThemes := strings.TrimSpace(os.Getenv("BLOCK_SHOT_THEMES"))
	if rawThemes == "" {
		rawThemes = "light,dark"
	}
	themes := splitCSVLower(rawThemes)
	for _, th := range themes {
		if th != "light" && th != "dark" {
			return SceneShotConfig{}, false, fmt.Errorf("unsupported BLOCK_SHOT_THEMES entry %q (want light or dark)", th)
		}
	}

	rawSizes := strings.TrimSpace(os.Getenv("BLOCK_SHOT_SIZES"))
	if rawSizes == "" {
		rawSizes = "1280x800,760x560"
	}
	var sizes []ShotSize
	for _, token := range splitCSVLower(rawSizes) {
		sz, err := parseShotSize(token)
		if err != nil {
			return SceneShotConfig{}, false, err
		}
		sizes = append(sizes, sz)
	}

	outDir := strings.TrimSpace(os.Getenv("BLOCK_SHOT_OUT"))
	if outDir == "" {
		outDir = defaultSceneShotOutDir
	}

	return SceneShotConfig{
		Scenes: scenes,
		Themes: themes,
		Sizes:  sizes,
		OutDir: outDir,
	}, true, nil
}

func splitCSVLower(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseShotSize(raw string) (ShotSize, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(raw)), "x")
	if len(parts) != 2 {
		return ShotSize{}, fmt.Errorf("invalid BLOCK_SHOT_SIZES entry %q (want WxH)", raw)
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ShotSize{}, fmt.Errorf("invalid BLOCK_SHOT_SIZES dimensions %q", raw)
	}
	return ShotSize{Width: w, Height: h}, nil
}

// RunSceneShots renders every (scene, theme, size) combination offscreen with
// ui.Tester, exports PNGs and summary.json into cfg.OutDir, and checks all
// registered blockui probes for non-positive size or viewport overflow.
func RunSceneShots(baseTempDir string, cfg SceneShotConfig) (*SceneShotSummary, error) {
	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir out dir: %w", err)
	}
	if strings.TrimSpace(baseTempDir) == "" {
		tmp, err := os.MkdirTemp("", "block-scene-shot-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		baseTempDir = tmp
	}

	prevDark := theme.IsDark()
	prevLang := theme.Language()
	defer func() {
		theme.SetDark(prevDark)
		theme.SetLanguage(prevLang)
	}()
	theme.SetLanguage(model.LangZh)

	summary := &SceneShotSummary{
		GeneratedAt: time.Now().Format(time.RFC3339),
		OutputDir:   cfg.OutDir,
	}

	shotIndex := 0
	for _, scene := range cfg.Scenes {
		for _, th := range cfg.Themes {
			for _, sz := range cfg.Sizes {
				shotIndex++
				res, err := renderSingleSceneShot(baseTempDir, shotIndex, scene, th, sz, cfg.OutDir)
				if err != nil {
					return summary, err
				}
				summary.Shots = append(summary.Shots, res)
				summary.TotalShots++
				summary.AnomalyCount += len(res.Anomalies)
			}
		}
	}

	summaryPath := filepath.Join(cfg.OutDir, "summary.json")
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return summary, fmt.Errorf("marshal summary.json: %w", err)
	}
	if err := os.WriteFile(summaryPath, append(raw, '\n'), 0o644); err != nil {
		return summary, fmt.Errorf("write summary.json: %w", err)
	}
	return summary, nil
}

func renderSingleSceneShot(baseTempDir string, idx int, scene, th string, sz ShotSize, outDir string) (SceneShotResult, error) {
	t0 := time.Now()
	shotDir := filepath.Join(baseTempDir, fmt.Sprintf("shot-%02d-%s-%s-%dx%d", idx, scene, th, sz.Width, sz.Height))
	dataDir := filepath.Join(shotDir, "app-data")
	nbDir := filepath.Join(shotDir, "notebook")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return SceneShotResult{}, err
	}
	if err := os.MkdirAll(filepath.Join(nbDir, "Projects"), 0o755); err != nil {
		return SceneShotResult{}, err
	}

	sampleNotePath := filepath.Join(nbDir, "Ocean Paper.md")
	sampleContent := strings.Join([]string{
		"# Ocean Paper 设计基线",
		"",
		"这是一段用于离屏渲染与布局自检的示例笔记，包含常规段落、待办与代码块。",
		"",
		"- [x] 完成离线场景快照器",
		"- [ ] 检查亮暗色与宽窄窗几何边界",
		"",
		"```go",
		"package main",
		"",
		"func main() {",
		`    println("Ocean Paper")`,
		"}",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(sampleNotePath, []byte(sampleContent), 0o644); err != nil {
		return SceneShotResult{}, err
	}
	if err := os.WriteFile(filepath.Join(nbDir, "Projects", "Roadmap.md"), []byte("# Roadmap\n\n- Q4 Release\n"), 0o644); err != nil {
		return SceneShotResult{}, err
	}

	blockui.ResetSmoke()
	defer blockui.ResetSmoke()
	defer theme.SetLanguage(model.LangZh)

	dark := th == "dark"

	ws := app.NewWorkspace(dataDir)
	defer func() {
		if sup := ws.Plugins(); sup != nil {
			sup.ShutdownAll()
		}
	}()
	if err := ws.SetNotebook(nbDir); err != nil {
		return SceneShotResult{}, fmt.Errorf("SetNotebook: %w", err)
	}
	if err := ws.OpenNote(sampleNotePath, 0); err != nil {
		return SceneShotResult{}, fmt.Errorf("OpenNote: %w", err)
	}

	tt := ui.NewTester(ws.Render, sz.Width, sz.Height)
	theme.SetDark(dark)
	tt.SetDark(dark)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	tt.Frame()

	switch scene {
	case "workspace":
		// Already in active workspace with sample note open.
	case "settings":
		ws.OpenSettings("appearance")
	case "store":
		ws.OpenSettings(modals.TabAppStore)
	case "export":
		if !ws.OpenExportStage() {
			return SceneShotResult{}, fmt.Errorf("OpenExportStage returned false for scene %q", scene)
		}
	case "find":
		tt.Key(ui.Cmd, ui.KeyF)
		tt.Frame()
		tt.Advance(200 * time.Millisecond)
		tt.Type("Ocean")
	case "ai":
		ws.AI().SeedScenePreview(false)
		tt.Frame()
		tt.Advance(120 * time.Millisecond)
	case "ai-stream":
		ws.AI().SeedScenePreview(true)
		for i := 0; i < 12; i++ {
			tt.Frame()
			tt.Advance(20 * time.Millisecond)
		}
	case "updater-ready", "updater-downloading", "updater-installing", "updater-error", "updater-versions", "updater-ready-en", "updater-source-menu":
		if scene == "updater-ready-en" {
			theme.SetLanguage(model.LangEn)
		}
		ws.Modals().OpenUpdaterPreview(updaterShotSnapshot(scene, filepath.Join(dataDir, "logs", "updater-install.log")))
		if scene == "updater-source-menu" {
			tt.Frame()
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			pt, ok := blockui.TargetCenter("updater-source-btn")
			if !ok {
				return SceneShotResult{}, fmt.Errorf("missing updater-source-btn target for scene %q", scene)
			}
			tt.ClickAt(pt.X, pt.Y)
		}
	default:
		return SceneShotResult{}, fmt.Errorf("unknown scene %q (supported: workspace, settings, store, export, find, ai, ai-stream, updater-ready, updater-downloading, updater-installing, updater-error, updater-versions, updater-ready-en, updater-source-menu)", scene)
	}

	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	tt.Frame()

	img := tt.Image()
	if img == nil {
		return SceneShotResult{}, fmt.Errorf("ui.Tester.Image() returned nil for %s/%s/%dx%d", scene, th, sz.Width, sz.Height)
	}

	sizeLabel := fmt.Sprintf("%dx%d", sz.Width, sz.Height)
	outName := fmt.Sprintf("%s_%s_%s.png", scene, th, sizeLabel)
	outPath := filepath.Join(outDir, outName)
	f, err := os.Create(outPath)
	if err != nil {
		return SceneShotResult{}, fmt.Errorf("create %s: %w", outPath, err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return SceneShotResult{}, fmt.Errorf("encode %s: %w", outPath, err)
	}
	if err := f.Close(); err != nil {
		return SceneShotResult{}, fmt.Errorf("close %s: %w", outPath, err)
	}

	targets := blockui.AllTargets()
	anomalies := inspectProbeBounds(targets, sz.Width, sz.Height)

	return SceneShotResult{
		Scene:      scene,
		Theme:      th,
		Size:       sizeLabel,
		Width:      sz.Width,
		Height:     sz.Height,
		ImagePath:  outPath,
		ProbeCount: len(targets),
		Anomalies:  anomalies,
		DurationMs: time.Since(t0).Milliseconds(),
	}, nil
}

func inspectProbeBounds(targets map[string]ui.Rect, viewW, viewH int) []ProbeAnomaly {
	const eps = float32(0.5)
	scrollRect, hasChatScroll := targets["ai-chat-scroll"]
	var anomalies []ProbeAnomaly
	for id, r := range targets {
		var reasons []string
		if r.W <= 0 || r.H <= 0 {
			reasons = append(reasons, "non-positive size")
		}
		if hasChatScroll && isAIChatScrollChild(id) {
			if r.X < scrollRect.X-eps || r.X+r.W > scrollRect.X+scrollRect.W+eps {
				reasons = append(reasons, "exceeds scroll horizontal bounds")
			}
		} else if r.X < -eps || r.Y < -eps || r.X+r.W > float32(viewW)+eps || r.Y+r.H > float32(viewH)+eps {
			reasons = append(reasons, "exceeds viewport bounds")
		}
		if len(reasons) > 0 {
			anomalies = append(anomalies, ProbeAnomaly{
				ID:     id,
				X:      r.X,
				Y:      r.Y,
				W:      r.W,
				H:      r.H,
				Reason: strings.Join(reasons, "; "),
			})
		}
	}
	return anomalies
}

func isAIChatScrollChild(id string) bool {
	switch id {
	case "ai-user-message", "ai-assistant-message", "ai-process-card", "ai-process-toggle", "ai-process-steps", "ai-process-live":
		return true
	}
	return strings.HasPrefix(id, "ai-process-tool-")
}

func updaterShotSnapshot(scene, logPath string) updater.Snapshot {
	releases := []updater.ReleaseEntry{
		{
			Version:     "1.0.942",
			Tag:         "v1.0.942",
			PublishedAt: "2026-11-18T12:00:00Z",
			HTMLURL:     updater.PublicReleasesWebURL,
			Zh: updater.LocalizedNotes{
				Highlights:   "本次更新带来更轻盈的更新弹窗体验与更稳健的后台校验。",
				New:          []string{"图标：新增 GitHub 品牌图标"},
				Improvements: []string{"关于：简化关于对话框中的操作和仓库按钮", "标题栏：品牌菜单中移除了 GitHub 发布说明入口"},
				Fixes:        []string{"编辑器：修复只读映像下更新提示缺失的问题"},
			},
			En: updater.LocalizedNotes{
				Highlights:   "A lighter update sheet with steadier background verification.",
				New:          []string{"Icons: Added GitHub brand icon"},
				Improvements: []string{"About: Simplified action and repository buttons", "Titlebar: Removed GitHub release link from brand menu"},
				Fixes:        []string{"Editor: Fixed missing update prompt on read-only images"},
			},
		},
		{
			Version:     "1.0.941",
			Tag:         "v1.0.941",
			PublishedAt: "2026-11-16T09:30:00Z",
			HTMLURL:     updater.PublicReleasesWebURL,
			Zh: updater.LocalizedNotes{
				New:   []string{"商店：新增瀑布流卡片与一键安装"},
				Fixes: []string{"批注：修复窄窗口下批注栏宽度计算偏差"},
			},
			En: updater.LocalizedNotes{
				New:   []string{"Store: Added masonry cards and one-click install"},
				Fixes: []string{"Annotations: Fixed rail width in narrow windows"},
			},
		},
	}
	notes := updater.AggregateReleases(releases, "1.0.940", "")
	snap := updater.Snapshot{
		Status:         updater.StatusReady,
		CurrentVersion: "1.0.940",
		LatestVersion:  "1.0.942",
		PublishedAt:    "2026-11-18T12:00:00Z",
		ReleaseURL:     updater.PublicReleasesWebURL,
		SourceMode:     updater.SourceAuto,
		ActiveChannel:  "mirror",
		FetchLatencyMs: 38,
		ViewMode:       updater.ViewMerged,
		InPlace:        true,
		LogPath:        logPath,
		Notes:          notes,
	}
	switch scene {
	case "updater-downloading":
		snap.Status = updater.StatusDownloading
		snap.Percent = 42
		snap.SpeedMBps = 3.1
		snap.DownloadedBytes = 5_637_144
		snap.TotalBytes = 13_421_772
	case "updater-installing":
		snap.Status = updater.StatusInstalling
		snap.Percent = 100
	case "updater-error":
		snap.Status = updater.StatusError
		snap.SourceMode = updater.SourceMirror
		snap.FetchLatencyMs = 1240
		snap.ErrorMessage = "校验安装包失败：SHA-256 摘要不匹配，请重试或切换线路"
	case "updater-versions":
		snap.ViewMode = updater.ViewByVersion
	case "updater-ready-en":
		snap.SourceMode = updater.SourceMirror
	}
	return snap
}
