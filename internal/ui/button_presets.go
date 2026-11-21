package ui

import (
	"block/internal/icons"
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// PresetOptions configures optional overrides on preset action buttons.
// Zero values preserve each preset's default label, height, and style.
type PresetOptions struct {
	Label              string
	AccessibilityLabel string
	Disabled           bool
	Loading            bool
	Height             float32
	Width              float32
	MinWidth           float32
	PaddingX           float32
	Grow               float32
	Shrink             *float32
	Truncate           bool
	SecondaryShellBG   bool
	RegularWeight      bool
	AlwaysDuo          bool
	Style              *ActionButtonStyle
}

// StyleRef returns a pointer to s for overriding PresetOptions.Style.
func StyleRef(s ActionButtonStyle) *ActionButtonStyle {
	return &s
}

func renderPresetButton(
	c *ui.Context,
	id string,
	defaultLabel string,
	glyph icons.Icon,
	defaultStyle ActionButtonStyle,
	opts PresetOptions,
) ui.Element {
	label := opts.Label
	if label == "" {
		label = defaultLabel
	}
	style := defaultStyle
	if opts.Style != nil {
		style = *opts.Style
	}
	return ActionButtonWithOptions(c, ActionButtonOptions{
		ID:                 id,
		Label:              label,
		AccessibilityLabel: opts.AccessibilityLabel,
		Leading:            &glyph,
		AlwaysDuo:          opts.AlwaysDuo,
		Style:              style,
		Disabled:           opts.Disabled,
		Loading:            opts.Loading,
		Height:             opts.Height,
		Width:              opts.Width,
		MinWidth:           opts.MinWidth,
		PaddingX:           opts.PaddingX,
		Grow:               opts.Grow,
		Shrink:             opts.Shrink,
		Truncate:           opts.Truncate,
		SecondaryShellBG:   opts.SecondaryShellBG,
		RegularWeight:      opts.RegularWeight,
	})
}

// BtnConfirm renders a primary confirmation button with a checkmark icon.
func BtnConfirm(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("确定", "Confirm"), icons.Check, ButtonPrimary, opts)
}

// BtnCancel renders a secondary cancel button with a close icon.
func BtnCancel(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("取消", "Cancel"), icons.Close, ButtonSecondary, opts)
}

// BtnClose renders a secondary close button with a close icon.
func BtnClose(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("关闭", "Close"), icons.Close, ButtonSecondary, opts)
}

// BtnBack renders a ghost back button with a left-arrow icon.
func BtnBack(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("返回", "Back"), icons.ArrowLeft, ButtonGhost, opts)
}

// BtnRetry renders an accent-outlined retry button with a refresh icon.
func BtnRetry(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("重试", "Retry"), icons.Refresh, ButtonAccentOutline, opts)
}

// BtnInstall renders a primary install button with a download icon.
func BtnInstall(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("安装", "Install"), icons.Download, ButtonPrimary, opts)
}

// BtnDelete renders a danger delete button with a trash icon.
func BtnDelete(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("删除", "Delete"), icons.Trash, ButtonDanger, opts)
}

// BtnStop renders a secondary stop button with a stop icon.
func BtnStop(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("停止", "Stop"), icons.Stop, ButtonSecondary, opts)
}

// BtnLater renders a secondary defer button with a clock icon.
func BtnLater(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("稍后提醒", "Later"), icons.Clock, ButtonSecondary, opts)
}

// BtnSkip renders a secondary skip button with a skip-forward icon.
func BtnSkip(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("跳过", "Skip"), icons.SkipForward, ButtonSecondary, opts)
}

// BtnExpand renders a secondary expand/viewer button with a maximize icon.
func BtnExpand(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("放大观看", "Open in viewer"), icons.Maximize, ButtonSecondary, opts)
}

// BtnMore renders a ghost show-more button with a down-chevron icon.
func BtnMore(c *ui.Context, id string, opts PresetOptions) ui.Element {
	return renderPresetButton(c, id, theme.Tr("显示更多", "Show more"), icons.ChevronDown, ButtonGhost, opts)
}
