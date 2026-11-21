package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ItemKind string

const (
	KindSegmented ItemKind = "segmented"
	KindInput     ItemKind = "input"
	KindFolder    ItemKind = "folder"
	KindSecret    ItemKind = "secret"
	KindSelect    ItemKind = "select"
	KindChecklist ItemKind = "checklist"
	KindGroup     ItemKind = "group"
	KindSwitch    ItemKind = "switch"
	KindActions   ItemKind = "actions"
	KindKV        ItemKind = "kv"
	KindMetrics   ItemKind = "metrics"
	KindTreemap   ItemKind = "treemap"
	KindPreview   ItemKind = "preview"
	KindLogList   ItemKind = "log_list"
)

type ButtonVariant string

const (
	VariantPrimary   ButtonVariant = "primary"
	VariantSecondary ButtonVariant = "secondary"
	VariantDanger    ButtonVariant = "danger"
)

type UIOption struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Badge   string `json:"badge,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
}

type UIButton struct {
	ID      string        `json:"id"`
	Label   string        `json:"label"`
	Variant ButtonVariant `json:"variant"`
}

type UIKVRow struct {
	Label   string `json:"label"`
	Value   string `json:"value"`
	Tooltip string `json:"tooltip,omitempty"`
}

type UIMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Tone  string `json:"tone,omitempty"`
}

type UITreemapSummary struct {
	TotalLabel string     `json:"total_label,omitempty"`
	TotalValue string     `json:"total_value"`
	SubText    string     `json:"sub_text,omitempty"`
	Metrics    []UIMetric `json:"metrics,omitempty"`
}

type UITreemapEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Bytes uint64 `json:"bytes"`
	Count int    `json:"count,omitempty"`
}

type UITreemapTile struct {
	ID        string           `json:"id"`
	Label     string           `json:"label"`
	Detail    string           `json:"detail,omitempty"`
	Bytes     uint64           `json:"bytes"`
	FileCount int              `json:"file_count,omitempty"`
	Color     string           `json:"color,omitempty"`
	Entries   []UITreemapEntry `json:"entries,omitempty"`
}

type UILogEntry struct {
	Title    string  `json:"title"`
	SubText  string  `json:"sub_text,omitempty"`
	Time     string  `json:"time,omitempty"`
	Status   string  `json:"status,omitempty"`
	Tone     string  `json:"tone,omitempty"`
	Ratio    float64 `json:"ratio,omitempty"`
	Tooltip  string  `json:"tooltip,omitempty"`
	ActionID string  `json:"action_id,omitempty"`
}

type UIPreviewCard struct {
	Badge string `json:"badge,omitempty"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type UIPreview struct {
	Variant  string          `json:"variant,omitempty"` // "article" | "cards" | "diff" | "plan"
	Palette  string          `json:"palette,omitempty"` // "ocean" | "paper" | "ink" | "warm"
	Badge    string          `json:"badge,omitempty"`
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle,omitempty"`
	Before   string          `json:"before,omitempty"`
	After    string          `json:"after,omitempty"`
	Body     string          `json:"body,omitempty"`
	Cards    []UIPreviewCard `json:"cards,omitempty"`
}

// UIVisibleWhen conditionally shows a UIItem based on another form control's current value.
type UIVisibleWhen struct {
	ControlID string   `json:"control_id"`
	Equals    any      `json:"equals,omitempty"`
	In        []string `json:"in,omitempty"`
}

type UIItem struct {
	Kind             ItemKind          `json:"kind"`
	ID               string            `json:"id,omitempty"`
	Label            string            `json:"label,omitempty"`
	Layout           string            `json:"layout,omitempty"` // "" | "full" | "stacked"
	Value            any               `json:"value,omitempty"`
	Placeholder      string            `json:"placeholder,omitempty"`
	AcceptExtensions []string          `json:"accept_extensions,omitempty"`
	AddFolder        bool              `json:"add_folder,omitempty"`
	Options          []UIOption        `json:"options,omitempty"`
	Fields           []UIItem          `json:"fields,omitempty"`
	Buttons          []UIButton        `json:"buttons,omitempty"`
	Rows             []UIKVRow         `json:"rows,omitempty"`
	Metrics          []UIMetric        `json:"metrics,omitempty"`
	Summary          *UITreemapSummary `json:"summary,omitempty"`
	Tiles            []UITreemapTile   `json:"tiles,omitempty"`
	TotalBytes       uint64            `json:"total_bytes,omitempty"`
	Preview          *UIPreview        `json:"preview,omitempty"`
	EmptyText        string            `json:"empty_text,omitempty"`
	Entries          []UILogEntry      `json:"entries,omitempty"`
	VisibleWhen      *UIVisibleWhen    `json:"visible_when,omitempty"`
}

type UICard struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Meta             string   `json:"meta,omitempty"`
	Badge            string   `json:"badge,omitempty"`
	Collapsible      bool     `json:"collapsible,omitempty"`
	DefaultCollapsed bool     `json:"default_collapsed,omitempty"`
	Items            []UIItem `json:"items"`
}

// ActionProgress captures live progress reported by a plugin during an active
// action via host.action.progress or declaratively on UIPage.Progress.
type ActionProgress struct {
	HasProgress bool
	Progress    float64 // 0..1 ratio when HasProgress is true
	Done        int     // completed items when Total > 0
	Total       int     // total items when > 0
	Label       string  // optional short override label
}

type UIPage struct {
	Title             string     `json:"title"`
	Status            string     `json:"status,omitempty"`
	Progress          *float64   `json:"progress,omitempty"`
	RefreshIntervalMs int        `json:"refresh_interval_ms,omitempty"`
	HeaderActions     []UIButton `json:"header_actions,omitempty"`
	Cards             []UICard   `json:"cards"`
}

// HasHeaderAction reports whether actionID matches any button in HeaderActions.
func (p *UIPage) HasHeaderAction(actionID string) bool {
	if p == nil || actionID == "" {
		return false
	}
	for _, b := range p.HeaderActions {
		if b.ID == actionID {
			return true
		}
	}
	return false
}

func DecodePageStrict(raw json.RawMessage) (*UIPage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var page UIPage
	if err := dec.Decode(&page); err != nil {
		return nil, fmt.Errorf("strict decode UIPage: %w", err)
	}
	if err := page.Validate(); err != nil {
		return nil, err
	}
	return &page, nil
}

// IsItemVisible evaluates item.VisibleWhen against the current form control values map.
func IsItemVisible(item UIItem, values map[string]any) bool {
	cond := item.VisibleWhen
	if cond == nil || strings.TrimSpace(cond.ControlID) == "" {
		return true
	}
	cur, exists := values[cond.ControlID]
	if !exists {
		return false
	}
	if len(cond.In) > 0 {
		curStr := fmt.Sprintf("%v", cur)
		for _, allowed := range cond.In {
			if curStr == allowed {
				return true
			}
		}
		return false
	}
	if cond.Equals != nil {
		switch want := cond.Equals.(type) {
		case bool:
			got, ok := cur.(bool)
			return ok && got == want
		case string:
			got, ok := cur.(string)
			return ok && got == want
		default:
			return fmt.Sprintf("%v", cur) == fmt.Sprintf("%v", want)
		}
	}
	return true
}

// Page size caps keep one plugin response from stalling every UI frame.
const (
	maxPageCards = 64
	maxPageItems = 400
)

func (p *UIPage) Validate() error {
	if err := checkText("page.title", p.Title, true); err != nil {
		return err
	}
	if err := checkUserText("page.status", p.Status, false); err != nil {
		return err
	}
	if p.Progress != nil && !(*p.Progress >= 0 && *p.Progress <= 1) {
		return fmt.Errorf("page.progress must be between 0 and 1, got %v", *p.Progress)
	}
	if p.RefreshIntervalMs != 0 && (p.RefreshIntervalMs < 500 || p.RefreshIntervalMs > 60000) {
		return fmt.Errorf("page.refresh_interval_ms must be 0 or between 500 and 60000, got %d", p.RefreshIntervalMs)
	}
	seenActionIDs := map[string]bool{}
	for i, b := range p.HeaderActions {
		if err := validateButton(fmt.Sprintf("page.header_actions[%d]", i), b, seenActionIDs); err != nil {
			return err
		}
	}
	if len(p.Cards) == 0 {
		return errors.New("page.cards must contain at least one card")
	}
	if len(p.Cards) > maxPageCards {
		return fmt.Errorf("page.cards allows at most %d cards", maxPageCards)
	}
	totalItems := 0
	for _, card := range p.Cards {
		totalItems += len(card.Items)
	}
	if totalItems > maxPageItems {
		return fmt.Errorf("page allows at most %d items across all cards, got %d", maxPageItems, totalItems)
	}
	seenCardIDs := map[string]bool{}
	seenControlIDs := map[string]bool{}
	for ci, card := range p.Cards {
		prefix := fmt.Sprintf("page.cards[%d]", ci)
		if strings.TrimSpace(card.ID) == "" {
			return fmt.Errorf("%s.id is required", prefix)
		}
		if seenCardIDs[card.ID] {
			return fmt.Errorf("%s.id %q is duplicated", prefix, card.ID)
		}
		seenCardIDs[card.ID] = true
		if err := checkText(prefix+".title", card.Title, false); err != nil {
			return err
		}
		if err := checkUserText(prefix+".meta", card.Meta, false); err != nil {
			return err
		}
		if len(card.Items) == 0 {
			return fmt.Errorf("%s.items must contain at least one item", prefix)
		}
		for ii, item := range card.Items {
			ip := fmt.Sprintf("%s.items[%d]", prefix, ii)
			if err := validateItem(ip, item, seenControlIDs, seenActionIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

// StringSliceValue normalizes a checklist control value ([]string or []any from JSON) into a clean []string copy.
func StringSliceValue(v any) ([]string, bool) {
	switch raw := v.(type) {
	case []string:
		out := make([]string, len(raw))
		copy(out, raw)
		return out, true
	case []any:
		out := make([]string, 0, len(raw))
		for _, elem := range raw {
			s, ok := elem.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case nil:
		return []string{}, true
	default:
		return nil, false
	}
}

func extractItemDefaultValues(item UIItem, out map[string]any) {
	switch item.Kind {
	case KindSegmented, KindInput, KindFolder, KindSecret, KindSelect, KindSwitch:
		if item.ID != "" {
			out[item.ID] = item.Value
		}
	case KindChecklist:
		if item.ID != "" {
			if list, ok := StringSliceValue(item.Value); ok {
				out[item.ID] = list
			} else {
				out[item.ID] = []string{}
			}
		}
	case KindGroup:
		for _, child := range item.Fields {
			extractItemDefaultValues(child, out)
		}
	}
}

// ExtractDefaultValues collects current form control values keyed by control ID.
func (p *UIPage) ExtractDefaultValues() map[string]any {
	out := map[string]any{}
	for _, card := range p.Cards {
		for _, item := range card.Items {
			extractItemDefaultValues(item, out)
		}
	}
	return out
}

// CollectActionIDs returns all button IDs in header_actions and actions items.
func (p *UIPage) CollectActionIDs() []string {
	var ids []string
	for _, b := range p.HeaderActions {
		ids = append(ids, b.ID)
	}
	for _, card := range p.Cards {
		for _, item := range card.Items {
			if item.Kind == KindActions {
				for _, b := range item.Buttons {
					ids = append(ids, b.ID)
				}
			}
		}
	}
	return ids
}

func validateItem(prefix string, item UIItem, seenControls, seenActions map[string]bool) error {
	if item.AddFolder && item.Kind != KindChecklist {
		return fmt.Errorf("%s.add_folder is only allowed on checklist", prefix)
	}
	if item.VisibleWhen != nil {
		if strings.TrimSpace(item.VisibleWhen.ControlID) == "" {
			return fmt.Errorf("%s.visible_when.control_id is required", prefix)
		}
		if item.VisibleWhen.Equals == nil && len(item.VisibleWhen.In) == 0 {
			return fmt.Errorf("%s.visible_when requires equals or in", prefix)
		}
	}
	switch item.Layout {
	case "", "full", "stacked":
	default:
		return fmt.Errorf("%s.layout %q must be empty, full, or stacked", prefix, item.Layout)
	}

	switch item.Kind {
	case KindSegmented, KindSelect:
		if item.Kind == KindSegmented && item.Layout == "full" {
			if err := registerControlOptionalLabel(prefix, item.ID, item.Label, seenControls); err != nil {
				return err
			}
		} else if err := registerControl(prefix, item.ID, item.Label, seenControls); err != nil {
			return err
		}
		strVal, ok := item.Value.(string)
		if !ok {
			return fmt.Errorf("%s.value must be a string", prefix)
		}
		minOpts := 2
		if item.Kind == KindSelect {
			minOpts = 1
		}
		if len(item.Options) < minOpts {
			return fmt.Errorf("%s.options must have at least %d option(s)", prefix, minOpts)
		}
		matched := false
		for oi, opt := range item.Options {
			if strings.TrimSpace(opt.Value) == "" {
				return fmt.Errorf("%s.options[%d].value is required", prefix, oi)
			}
			if err := checkUserText(fmt.Sprintf("%s.options[%d].label", prefix, oi), opt.Label, true); err != nil {
				return err
			}
			if opt.Value == strVal {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("%s.value %q does not match any option", prefix, strVal)
		}
	case KindChecklist:
		if err := registerControlOptionalLabel(prefix, item.ID, item.Label, seenControls); err != nil {
			return err
		}
		if _, ok := StringSliceValue(item.Value); !ok {
			return fmt.Errorf("%s.value must be an array of strings", prefix)
		}
		if err := checkText(prefix+".empty_text", item.EmptyText, false); err != nil {
			return err
		}
		seenOptValues := map[string]bool{}
		for oi, opt := range item.Options {
			op := fmt.Sprintf("%s.options[%d]", prefix, oi)
			if strings.TrimSpace(opt.Value) == "" {
				return fmt.Errorf("%s.value is required", op)
			}
			if seenOptValues[opt.Value] {
				return fmt.Errorf("%s.value %q is duplicated", op, opt.Value)
			}
			seenOptValues[opt.Value] = true
			if err := checkUserText(op+".label", opt.Label, true); err != nil {
				return err
			}
			if err := checkUserText(op+".detail", opt.Detail, false); err != nil {
				return err
			}
			if err := checkText(op+".badge", opt.Badge, false); err != nil {
				return err
			}
		}
	case KindGroup:
		if len(item.Fields) < 2 {
			return fmt.Errorf("%s.fields must contain at least 2 items", prefix)
		}
		for fi, field := range item.Fields {
			fp := fmt.Sprintf("%s.fields[%d]", prefix, fi)
			switch field.Kind {
			case KindInput, KindSecret, KindSegmented, KindSelect, KindFolder, KindSwitch:
			default:
				return fmt.Errorf("%s.kind %q is not allowed inside group", fp, field.Kind)
			}
			if err := validateItem(fp, field, seenControls, seenActions); err != nil {
				return err
			}
		}
	case KindInput, KindFolder, KindSecret:
		if err := registerControl(prefix, item.ID, item.Label, seenControls); err != nil {
			return err
		}
		if _, ok := item.Value.(string); !ok {
			return fmt.Errorf("%s.value must be a string", prefix)
		}
		if err := checkText(prefix+".placeholder", item.Placeholder, false); err != nil {
			return err
		}
	case KindSwitch:
		if err := registerControl(prefix, item.ID, item.Label, seenControls); err != nil {
			return err
		}
		if _, ok := item.Value.(bool); !ok {
			return fmt.Errorf("%s.value must be a boolean", prefix)
		}
	case KindActions:
		if len(item.Buttons) == 0 {
			return fmt.Errorf("%s.buttons must contain at least 1 button", prefix)
		}
		for bi, b := range item.Buttons {
			if err := validateButton(fmt.Sprintf("%s.buttons[%d]", prefix, bi), b, seenActions); err != nil {
				return err
			}
		}
	case KindKV:
		if len(item.Rows) == 0 {
			return fmt.Errorf("%s.rows must contain at least 1 row", prefix)
		}
		for ri, row := range item.Rows {
			if err := checkText(fmt.Sprintf("%s.rows[%d].label", prefix, ri), row.Label, true); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.rows[%d].value", prefix, ri), row.Value, true); err != nil {
				return err
			}
		}
	case KindMetrics:
		if len(item.Metrics) == 0 {
			return fmt.Errorf("%s.metrics must contain at least 1 metric", prefix)
		}
		for mi, m := range item.Metrics {
			if err := checkText(fmt.Sprintf("%s.metrics[%d].label", prefix, mi), m.Label, true); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.metrics[%d].value", prefix, mi), m.Value, true); err != nil {
				return err
			}
		}
	case KindTreemap:
		if item.Summary != nil {
			if err := checkText(prefix+".summary.total_label", item.Summary.TotalLabel, false); err != nil {
				return err
			}
			if err := checkUserText(prefix+".summary.total_value", item.Summary.TotalValue, true); err != nil {
				return err
			}
			if err := checkUserText(prefix+".summary.sub_text", item.Summary.SubText, false); err != nil {
				return err
			}
			for mi, m := range item.Summary.Metrics {
				if err := checkText(fmt.Sprintf("%s.summary.metrics[%d].label", prefix, mi), m.Label, true); err != nil {
					return err
				}
				if err := checkUserText(fmt.Sprintf("%s.summary.metrics[%d].value", prefix, mi), m.Value, true); err != nil {
					return err
				}
			}
		}
		if len(item.Tiles) == 0 {
			return fmt.Errorf("%s.tiles must contain at least 1 tile", prefix)
		}
		seenTileIDs := map[string]bool{}
		for ti, tile := range item.Tiles {
			tp := fmt.Sprintf("%s.tiles[%d]", prefix, ti)
			if strings.TrimSpace(tile.ID) == "" {
				return fmt.Errorf("%s.id is required", tp)
			}
			if seenTileIDs[tile.ID] {
				return fmt.Errorf("%s.id %q is duplicated", tp, tile.ID)
			}
			seenTileIDs[tile.ID] = true
			if err := checkText(tp+".label", tile.Label, true); err != nil {
				return err
			}
			if err := checkText(tp+".detail", tile.Detail, false); err != nil {
				return err
			}
			for ei, ent := range tile.Entries {
				ep := fmt.Sprintf("%s.entries[%d]", tp, ei)
				if err := checkUserText(ep+".name", ent.Name, true); err != nil {
					return err
				}
				if err := checkUserText(ep+".path", ent.Path, false); err != nil {
					return err
				}
			}
		}
	case KindPreview:
		if item.Preview == nil {
			return fmt.Errorf("%s.preview is required for kind \"preview\"", prefix)
		}
		pv := item.Preview
		switch pv.Variant {
		case "", "article", "cards", "diff", "plan":
		default:
			return fmt.Errorf("%s.preview.variant %q must be article, cards, diff, or plan", prefix, pv.Variant)
		}
		switch pv.Palette {
		case "", "ocean", "paper", "ink", "warm":
		default:
			return fmt.Errorf("%s.preview.palette %q must be ocean, paper, ink, or warm", prefix, pv.Palette)
		}
		if err := checkUserText(prefix+".preview.title", pv.Title, true); err != nil {
			return err
		}
		if err := checkUserText(prefix+".preview.subtitle", pv.Subtitle, false); err != nil {
			return err
		}
		if err := checkText(prefix+".preview.badge", pv.Badge, false); err != nil {
			return err
		}
		if err := checkUserText(prefix+".preview.before", pv.Before, false); err != nil {
			return err
		}
		if err := checkUserText(prefix+".preview.after", pv.After, false); err != nil {
			return err
		}
		if err := checkUserText(prefix+".preview.body", pv.Body, false); err != nil {
			return err
		}
		for ci, c := range pv.Cards {
			cp := fmt.Sprintf("%s.preview.cards[%d]", prefix, ci)
			if err := checkText(cp+".badge", c.Badge, false); err != nil {
				return err
			}
			if err := checkUserText(cp+".title", c.Title, true); err != nil {
				return err
			}
			if err := checkUserText(cp+".body", c.Body, false); err != nil {
				return err
			}
		}
	case KindLogList:
		if err := checkText(prefix+".empty_text", item.EmptyText, true); err != nil {
			return err
		}
		for ei, e := range item.Entries {
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].title", prefix, ei), e.Title, true); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].sub_text", prefix, ei), e.SubText, false); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].time", prefix, ei), e.Time, false); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].status", prefix, ei), e.Status, false); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].tooltip", prefix, ei), e.Tooltip, false); err != nil {
				return err
			}
			if e.ActionID != "" && (strings.TrimSpace(e.ActionID) == "" || len(e.ActionID) > 128 || ContainsEmoji(e.ActionID)) {
				return fmt.Errorf("%s.entries[%d].action_id is invalid", prefix, ei)
			}
		}
	default:
		return fmt.Errorf("%s.kind %q is unsupported", prefix, item.Kind)
	}
	return nil
}

func registerControl(prefix, id, label string, seen map[string]bool) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s.id is required", prefix)
	}
	if seen[id] {
		return fmt.Errorf("%s.id %q is duplicated on the page", prefix, id)
	}
	seen[id] = true
	return checkText(prefix+".label", label, true)
}

func registerControlOptionalLabel(prefix, id, label string, seen map[string]bool) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s.id is required", prefix)
	}
	if seen[id] {
		return fmt.Errorf("%s.id %q is duplicated on the page", prefix, id)
	}
	seen[id] = true
	return checkText(prefix+".label", label, false)
}

func validateButton(prefix string, b UIButton, seen map[string]bool) error {
	if strings.TrimSpace(b.ID) == "" {
		return fmt.Errorf("%s.id is required", prefix)
	}
	if seen[b.ID] {
		return fmt.Errorf("%s.id %q is duplicated", prefix, b.ID)
	}
	seen[b.ID] = true
	if err := checkText(prefix+".label", b.Label, true); err != nil {
		return err
	}
	switch b.Variant {
	case VariantPrimary, VariantSecondary, VariantDanger:
		return nil
	default:
		return fmt.Errorf("%s.variant %q must be primary, secondary, or danger", prefix, b.Variant)
	}
}

func checkText(field, val string, required bool) error {
	if required && strings.TrimSpace(val) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if ContainsEmoji(val) {
		return fmt.Errorf("%s must not contain emoji (Ocean Paper rule)", field)
	}
	return nil
}

// checkUserText validates dynamic values that may reflect user note titles, body excerpts, or folder names.
func checkUserText(field, val string, required bool) error {
	if required && strings.TrimSpace(val) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}
