package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// TreemapEntry represents a single file or grouped breakdown item inside a TreemapItem.
type TreemapEntry struct {
	Name  string
	Path  string
	Bytes uint64
	Count int
}

// TreemapItem describes one storage or breakdown category rendered inside RenderStorageTreemap.
type TreemapItem struct {
	ID        string
	ColorKey  string
	Label     string
	Detail    string
	Bytes     uint64
	FileCount int
	Color     ui.Color
	Entries   []TreemapEntry
}

// TreemapSummaryMetric is one secondary metric shown in the integrated storage header.
type TreemapSummaryMetric struct {
	Label string
	Value string
}

// TreemapSummary renders an integrated top summary bar (total storage + live metrics)
// inside the same card as the Bento storage map.
type TreemapSummary struct {
	TotalLabel string
	TotalValue string
	SubText    string
	Metrics    []TreemapSummaryMetric
}

// TreemapState tracks the active/hovered storage category for a treemap widget.
type TreemapState struct {
	mu         sync.Mutex
	selectedID string
}

// SelectedID returns the currently selected category ID.
func (s *TreemapState) SelectedID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedID
}

// SetSelectedID updates the selected category ID and reports whether it changed.
func (s *TreemapState) SetSelectedID(id string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	changed := s.selectedID != id
	if changed {
		s.selectedID = id
	}
	s.mu.Unlock()
	return changed
}

// TreemapOptions customizes probe identifiers, optional integrated header, and selection callbacks.
type TreemapOptions struct {
	CanvasKey       string
	CanvasProbeID   string
	DetailKey       string
	TileProbePrefix string
	Height          float32
	Summary         *TreemapSummary
	OnSelect        func(id string)
}

func resolveTileColorKey(it TreemapItem) string {
	if strings.TrimSpace(it.ColorKey) != "" {
		return it.ColorKey
	}
	return it.ID
}

// RenderStorageTreemap draws the D1-PastelMist single-card Bento storage distribution:
// optional integrated summary header + 2D proportional low-saturation pastel Bento blocks
// (each category has its own soft pastel surface, border, and harmonized text, zero left bars,
// zero bottom pill tags) + synchronized同色系 inline granular breakdown list.
func RenderStorageTreemap(
	c *ui.Context,
	state *TreemapState,
	items []TreemapItem,
	totalBytes uint64,
	opts TreemapOptions,
) {
	if state == nil || len(items) == 0 {
		return
	}
	pal := theme.Ocean()
	canvasKey := opts.CanvasKey
	if canvasKey == "" {
		canvasKey = "treemap-canvas"
	}
	detailKey := opts.DetailKey
	if detailKey == "" {
		detailKey = canvasKey + "-detail"
	}
	tilePrefix := opts.TileProbePrefix
	if tilePrefix == "" {
		tilePrefix = canvasKey + "-tile-"
	}

	// Sort descending by physical bytes so the heaviest category always occupies the hero block.
	ranked := make([]TreemapItem, len(items))
	copy(ranked, items)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Bytes != ranked[j].Bytes {
			return ranked[i].Bytes > ranked[j].Bytes
		}
		return ranked[i].FileCount > ranked[j].FileCount
	})

	selectedID := state.SelectedID()
	var active *TreemapItem
	for i := range ranked {
		if ranked[i].ID == selectedID {
			active = &ranked[i]
			break
		}
	}
	if active == nil {
		active = &ranked[0]
		selectedID = active.ID
	}

	activeTC := pal.PerfTileColors(resolveTileColorKey(*active))

	selectID := func(id string) {
		if state.SetSelectedID(id) && opts.OnSelect != nil {
			opts.OnSelect(id)
		}
	}

	ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space16).Children(func() {
		if opts.Summary != nil {
			renderTreemapSummaryHeader(c, opts.Summary)
		}

		// 2D Bento pastel storage blocks (D1-PastelMist geometry: 1.42fr : 1fr, 292px tall).
		bentoH := opts.Height
		if bentoH <= 0 {
			bentoH = 292
		}
		bento := ui.Row(c.Key(canvasKey)).FillWidth().MinWidth(0).Height(bentoH).Gap(theme.Space10).AlignItems(ui.Stretch)
		if opts.CanvasProbeID != "" {
			Probe(bento, opts.CanvasProbeID)
		}
		bento.Children(func() {
			if len(ranked) == 1 {
				renderBentoTile(c, ranked[0], totalBytes, selectedID, tilePrefix, bentoTierHero, selectID)
				return
			}

			// Left column (1.42fr): #1 largest category hero tile.
			ui.Column(c).Grow(142).Basis(0).MinWidth(0).FillHeight().Children(func() {
				renderBentoTile(c, ranked[0], totalBytes, selectedID, tilePrefix, bentoTierHero, selectID)
			})

			// Right column (1fr): stacked tiers (#2 top 108px, #3-#4 flex middle, #5-#7 bottom 68px).
			rest := ranked[1:]
			ui.Column(c).Grow(100).Basis(0).MinWidth(0).FillHeight().Gap(theme.Space10).Children(func() {
				if len(rest) == 1 {
					renderBentoTile(c, rest[0], totalBytes, selectedID, tilePrefix, bentoTierSecondary, selectID)
					return
				}

				ui.Row(c).FillWidth().MinWidth(0).Height(108).Shrink(0).Children(func() {
					renderBentoTile(c, rest[0], totalBytes, selectedID, tilePrefix, bentoTierSecondary, selectID)
				})

				midEnd := min(len(rest), 3)
				mid := rest[1:midEnd]
				bot := rest[midEnd:]

				if len(mid) > 0 {
					ui.Row(c).FillWidth().MinWidth(0).Grow(1).Gap(theme.Space10).AlignItems(ui.Stretch).Children(func() {
						for _, it := range mid {
							item := it
							ui.Column(c).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
								renderBentoTile(c, item, totalBytes, selectedID, tilePrefix, bentoTierMedium, selectID)
							})
						}
					})
				}

				if len(bot) > 0 {
					ui.Row(c).FillWidth().MinWidth(0).Height(68).Shrink(0).Gap(theme.Space10).AlignItems(ui.Stretch).Children(func() {
						for bi, it := range bot {
							item := it
							growW := float32(100)
							if bi == 0 && len(bot) == 3 {
								growW = 125
							}
							ui.Column(c).Grow(growW).Basis(0).MinWidth(0).FillHeight().Children(func() {
								renderBentoTile(c, item, totalBytes, selectedID, tilePrefix, bentoTierCompact, selectID)
							})
						}
					})
				}
			})
		})

		// Synchronized pastel-tinted inline breakdown section (no bottom pill tags).
		ui.Column(c.Key(detailKey)).FillWidth().MinWidth(0).
			Padding(theme.Space16, 0, 0, 0).Gap(theme.Space10).
			BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(theme.Space12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(theme.Space8).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
					dot := ui.Box(c).Size(theme.Space8, theme.Space8).Shrink(0).Radius(theme.RadiusPill)
					dot.Background(dot.AnimateColor("treemapDot-"+canvasKey, activeTC.Dot, FadeColor))
					ui.Text(c, active.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(0).SingleLine()
					meta := active.Detail
					if active.FileCount > 0 {
						countLbl := fmt.Sprintf(theme.Tr("共 %d 个文件", "%d files"), active.FileCount)
						if meta != "" {
							meta = meta + " · " + countLbl
						} else {
							meta = countLbl
						}
					}
					if meta != "" {
						ui.Text(c, meta).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
					}
				})
				ui.Text(c, fmt.Sprintf("%s (%s)", FormatBytesIEC(active.Bytes), FormatPercent(active.Bytes, totalBytes))).
					Font("monospace").FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).TextColor(pal.Text).Shrink(0).SingleLine()
			})

			if len(active.Entries) == 0 {
				ui.Text(c, theme.Tr("暂无文件占用", "No files in this category")).
					FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).TextColor(pal.TextSecondary)
				return
			}

			var maxEntryBytes uint64
			for _, ent := range active.Entries {
				if ent.Bytes > maxEntryBytes {
					maxEntryBytes = ent.Bytes
				}
			}

			var shownBytes uint64
			var shownCount int
			ui.Column(c.Key(detailKey + "-entries")).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
				for i, ent := range active.Entries {
					e := ent
					shownBytes += e.Bytes
					cCount := e.Count
					if cCount <= 0 {
						cCount = 1
					}
					shownCount += cCount
					displayName := strings.TrimSpace(e.Name)
					if displayName == "" {
						displayName = e.Path
					}
					tip := strings.TrimSpace(e.Path)
					if tip == "" {
						tip = displayName
					}
					ratio := float32(0)
					if maxEntryBytes > 0 && e.Bytes > 0 {
						ratio = float32(e.Bytes) / float32(maxEntryBytes)
						if ratio < 0.04 {
							ratio = 0.04
						}
						if ratio > 1 {
							ratio = 1
						}
					}

					row := ui.Row(c.Key(fmt.Sprintf("%s-ent-%d", detailKey, i))).FillWidth().MinWidth(0).
						Height(theme.HeightControl).PaddingX(theme.Space10).Gap(theme.Space12).
						Radius(theme.RadiusSM).Background(pal.Sidebar).
						AlignItems(ui.Center).Justify(ui.SpaceBetween)
					if ratio > 0 {
						rCopy := ratio
						barColor := activeTC.BarBG
						row.Draw(func(p *ui.Painter, bounds ui.Rect) {
							w := bounds.W * rCopy
							if w >= 2 {
								p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, W: w, H: bounds.H}, barColor, theme.RadiusSM)
							}
						})
					}
					row.Children(func() {
						ui.Row(c).Gap(theme.Space6).AlignItems(ui.Center).Grow(1).MinWidth(0).Children(func() {
							nameEl := ui.Text(c, displayName).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
								TextColor(pal.Text).MinWidth(0).Shrink(1).SingleLine().Ellipsis("…")
							WrappedTooltip(c, nameEl, tip)
							if e.Count > 1 {
								ui.Text(c, fmt.Sprintf(theme.Tr("· %d 项", "· %d files"), e.Count)).
									FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
									TextColor(pal.TextSecondary).Shrink(0).SingleLine()
							}
						})
						ui.Text(c, fmt.Sprintf("%s · %s", FormatBytesIEC(e.Bytes), FormatPercent(e.Bytes, active.Bytes))).
							Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).Shrink(0).SingleLine()
					})
				}

				remFiles := active.FileCount - shownCount
				var remBytes uint64
				if active.Bytes > shownBytes {
					remBytes = active.Bytes - shownBytes
				}
				if remFiles > 0 || remBytes > 0 {
					remLabel := theme.Tr("其余文件", "Other files")
					if remFiles > 0 {
						remLabel = fmt.Sprintf(theme.Tr("其余 %d 个文件", "%d other files"), remFiles)
					}
					ui.Row(c.Key(detailKey + "-ent-rest")).FillWidth().MinWidth(0).
						Padding(theme.Space4, theme.Space10, 0, theme.Space10).Gap(theme.Space12).
						AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, remLabel).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).Grow(1).MinWidth(0).SingleLine()
						ui.Text(c, fmt.Sprintf("%s · %s", FormatBytesIEC(remBytes), FormatPercent(remBytes, active.Bytes))).
							Font("monospace").FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).Shrink(0).SingleLine()
					})
				}
			})
		})
	})
}

func renderTreemapSummaryHeader(c *ui.Context, sum *TreemapSummary) {
	pal := theme.Ocean()
	ui.Row(c.Key("treemap-summary-header")).FillWidth().MinWidth(0).Gap(theme.Space16).
		AlignItems(ui.End).Justify(ui.SpaceBetween).Wrap().Children(func() {
		ui.Column(c).Gap(theme.Space2).MinWidth(0).Children(func() {
			if strings.TrimSpace(sum.TotalLabel) != "" {
				ui.Text(c, sum.TotalLabel).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(pal.TextSecondary).SingleLine()
			}
			ui.Row(c).Gap(theme.Space10).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, sum.TotalValue).Font("monospace").FontSize(theme.FontPage).FixedLineHeight(theme.LinePage).
					FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
				if strings.TrimSpace(sum.SubText) != "" {
					ui.Text(c, sum.SubText).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(pal.TextSecondary).SingleLine()
				}
			})
		})

		if len(sum.Metrics) > 0 {
			ui.Row(c).Gap(theme.Space20).AlignItems(ui.Center).Wrap().Children(func() {
				for i, m := range sum.Metrics {
					if i > 0 {
						ui.Box(c).Size(1, theme.HeightCompact).Background(pal.Hairline)
					}
					ui.Column(c).Gap(theme.Space2).AlignItems(ui.End).Children(func() {
						ui.Text(c, m.Label).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(pal.TextSecondary).SingleLine()
						ui.Text(c, m.Value).Font("monospace").FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
							FontWeight(theme.WeightControl).TextColor(pal.Text).SingleLine()
					})
				}
			})
		}
	})
}

type bentoTier int

const (
	bentoTierHero bentoTier = iota
	bentoTierSecondary
	bentoTierMedium
	bentoTierCompact
)

func renderBentoTile(
	c *ui.Context,
	item TreemapItem,
	totalBytes uint64,
	selectedID string,
	tilePrefix string,
	tier bentoTier,
	onSelect func(id string),
) {
	pal := theme.Ocean()
	tc := pal.PerfTileColors(resolveTileColorKey(item))
	isSelected := item.ID == selectedID
	tileID := tilePrefix + item.ID

	padY, padX := theme.Space16, theme.Space16
	switch tier {
	case bentoTierHero:
		padY, padX = theme.Space20, theme.Space20
	case bentoTierSecondary:
		padY, padX = theme.Space12, theme.Space16
	case bentoTierMedium:
		padY, padX = theme.Space12, theme.Space12
	case bentoTierCompact:
		padY, padX = theme.Space10, theme.Space12
	}

	btn := ui.ButtonBase(c.Key(tileID)).FillWidth().FillHeight().MinWidth(0).
		Padding(padY, padX, padY, padX).Radius(theme.RadiusLG).
		Cursor(ui.CursorPointer).Label(item.Label)

	hovered := btn.Hovered()
	pressed := btn.Pressed()

	bg := tc.BG
	bd := tc.Border
	if isSelected || pressed || hovered {
		bg = tc.HotBG
		bd = tc.HotBorder
	}

	AnimateSurfaceColors(btn, bg, bd, true)
	if isSelected {
		btn.Border(1.5, bd)
	}
	TactileCard(btn, hovered, pressed, isSelected, false)
	Probe(btn, tileID)

	btn.Children(func() {
		ui.Column(c).FillWidth().FillHeight().MinWidth(0).Justify(ui.SpaceBetween).Children(func() {
			switch tier {
			case bentoTierHero:
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space6).Children(func() {
					ui.Text(c, item.Label).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
					ui.Text(c, FormatBytesIEC(item.Bytes)).Font("monospace").
						FontSize(theme.FontPage).FixedLineHeight(theme.LinePage).
						FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
					ui.Text(c, FormatPercent(item.Bytes, totalBytes)).Font("monospace").
						FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
						TextColor(tc.SubText).SingleLine()
				})
				if item.FileCount > 0 {
					ui.Text(c, fmt.Sprintf(theme.Tr("%d 个文件", "%d files"), item.FileCount)).
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(tc.SubText).SingleLine()
				}

			case bentoTierSecondary:
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space4).Children(func() {
					ui.Text(c, item.Label).FontSize(theme.FontTitle).FixedLineHeight(theme.LineTitle).
						FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
					ui.Text(c, FormatBytesIEC(item.Bytes)).Font("monospace").
						FontSize(theme.FontHeading).FixedLineHeight(theme.LineHeading).
						FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
				})
				ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(theme.Space8).Children(func() {
					ui.Text(c, FormatPercent(item.Bytes, totalBytes)).Font("monospace").
						FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(tc.SubText).SingleLine()
					if item.FileCount > 0 {
						ui.Text(c, fmt.Sprintf(theme.Tr("%d 项", "%d items"), item.FileCount)).
							FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
							TextColor(tc.SubText).SingleLine()
					}
				})

			case bentoTierMedium:
				ui.Text(c, item.Label).FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
					FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
				ui.Column(c).FillWidth().MinWidth(0).Gap(theme.Space2).Children(func() {
					ui.Text(c, FormatBytesIEC(item.Bytes)).Font("monospace").
						FontSize(theme.FontBody).FixedLineHeight(theme.LineBody).
						FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
					sub := FormatPercent(item.Bytes, totalBytes)
					if item.FileCount > 0 {
						sub = fmt.Sprintf(theme.Tr("%s · %d 项", "%s · %d"), sub, item.FileCount)
					}
					ui.Text(c, sub).FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
						TextColor(tc.SubText).SingleLine().Ellipsis("…")
				})

			case bentoTierCompact:
				ui.Text(c, item.Label).FontSize(theme.FontSmall).FixedLineHeight(theme.LineSmall).
					FontWeight(theme.WeightControl).TextColor(tc.TitleText).SingleLine().Ellipsis("…")
				ui.Text(c, FormatBytesIEC(item.Bytes)).Font("monospace").
					FontSize(theme.FontMeta).FixedLineHeight(theme.LineMeta).
					TextColor(tc.SubText).SingleLine().Ellipsis("…")
			}
		})
	})

	if btn.Clicked() || hovered {
		onSelect(item.ID)
	}
}

// FormatBytesIEC formats byte counts using binary IEC units (B, KiB, MiB, GiB, TiB).
func FormatBytesIEC(b uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	val := float64(b)
	idx := 0
	for val >= 1024.0 && idx < len(units)-1 {
		val /= 1024.0
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f %s", val, units[idx])
}

// FormatPercent formats a part/total ratio as a percentage string.
func FormatPercent(b, total uint64) string {
	if total == 0 {
		return "0.0%"
	}
	pct := float64(b) / float64(total) * 100.0
	if b > 0 && pct < 0.01 {
		return "<0.01%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}
