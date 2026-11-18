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
	KindSwitch    ItemKind = "switch"
	KindActions   ItemKind = "actions"
	KindKV        ItemKind = "kv"
	KindMetrics   ItemKind = "metrics"
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
	Value string `json:"value"`
	Label string `json:"label"`
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
}

type UILogEntry struct {
	Title  string `json:"title"`
	Time   string `json:"time"`
	Status string `json:"status,omitempty"`
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
	Kind        ItemKind       `json:"kind"`
	ID          string         `json:"id,omitempty"`
	Label       string         `json:"label,omitempty"`
	Value       any            `json:"value,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Options     []UIOption     `json:"options,omitempty"`
	Buttons     []UIButton     `json:"buttons,omitempty"`
	Rows        []UIKVRow      `json:"rows,omitempty"`
	Metrics     []UIMetric     `json:"metrics,omitempty"`
	Preview     *UIPreview     `json:"preview,omitempty"`
	EmptyText   string         `json:"empty_text,omitempty"`
	Entries     []UILogEntry   `json:"entries,omitempty"`
	VisibleWhen *UIVisibleWhen `json:"visible_when,omitempty"`
}

type UICard struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Items []UIItem `json:"items"`
}

type UIPage struct {
	Title         string     `json:"title"`
	Status        string     `json:"status,omitempty"`
	HeaderActions []UIButton `json:"header_actions,omitempty"`
	Cards         []UICard   `json:"cards"`
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

func (p *UIPage) Validate() error {
	if err := checkText("page.title", p.Title, true); err != nil {
		return err
	}
	if err := checkUserText("page.status", p.Status, false); err != nil {
		return err
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
		if err := checkText(prefix+".title", card.Title, true); err != nil {
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

// ExtractDefaultValues collects current form control values keyed by control ID.
func (p *UIPage) ExtractDefaultValues() map[string]any {
	out := map[string]any{}
	for _, card := range p.Cards {
		for _, item := range card.Items {
			switch item.Kind {
			case KindSegmented, KindInput, KindFolder, KindSecret, KindSelect, KindSwitch:
				if item.ID != "" {
					out[item.ID] = item.Value
				}
			}
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
	if item.VisibleWhen != nil {
		if strings.TrimSpace(item.VisibleWhen.ControlID) == "" {
			return fmt.Errorf("%s.visible_when.control_id is required", prefix)
		}
		if item.VisibleWhen.Equals == nil && len(item.VisibleWhen.In) == 0 {
			return fmt.Errorf("%s.visible_when requires equals or in", prefix)
		}
	}

	switch item.Kind {
	case KindSegmented, KindSelect:
		if err := registerControl(prefix, item.ID, item.Label, seenControls); err != nil {
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
			if err := checkText(fmt.Sprintf("%s.entries[%d].time", prefix, ei), e.Time, true); err != nil {
				return err
			}
			if err := checkUserText(fmt.Sprintf("%s.entries[%d].status", prefix, ei), e.Status, false); err != nil {
				return err
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
