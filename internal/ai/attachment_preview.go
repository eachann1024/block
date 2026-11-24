package ai

import (
	"fmt"
	"slices"

	"block/internal/icons"
	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"

	"github.com/egoist/mygo/ui"
)

type attachmentLightboxSource struct {
	isDraft       bool
	convID        uint64
	msgIdx        int
	textPath      string
	textBody      string
	textTruncated bool
	textLoaded    bool
}

func (c *Controller) openDraftLightbox(index int) {
	c.mu.Lock()
	if len(c.draftAttachments) == 0 {
		c.mu.Unlock()
		return
	}
	if index < 0 || index >= len(c.draftAttachments) {
		index = 0
	}
	c.historyOpen = false
	c.deckOpen = false
	c.pickerDismissed = true
	c.lightboxSource = attachmentLightboxSource{isDraft: true}
	c.lightboxOpen = true
	c.lightbox.Open = true
	c.lightbox.Index = index
	c.lightbox.ResetView()
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) openMessageLightbox(convID uint64, msgIdx int, index int) {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) || len(conv.Messages[msgIdx].Attachments) == 0 {
		c.mu.Unlock()
		return
	}
	atts := conv.Messages[msgIdx].Attachments
	if index < 0 || index >= len(atts) {
		index = 0
	}
	c.historyOpen = false
	c.deckOpen = false
	c.pickerDismissed = true
	c.lightboxSource = attachmentLightboxSource{
		isDraft: false,
		convID:  convID,
		msgIdx:  msgIdx,
	}
	c.lightboxOpen = true
	c.lightbox.Open = true
	c.lightbox.Index = index
	c.lightbox.ResetView()
	c.mu.Unlock()
	c.requestRedraw()
}

func (c *Controller) renderAttachmentLightbox(cUI *ui.Context) {
	c.mu.Lock()
	if !c.lightboxOpen {
		c.lightbox.Open = false
		c.mu.Unlock()
		return
	}
	c.lightbox.Open = true
	src := c.lightboxSource
	var atts []model.Attachment
	if src.isDraft {
		atts = slices.Clone(c.draftAttachments)
	} else if conv := c.findConvLocked(src.convID); conv != nil && src.convID == c.currentID && src.msgIdx >= 0 && src.msgIdx < len(conv.Messages) {
		atts = slices.Clone(conv.Messages[src.msgIdx].Attachments)
	}
	if len(atts) == 0 {
		c.lightboxOpen = false
		c.lightbox.Open = false
		c.mu.Unlock()
		return
	}
	if c.lightbox.Index < 0 {
		c.lightbox.Index = 0
	} else if c.lightbox.Index >= len(atts) {
		c.lightbox.Index = len(atts) - 1
	}
	activeIdx := c.lightbox.Index
	activeAtt := atts[activeIdx]
	needTextLoad := !activeAtt.IsImage() && attachmentIsText(activeAtt) && (!src.textLoaded || src.textPath != activeAtt.Path)
	cachedTextPath := src.textPath
	cachedTextBody := src.textBody
	cachedTextTrunc := src.textTruncated
	onReveal := c.onRevealPath
	c.mu.Unlock()

	if needTextLoad {
		body, truncated, ok := c.readAttachmentPreviewText(activeAtt)
		c.mu.Lock()
		c.lightboxSource.textPath = activeAtt.Path
		if ok {
			c.lightboxSource.textBody = body
			c.lightboxSource.textTruncated = truncated
		} else {
			c.lightboxSource.textBody = ""
			c.lightboxSource.textTruncated = false
		}
		c.lightboxSource.textLoaded = true
		cachedTextPath = c.lightboxSource.textPath
		cachedTextBody = c.lightboxSource.textBody
		cachedTextTrunc = c.lightboxSource.textTruncated
		c.mu.Unlock()
	}

	items := make([]bui.LightboxItem, len(atts))
	for i, att := range atts {
		item := bui.LightboxItem{
			ID:        fmt.Sprintf("%d:%s", i, att.Path),
			Name:      att.Name,
			IsImage:   att.IsImage(),
			CanRemove: src.isDraft,
		}
		if att.IsImage() {
			var imgSrc ui.ImageSource
			var iw, ih int
			var loading, failed bool
			if i == activeIdx {
				imgSrc, iw, ih, loading, failed = c.resolveAttachmentFullImage(att)
			} else {
				imgSrc, iw, ih, loading, failed = c.resolveAttachmentImage(att)
			}
			item.Image = imgSrc
			item.ImageW = iw
			item.ImageH = ih
			item.Loading = loading
			item.Unavailable = failed
			item.Meta = attachmentMetaString(att, iw, ih)
		} else {
			item.Meta = attachmentMetaString(att, 0, 0)
			if i == activeIdx && cachedTextPath == att.Path && cachedTextBody != "" {
				item.TextPreview = cachedTextBody
				item.TextTruncated = cachedTextTrunc
			}
		}
		items[i] = item
	}

	cb := bui.LightboxCallbacks{
		OnClose: func() {
			c.mu.Lock()
			c.lightboxOpen = false
			if src.isDraft {
				c.composerHandle.Focus()
			}
			c.mu.Unlock()
			c.requestRedraw()
		},
		OnSelect: func(idx int) {
			c.requestRedraw()
		},
		OnCopy: func(idx int) bool {
			if idx < 0 || idx >= len(atts) {
				return false
			}
			return c.copyAttachmentToClipboard(cUI.Services(), atts[idx])
		},
	}
	if onReveal != nil {
		cb.OnReveal = func(idx int) {
			if idx >= 0 && idx < len(atts) && atts[idx].Path != "" {
				onReveal(atts[idx].Path)
			}
		}
	}
	if src.isDraft {
		cb.OnRemove = func(idx int) {
			if idx < 0 || idx >= len(atts) {
				return
			}
			c.removeDraftAttachment(atts[idx])
			c.mu.Lock()
			if len(c.draftAttachments) == 0 {
				c.lightboxOpen = false
				c.lightbox.Open = false
				c.composerHandle.Focus()
			} else {
				if c.lightbox.Index >= len(c.draftAttachments) {
					c.lightbox.Index = len(c.draftAttachments) - 1
				}
				c.lightbox.ResetView()
			}
			c.mu.Unlock()
			c.requestRedraw()
		}
	}

	bui.RenderLightbox(cUI, "ai-attachment-lightbox", &c.lightbox, items, cb)
	c.mu.Lock()
	if !c.lightbox.Open {
		c.lightboxOpen = false
	}
	c.mu.Unlock()
}

type indexedAttachment struct {
	idx int
	att model.Attachment
}

func splitAttachmentsByKind(atts []model.Attachment) (images []indexedAttachment, files []indexedAttachment) {
	for i, a := range atts {
		if a.IsImage() {
			images = append(images, indexedAttachment{idx: i, att: a})
		} else {
			files = append(files, indexedAttachment{idx: i, att: a})
		}
	}
	return images, files
}

func (c *Controller) renderUserMessageAttachments(cUI *ui.Context, pal theme.Palette, convID uint64, msgIdx int, atts []model.Attachment, probeFirst bool) {
	images, files := splitAttachmentsByKind(atts)
	ais := pal.AISurface()

	if len(images) == 1 {
		ia := images[0]
		imgSrc, iw, ih, loading, failed := c.resolveAttachmentImage(ia.att)
		drawW, drawH := singleImageThumbSize(iw, ih)
		btnID := fmt.Sprintf("user-att-img-%d-%d-%d", convID, msgIdx, ia.idx)
		c.renderMessageImageTile(cUI, pal, ais, btnID, ia.att, imgSrc, iw, ih, drawW, drawH, loading, failed, probeFirst, func() {
			c.openMessageLightbox(convID, msgIdx, ia.idx)
		})
		probeFirst = false
	} else if len(images) > 1 {
		thumbSide := theme.AIAttachGridThumb
		if len(images) == 2 {
			thumbSide = theme.AIAttachGridThumb + theme.Space20
		}
		grid := ui.Row(cUI.Key(fmt.Sprintf("user-att-grid-%d-%d", convID, msgIdx))).
			MaxWidth(320).Justify(ui.End).Gap(theme.Space6).Wrap()
		grid.Children(func() {
			for i, ia := range images {
				imgSrc, iw, ih, loading, failed := c.resolveAttachmentImage(ia.att)
				btnID := fmt.Sprintf("user-att-img-%d-%d-%d", convID, msgIdx, ia.idx)
				c.renderMessageImageTile(cUI, pal, ais, btnID, ia.att, imgSrc, iw, ih, thumbSide, thumbSide, loading, failed, probeFirst && i == 0, func() {
					c.openMessageLightbox(convID, msgIdx, ia.idx)
				})
			}
		})
		probeFirst = false
	}

	if len(files) > 0 {
		row := ui.Row(cUI.Key(fmt.Sprintf("user-att-files-%d-%d", convID, msgIdx))).
			MaxWidth(320).Justify(ui.End).Gap(theme.Space6).Wrap()
		row.Children(func() {
			for i, fa := range files {
				pillID := fmt.Sprintf("user-att-file-%d-%d-%d", convID, msgIdx, fa.idx)
				pill := ui.ButtonBase(cUI.Key(pillID)).Height(theme.HeightCompact).MaxWidth(theme.AIAttachFileChipMaxW).
					PaddingX(theme.Space8).Gap(theme.Space6).AlignItems(ui.Center).
					Radius(theme.RadiusMD).Cursor(ui.CursorPointer).Label(fa.att.Name)
				hovered := pill.Hovered()
				border := ais.CapsuleBorder
				bg := ais.CapsuleBG
				if hovered {
					border = pal.Accent
					bg = ais.HoverBG
				}
				bui.AnimateSurfaceColors(pill, bg, border, true)
				bui.TactileButton(pill, hovered, pill.Pressed(), false)
				if probeFirst && i == 0 {
					bui.Probe(pill, "ai-user-message")
				}
				bui.Probe(pill, "ai-user-attachment")
				c.attachFileTooltip(cUI, pill, fa.att, 0, 0, false, theme.Tr("点击查看详情", "Click to preview"))
				pill.Children(func() {
					icons.DrawAnimated(cUI, icons.Note, theme.IconAction, pal.DuoIconColors(pal.Accent), hovered)
					ui.Text(cUI, fa.att.Name).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
						TextColor(pal.Text).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…")
					if sz := formatAttachmentSize(fa.att.Size); sz != "" {
						ui.Text(cUI, sz).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).Shrink(0).SingleLine()
					}
				})
				if pill.Clicked() {
					c.openMessageLightbox(convID, msgIdx, fa.idx)
				}
			}
		})
	}
}

func (c *Controller) renderMessageImageTile(
	cUI *ui.Context,
	pal theme.Palette,
	ais theme.AISurfaceColors,
	id string,
	att model.Attachment,
	imgSrc ui.ImageSource,
	iw, ih int,
	w, h float32,
	loading, failed bool,
	asUserMsgProbe bool,
	onOpen func(),
) {
	tile := ui.ButtonBase(cUI.Key(id)).Size(w, h).Radius(theme.RadiusLG).Clip().
		Center().Cursor(ui.CursorPointer).Label(att.Name)
	hovered := tile.Hovered()
	border := ais.CapsuleBorder
	if hovered {
		border = pal.Accent
	}
	bui.AnimateSurfaceColors(tile, ais.MediaFrameBG, border, true)
	bui.TactileCard(tile, hovered, tile.Pressed(), false, false)
	if asUserMsgProbe {
		bui.Probe(tile, "ai-user-message")
	}
	bui.Probe(tile, "ai-user-attachment")
	c.attachFileTooltip(cUI, tile, att, iw, ih, failed, theme.Tr("点击放大查看", "Click to zoom"))

	tile.Children(func() {
		switch {
		case imgSrc != nil:
			ui.Image(cUI, imgSrc).Fill().Fit(ui.Cover)
			if hovered {
				badge := ui.Box(cUI.Key(id+"-zoom-badge")).Absolute().Right(theme.Space6).Bottom(theme.Space6).
					Size(theme.HeightMicro, theme.HeightMicro).Radius(theme.RadiusSM).Center().
					Background(pal.ImageOverlayBadgeBG())
				badge.Children(func() {
					icons.Draw(cUI, icons.WindowMax, theme.IconMicro, pal.MonoIconColors(pal.ImageOverlayBadgeFG()))
				})
			}
		case loading:
			icons.Draw(cUI, icons.Image, theme.IconRow, pal.DuoIconColors(pal.TextSecondary))
		default:
			ui.Column(cUI).Center().Gap(theme.Space4).Padding(theme.Space8).Children(func() {
				icons.Draw(cUI, icons.Image, theme.IconRow, pal.DuoIconColors(pal.TextSecondary))
				ui.Text(cUI, att.Name).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).
					MaxWidth(max(w-theme.Space16, 40)).SingleLine().Ellipsis("…")
			})
		}
	})

	if tile.Clicked() && onOpen != nil {
		onOpen()
	}
}

func (c *Controller) renderComposerAttachments(cUI *ui.Context, pal theme.Palette, atts []model.Attachment) {
	images, files := splitAttachmentsByKind(atts)
	ais := pal.AISurface()

	if len(images) > 0 {
		ui.Row(cUI.Key("ai-composer-att-images")).FillWidth().Gap(theme.Space8).Wrap().Padding(0, 0, theme.Space4, 0).Children(func() {
			for _, ia := range images {
				c.renderComposerImageTile(cUI, pal, ais, ia.idx, ia.att)
			}
		})
	}
	if len(files) > 0 {
		ui.Row(cUI.Key("ai-composer-att-files")).FillWidth().Gap(theme.Space6).Wrap().Padding(0, 0, theme.Space4, 0).Children(func() {
			for _, fa := range files {
				c.renderComposerFileChip(cUI, pal, ais, fa.idx, fa.att)
			}
		})
	}
}

func (c *Controller) renderComposerImageTile(cUI *ui.Context, pal theme.Palette, ais theme.AISurfaceColors, idx int, att model.Attachment) {
	id := fmt.Sprintf("chip-att-%d", idx)
	imgSrc, iw, ih, loading, failed := c.resolveAttachmentImage(att)

	frame := ui.Box(cUI.Key(id)).Size(theme.AIAttachComposerThumb, theme.AIAttachComposerThumb).
		Radius(theme.RadiusMD).Clip().Background(ais.MediaFrameBG)
	hovered := frame.Hovered()
	border := ais.CapsuleBorder
	if hovered {
		border = pal.Accent
	}
	frame.Border(1, border)
	bui.Probe(frame, "ai-attachment-chip")

	var openClicked bool
	var removeClicked bool
	var xPressedInside bool

	frame.Children(func() {
		previewBtn := ui.ButtonBase(cUI.Key(id + "-preview")).Fill().Center().
			Cursor(ui.CursorPointer).Label(theme.Tr("预览 ", "Preview ") + att.Name)
		bui.TactileCard(previewBtn, previewBtn.Hovered(), previewBtn.Pressed(), false, false)
		c.attachFileTooltip(cUI, previewBtn, att, iw, ih, failed, theme.Tr("点击放大预览", "Click to preview"))
		previewBtn.Children(func() {
			switch {
			case imgSrc != nil:
				ui.Image(cUI, imgSrc).Fill().Fit(ui.Cover)
				if hovered {
					badge := ui.Box(cUI.Key(id+"-zoom")).Absolute().Left(theme.Space4).Bottom(theme.Space4).
						Size(theme.AIAttachComposerRemoveSize, theme.AIAttachComposerRemoveSize).
						Radius(theme.RadiusXS).Center().Background(pal.ImageOverlayBadgeBG())
					badge.Children(func() {
						icons.Draw(cUI, icons.WindowMax, theme.IconMicro, pal.MonoIconColors(pal.ImageOverlayBadgeFG()))
					})
				}
			case loading:
				icons.Draw(cUI, icons.Image, theme.IconAction, pal.DuoIconColors(pal.TextSecondary))
			default:
				icons.Draw(cUI, icons.Image, theme.IconAction, pal.TintedIconColors(pal.Danger))
			}
		})
		openClicked = previewBtn.Clicked()

		xBtn := ui.ButtonBase(cUI.Key(id+"-x")).Absolute().Top(theme.Space2).Right(theme.Space2).
			Size(theme.AIAttachComposerRemoveSize, theme.AIAttachComposerRemoveSize).
			Radius(theme.RadiusPill).Center().Cursor(ui.CursorPointer).
			Label(theme.Tr("移除 ", "Remove ") + att.Name)
		xHover := xBtn.Hovered()
		xBG := pal.ImageOverlayBadgeBG()
		if xHover {
			xBG = pal.Danger
		}
		xBtn.Background(xBG)
		if !hovered && !xHover {
			xBtn.Opacity(0.78)
		}
		bui.Tooltip(cUI, xBtn, theme.Tr("移除附件", "Remove attachment"))
		xBtn.Children(func() {
			icons.DrawAnimated(cUI, icons.Close, theme.IconMicro, pal.MonoIconColors(pal.ImageOverlayBadgeFG()), xHover || xBtn.Pressed())
		})
		removeClicked = xBtn.Clicked()
		xPressedInside = xBtn.PressedInside()
	})

	if removeClicked {
		c.removeDraftAttachment(att)
		c.mu.Lock()
		c.composerHandle.Focus()
		c.mu.Unlock()
		c.requestRedraw()
		return
	}
	if openClicked && !xPressedInside {
		c.openDraftLightbox(idx)
	}
}

func (c *Controller) renderComposerFileChip(cUI *ui.Context, pal theme.Palette, ais theme.AISurfaceColors, idx int, att model.Attachment) {
	id := fmt.Sprintf("chip-att-%d", idx)
	chip := ui.Row(cUI.Key(id)).Height(theme.HeightCompact).Padding(0, theme.Space4, 0, theme.Space8).Gap(theme.Space4).
		AlignItems(ui.Center).Radius(theme.RadiusMD)
	hovered := chip.Hovered()
	border := ais.CapsuleBorder
	bg := ais.CapsuleBG
	if hovered {
		border = pal.Accent
		bg = ais.HoverBG
	}
	bui.AnimateSurfaceColors(chip, bg, border, true)
	bui.Probe(chip, "ai-attachment-chip")

	chip.Children(func() {
		openBtn := ui.ButtonBase(cUI.Key(id + "-open")).FillHeight().Gap(theme.Space6).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Label(theme.Tr("查看 ", "Preview ") + att.Name)
		c.attachFileTooltip(cUI, openBtn, att, 0, 0, false, theme.Tr("点击查看详情", "Click to preview"))
		openBtn.Children(func() {
			icons.DrawAnimated(cUI, icons.Note, theme.IconAction, pal.DuoIconColors(pal.Accent), hovered)
			ui.Text(cUI, att.Name).FontSize(theme.FontSmall).FontWeight(theme.WeightControl).
				TextColor(pal.Text).SingleLine().MaxWidth(164).Ellipsis("…")
			if sz := formatAttachmentSize(att.Size); sz != "" {
				ui.Text(cUI, sz).FontSize(theme.FontMeta).TextColor(pal.TextSecondary).SingleLine()
			}
		})
		if openBtn.Clicked() {
			c.openDraftLightbox(idx)
		}

		xBtn := bui.IconButton(cUI, icons.Close, bui.IconButtonOptions{
			ID:       id + "-x",
			Label:    theme.Tr("移除 ", "Remove ") + att.Name,
			Tooltip:  theme.Tr("移除", "Remove"),
			Size:     theme.HeightMicro - theme.Space2,
			IconSize: theme.IconMicro,
			Radius:   theme.RadiusXS,
		})
		if xBtn.Clicked() {
			c.removeDraftAttachment(att)
			c.mu.Lock()
			c.composerHandle.Focus()
			c.mu.Unlock()
			c.requestRedraw()
		}
	})
}

func (c *Controller) attachFileTooltip(cUI *ui.Context, el ui.Element, att model.Attachment, iw, ih int, failed bool, actionHint string) {
	rows := []bui.TooltipRow{
		{Trigger: att.Name},
	}
	if meta := attachmentMetaString(att, iw, ih); meta != "" {
		rows[0].Detail = meta
	}
	if failed {
		rows = append(rows, bui.TooltipRow{
			Detail:      theme.Tr("图片缓存不可用", "Cached image unavailable"),
			TriggerTone: bui.TooltipToneDanger,
		})
	} else if actionHint != "" {
		rows = append(rows, bui.TooltipRow{
			Detail: actionHint,
		})
	}
	bui.RichTooltip(cUI, el, bui.TooltipSpec{
		Placement: bui.TooltipAbove,
		Rows:      rows,
	})
}

func estimateAttachmentsHeight(atts []model.Attachment) float32 {
	if len(atts) == 0 {
		return 0
	}
	images, files := splitAttachmentsByKind(atts)
	var h float32
	if len(images) == 1 {
		h += theme.AIAttachSingleMaxH + theme.Space6
	} else if len(images) > 1 {
		rows := (len(images) + 2) / 3
		h += float32(rows)*(theme.AIAttachGridThumb+theme.Space6) + theme.Space4
	}
	if len(files) > 0 {
		h += theme.HeightCompact + theme.Space6
	}
	return h
}
