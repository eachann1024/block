package app

import (
	"fmt"
	"sort"
	"strings"

	"block/internal/fuzzy"
	"block/internal/icons"
	"block/internal/plugins"
	"block/internal/plugins/pluginui"
	"block/internal/theme"
	blockui "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

const (
	commandPaletteWidth     float32 = 560
	commandPaletteMaxHeight float32 = 520
	commandPaletteHeaderH   float32 = 52
	commandPaletteFooterH   float32 = 36
	commandPaletteEmptyH    float32 = 160
	commandPaletteMinBodyH  float32 = 120
)

// PaletteItem represents a single executable entry inside CommandPalette.
type PaletteItem struct {
	ID       string
	Title    string
	Subtitle string
	Shortcut string
	Icon     *plugins.DuoIconSpec
}

// CommandPalette manages state and rendering for the centered command palette modal.
type CommandPalette struct {
	open           bool
	query          string
	lastQuery      string
	selected       int
	revealSelected bool
	focusPending   bool
	inputHandle    ui.Handle
	scroll         ui.ScrollState

	cacheValid    bool
	cacheQuery    string
	cacheCount    int
	cacheHash     uint64
	cacheFiltered []PaletteItem
}

func hashPaletteItems64(items []PaletteItem) uint64 {
	const (
		offset uint64 = 14695981039346656037
		prime  uint64 = 1099511628211
	)
	h := offset
	feed := func(s string) {
		for i := range len(s) {
			h ^= uint64(s[i])
			h *= prime
		}
		h ^= 0xff
		h *= prime
	}
	for i := range items {
		feed(items[i].ID)
		feed(items[i].Title)
		feed(items[i].Subtitle)
		feed(items[i].Shortcut)
	}
	return h
}

// Open opens the command palette and resets filter and selection state.
func (p *CommandPalette) Open() {
	p.open = true
	p.query = ""
	p.lastQuery = ""
	p.selected = 0
	p.revealSelected = false
	p.focusPending = true
	p.cacheValid = false
}

// Close closes the command palette and clears the active filter.
func (p *CommandPalette) Close() {
	p.open = false
	p.query = ""
	p.lastQuery = ""
	p.selected = 0
	p.revealSelected = false
	p.focusPending = false
	p.cacheValid = false
	p.cacheFiltered = nil
}

func (p *CommandPalette) filteredItems(items []PaletteItem) []PaletteItem {
	ih := hashPaletteItems64(items)
	if p.cacheValid && p.cacheQuery == p.query && p.cacheCount == len(items) && p.cacheHash == ih {
		return p.cacheFiltered
	}
	filtered := filterPaletteItems(items, p.query)
	p.cacheValid = true
	p.cacheQuery = p.query
	p.cacheCount = len(items)
	p.cacheHash = ih
	p.cacheFiltered = filtered
	return filtered
}

// IsOpen reports whether the command palette is currently visible.
func (p *CommandPalette) IsOpen() bool {
	return p.open
}

// Render draws the command palette modal when open and invokes onRun when an item is chosen.
func (p *CommandPalette) Render(c *ui.Context, items []PaletteItem, onRun func(PaletteItem)) {
	if !p.open {
		return
	}

	pal := theme.Ocean()
	filtered := p.filteredItems(items)
	if len(filtered) == 0 {
		p.selected = 0
	} else if p.selected < 0 {
		p.selected = 0
	} else if p.selected >= len(filtered) {
		p.selected = len(filtered) - 1
	}

	_, vpH := c.Size()
	maxCardH := commandPaletteMaxHeight
	if vpH > 0 {
		maxCardH = min(commandPaletteMaxHeight, max(vpH-40, commandPaletteHeaderH+commandPaletteFooterH+commandPaletteMinBodyH))
	}
	maxBodyH := max(maxCardH-commandPaletteHeaderH-commandPaletteFooterH-2, commandPaletteMinBodyH)

	runSelected := func() {
		if p.query != p.lastQuery {
			p.lastQuery = p.query
			p.selected = 0
			p.scroll.Y = 0
			filtered = p.filteredItems(items)
		}
		if len(filtered) == 0 || p.selected < 0 || p.selected >= len(filtered) {
			return
		}
		chosen := filtered[p.selected]
		p.Close()
		if onRun != nil {
			onRun(chosen)
		}
	}

	stepSelection := func(delta int) {
		if len(filtered) == 0 {
			return
		}
		p.selected = (p.selected + delta + len(filtered)) % len(filtered)
		p.revealSelected = true
	}

	blockui.Dialog(c, "command-palette", theme.Tr("命令面板", "Command Palette"), &p.open, blockui.ModalLayout{
		Width:     commandPaletteWidth,
		MaxHeight: maxCardH,
		Centered:  true,
	}, func() {
		if p.focusPending {
			p.inputHandle.Focus()
			p.focusPending = false
		}

		var in ui.Element
		ui.Row(c.Key("command-palette-header")).FillWidth().Height(commandPaletteHeaderH).Shrink(0).
			Padding(0, theme.Space16, 0, theme.Space16).Gap(theme.Space10).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(pal.Hairline).
			Children(func() {
				icons.Draw(c, icons.FindSearch, theme.IconRow, pal.DuoIconColors(pal.TextSecondary))
				ui.Box(c.Key("command-palette-input-wrap")).Grow(1).MinWidth(0).Height(30).Clip().Children(func() {
					in = ui.TextInputBase(c.Key("command-palette-input"), &p.query).
						FillWidth().Height(30).Padding(0).
						Label(theme.Tr("命令面板", "Command Palette")).
						FontSize(theme.FontEditor).FixedLineHeight(theme.LineTitle).
						TextColor(pal.Text).CaretColor(pal.Accent).
						Bind(&p.inputHandle).AutoFocus()
					blockui.Probe(in, "command-palette-input")

					if p.query == "" && !in.Composing() {
						ui.Row(c.Key("command-palette-placeholder")).
							Absolute().Top(0).Bottom(0).Left(theme.Space2).Right(0).
							AlignItems(ui.Center).IgnoreInput(true).
							Children(func() {
								ui.Text(c, theme.Tr("输入命令名称…", "Type a command…")).
									FontSize(theme.FontEditor).FixedLineHeight(theme.LineTitle).
									TextColor(pal.Placeholder).SingleLine()
							})
					}
				})
			})

		if in.Valid() {
			composing := in.Composing()
			in.OnTextKey(func(k ui.TextKey) bool {
				if composing {
					return false
				}
				switch k.Key {
				case ui.KeyDown:
					stepSelection(1)
					return true
				case ui.KeyUp:
					stepSelection(-1)
					return true
				case ui.KeyEnter:
					runSelected()
					return true
				case ui.KeyEscape:
					p.Close()
					return true
				case ui.KeyW:
					if k.Mods == ui.Cmd {
						p.Close()
						return true
					}
				}
				return false
			})
			if in.Changed() || p.query != p.lastQuery {
				p.lastQuery = p.query
				p.selected = 0
				p.scroll.Y = 0
				p.revealSelected = true
				filtered = p.filteredItems(items)
			}
			if !in.Composing() && in.Submitted() {
				runSelected()
			}
		}

		if len(filtered) == 0 {
			emptyMsg := theme.Tr("没有匹配的命令", "No matching commands")
			if len(items) == 0 {
				emptyMsg = theme.Tr("暂无可用命令", "No commands available")
			}
			empty := ui.Box(c.Key("command-palette-empty")).FillWidth().Height(commandPaletteEmptyH).Center().
				Padding(0, theme.Space24, 0, theme.Space24)
			blockui.Probe(empty, "command-palette-empty")
			empty.Children(func() {
				ui.Text(c, emptyMsg).FontSize(theme.FontSmall).TextColor(pal.TextSecondary).SingleLine()
			})
		} else {
			scroll := blockui.Scroll(c.Key("command-palette-list"), &p.scroll).
				FillWidth().MaxHeight(maxBodyH).
				Padding(theme.Space8, theme.ScrollGutter, theme.Space8, theme.Space8).Gap(theme.Space2).
				ScrollbarStyle(ui.ScrollbarStyle{
					TrackWidth: theme.ScrollGutter,
					ThumbWidth: theme.ScrollThumbWidth,
					Thumb:      pal.ScrollThumb,
					Hover:      pal.ScrollThumbHover,
				})
			scroll.Children(func() {
				for idx, it := range filtered {
					item := it
					selected := idx == p.selected
					rowID := fmt.Sprintf("command-palette-item-%d", idx)
					row := ui.ButtonBase(c.Key(rowID)).FillWidth().
						Padding(theme.Space8, theme.Space12, theme.Space8, theme.Space12).
						Radius(theme.RadiusMD).Cursor(ui.CursorPointer).TabStop(false).Label(item.Title)
					hovered := row.Hovered()
					pressed := row.Pressed()
					switch {
					case selected:
						row.Background(pal.Selected).Border(1, pal.SelectedBorder)
					case pressed:
						row.Background(pal.Selected).Border(1, ui.Transparent)
					case hovered:
						row.Background(pal.Hover).Border(1, ui.Transparent)
					default:
						row.Border(1, ui.Transparent)
					}
					blockui.TactileCard(row, hovered, pressed, selected, false)
					if selected && p.revealSelected {
						blockui.RevealActiveItem(row)
						p.revealSelected = false
					}
					row.Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(theme.Space10).Children(func() {
							if item.Icon != nil && len(item.Icon.StrokePaths) > 0 {
								ic := pal.DuoIconColors(pal.TextSecondary)
								if selected || hovered || pressed {
									ic = pal.DuoIconColors(pal.Text)
								}
								pluginui.DrawDuoSpecAnimated(c, *item.Icon, theme.IconRow, ic, selected || hovered || pressed)
							}
							ui.Column(c).Grow(1).MinWidth(0).Gap(theme.Space2).Children(func() {
								var titleMarks, subMarks [][2]int
								if strings.TrimSpace(p.query) != "" {
									titleMarks = fuzzy.HighlightRanges(item.Title, p.query)
									if item.Subtitle != "" && len(titleMarks) == 0 {
										subMarks = fuzzy.HighlightRanges(item.Subtitle, p.query)
									}
								}
								blockui.HighlightedText(c, item.Title, titleMarks, theme.FontBody, theme.WeightControl, pal.Text, pal.SearchHighlight).
									LineHeight(theme.LineBody / theme.FontBody)
								if item.Subtitle != "" {
									blockui.HighlightedText(c, item.Subtitle, subMarks, theme.FontSmall, 0, pal.TextSecondary, pal.SearchHighlight).
										LineHeight(theme.LineSmall / theme.FontSmall)
								}
							})
							if item.Shortcut != "" {
								blockui.ShortcutKeycap(c, item.Shortcut)
							}
						})
					})
					blockui.Probe(row, rowID)
					if row.Clicked() {
						p.Close()
						if onRun != nil {
							onRun(item)
						}
					}
				}
			})
		}

		renderPaletteFooter(c, pal)
	})

	if !p.open {
		p.query = ""
		p.lastQuery = ""
		p.selected = 0
		p.revealSelected = false
		p.focusPending = false
	}
}

func renderPaletteFooter(c *ui.Context, pal theme.Palette) {
	ui.Row(c.Key("command-palette-footer")).FillWidth().Height(commandPaletteFooterH).Shrink(0).
		Padding(0, theme.Space16, 0, theme.Space16).Gap(theme.Space12).AlignItems(ui.Center).
		Radius(0, 0, theme.RadiusXLInner, theme.RadiusXLInner).
		BorderWidth(1, 0, 0, 0).BorderColor(pal.Hairline).Background(pal.Sidebar).
		Children(func() {
			renderPaletteHint(c, pal, []string{"↑", "↓"}, theme.Tr("选择", "Select"))
			renderPaletteHint(c, pal, []string{"↵"}, theme.Tr("运行", "Run"))
			renderPaletteHint(c, pal, []string{"esc"}, theme.Tr("关闭", "Close"))
		})
}

func renderPaletteHint(c *ui.Context, pal theme.Palette, keys []string, label string) {
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
		for _, k := range keys {
			blockui.Keycap(c, k)
		}
		ui.Text(c, label).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
	})
}

type scoredPaletteItem struct {
	item  PaletteItem
	score int
	index int
}

func filterPaletteItems(items []PaletteItem, rawQuery string) []PaletteItem {
	q := strings.TrimSpace(rawQuery)
	if q == "" {
		return items
	}
	scored := make([]scoredPaletteItem, 0, len(items))
	for i, it := range items {
		if sc, ok := fuzzy.ScoreFields(q, it.Title, it.Subtitle); ok {
			scored = append(scored, scoredPaletteItem{item: it, score: sc, index: i})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].index < scored[j].index
	})
	out := make([]PaletteItem, len(scored))
	for i, s := range scored {
		out[i] = s.item
	}
	return out
}
