package ui

import (
	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// TableGridRuleWidth is the layout space reserved for one grid line.
const TableGridRuleWidth float32 = 1

// TableGrid exposes one background through the perimeter and the gaps
// between cells. Cell backgrounds should be opaque.
func TableGrid(c *ui.Context, ruleColor ui.Color) ui.Element {
	return ui.Column(c).Radius(theme.RadiusSM).Clip().
		Border(TableGridRuleWidth, ui.Transparent).Background(ruleColor).Gap(TableGridRuleWidth)
}

func TableGridRow(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(TableGridRuleWidth)
}
