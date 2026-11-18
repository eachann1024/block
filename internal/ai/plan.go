package ai

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"
)

type planSelKey struct {
	convID uint64
	msgIdx int
	page   string
}

type diffSelection struct {
	before   string
	after    string
	excluded map[int]bool
	raw      []rawDiffLine
}

type rawDiffLine struct {
	change int
	text   string
}

// PlanDiffLine represents one line in a selective diff review.
type PlanDiffLine struct {
	Index          int
	SelectionIndex int
	Change         int // -1 delete, 0 equal, +1 insert
	Text           string
	Included       bool
}

// ActivePlanPreview describes the currently linked plan preview in the editor.
type ActivePlanPreview struct {
	ConvID     uint64
	MsgIndex   int
	Plan       *model.BatchPlan
	Page       string
	Path       string
	BeforeText string
	AfterText  string
	IsCreate   bool
}

type planReceipt struct {
	Page    string
	AbsPath string
	Before  string
	After   string
	Created bool
}

var (
	planReceiptsMu = make(map[string][]planReceipt)
)

func unclosedConstructs(src string) []string {
	var issues []string
	var fenceChar byte
	var fenceWidth int
	inFence := false
	inTable := false

	for _, raw := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(raw, " \t")
		if inFence {
			w := 0
			for w < len(trimmed) && trimmed[w] == fenceChar {
				w++
			}
			if w >= fenceWidth && strings.TrimSpace(trimmed[w:]) == "" {
				inFence = false
			}
			continue
		}
		if len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			ch := trimmed[0]
			w := 0
			for w < len(trimmed) && trimmed[w] == ch {
				w++
			}
			if w >= 3 && !(ch == '`' && strings.ContainsRune(trimmed[w:], '`')) {
				inFence = true
				fenceChar = ch
				fenceWidth = w
				continue
			}
		}
		low := strings.ToLower(trimmed)
		if strings.HasPrefix(low, "<table") && !strings.Contains(low, "</table>") {
			inTable = true
		}
		if inTable && strings.Contains(low, "</table>") {
			inTable = false
		}
	}
	if inFence {
		issues = append(issues, "unclosed_fence")
	}
	if inTable {
		issues = append(issues, "unclosed_html_table")
	}
	return issues
}

func checkMarkdownPreflight(before, after string) bool {
	prev := unclosedConstructs(before)
	for _, code := range unclosedConstructs(after) {
		found := false
		for i, p := range prev {
			if p == code {
				prev = append(prev[:i], prev[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func validateRelativePagePath(page string) error {
	if strings.TrimSpace(page) == "" || filepath.IsAbs(page) || strings.HasPrefix(page, "/") || strings.HasPrefix(page, "\\") {
		return errors.New("Invalid page path")
	}
	for _, part := range strings.Split(filepath.ToSlash(page), "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("Invalid page path")
		}
	}
	return nil
}

func (c *Controller) buildBatchPlanLocked(args map[string]any, root string) (string, *model.BatchPlan, []model.SourceNote) {
	var rawOps []any
	switch v := args["operations"].(type) {
	case []any:
		rawOps = v
	case []map[string]any:
		for _, item := range v {
			rawOps = append(rawOps, item)
		}
	}
	if len(rawOps) == 0 {
		return marshalJSON(map[string]any{"error": "operations 为空"}), nil, nil
	}

	currentPage := c.activePageLocked(root)
	var ops []model.PlanOp
	expected := make(map[string]string)
	existsMap := make(map[string]bool)

	for _, item := range rawOps {
		om, ok := item.(map[string]any)
		if !ok {
			continue
		}
		opType, _ := om["type"].(string)
		page, _ := om["pageId"].(string)
		if page == "" {
			page = currentPage
		}

		switch opType {
		case "create":
			title, _ := om["title"].(string)
			if strings.TrimSpace(title) == "" {
				title = theme.Tr("未命名", "Untitled")
			}
			md, _ := om["markdown"].(string)
			ops = append(ops, model.PlanOp{
				Kind: model.PlanOpCreateNote,
				Page: title,
				Old:  "",
				New:  md,
			})
		case "edit":
			md, _ := om["markdown"].(string)
			ops = append(ops, model.PlanOp{
				Kind: model.PlanOpReplace,
				Page: page,
				Old:  "",
				New:  md,
			})
		case "search_replace":
			oldStr, _ := om["oldString"].(string)
			newStr, _ := om["newString"].(string)
			replaceAll, _ := om["replaceAll"].(bool)
			cur := c.readOrSimPage(root, page, ops, expected, existsMap)
			if oldStr == "" || !strings.Contains(cur, oldStr) {
				return marshalJSON(map[string]any{
					"error": map[string]any{
						"code":    "operation_precondition_failed",
						"message": "操作无法按顺序应用，请核对原文并修正。",
					},
				}), nil, nil
			}
			next := ""
			if replaceAll {
				next = strings.ReplaceAll(cur, oldStr, newStr)
			} else {
				next = strings.Replace(cur, oldStr, newStr, 1)
			}
			ops = append(ops, model.PlanOp{
				Kind: model.PlanOpReplace,
				Page: page,
				Old:  cur,
				New:  next,
			})
		default:
			return marshalJSON(map[string]any{"error": "暂不支持的操作：" + opType}), nil, nil
		}
	}

	if len(ops) == 0 {
		return marshalJSON(map[string]any{"error": "operations 为空"}), nil, nil
	}

	sim := make(map[string]string)
	for i := range ops {
		op := &ops[i]
		if err := validateRelativePagePath(op.Page); err != nil {
			return marshalJSON(map[string]any{"error": err.Error()}), nil, nil
		}
		if _, seen := expected[op.Page]; !seen {
			md, exists := c.readPageWithExistsLocked(root, op.Page)
			expected[op.Page] = md
			existsMap[op.Page] = exists
		}
		if op.Kind == model.PlanOpCreateNote {
			if existsMap[op.Page] {
				return marshalJSON(map[string]any{"error": "目标笔记已存在：" + op.Page}), nil, nil
			}
			sim[op.Page] = op.New
		} else {
			cur := expected[op.Page]
			if prev, ok := sim[op.Page]; ok {
				cur = prev
			}
			if op.Old == "" {
				op.Old = cur
			}
			sim[op.Page] = op.New
		}
	}

	for page, after := range sim {
		if !checkMarkdownPreflight(expected[page], after) {
			return marshalJSON(map[string]any{
				"error": map[string]any{
					"code":    "unsupported_markdown",
					"message": "最终内容包含未闭合的代码围栏或 HTML 表格，请修正后最多重试一次。",
				},
			}), nil, nil
		}
	}

	title, _ := args["title"].(string)
	summary, _ := args["summary"].(string)
	if summary == "" {
		summary = title
	}
	plan := &model.BatchPlan{
		ID:       fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Summary:  summary,
		Root:     root,
		Ops:      ops,
		Expected: expected,
		Status:   "pending",
	}
	return marshalJSON(map[string]any{
		"status":  "awaiting_approval",
		"message": "方案已生成，尚未修改笔记。等待用户在卡片中确认，不要重复提交。",
	}), plan, nil
}

func (c *Controller) readOrSimPage(root, page string, ops []model.PlanOp, expected map[string]string, existsMap map[string]bool) string {
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].Page == page {
			return ops[i].New
		}
	}
	if v, ok := expected[page]; ok {
		return v
	}
	md, exists := c.readPageWithExistsLocked(root, page)
	expected[page] = md
	existsMap[page] = exists
	return md
}

func (c *Controller) readPageWithExistsLocked(root, page string) (string, bool) {
	if page == "" {
		return "", false
	}
	abs := page
	if !filepath.IsAbs(abs) && root != "" {
		abs = filepath.Join(root, filepath.FromSlash(page)+".md")
	}
	if c.host != nil {
		if txt, err := c.host.ReadNote(abs); err == nil {
			return txt, true
		}
	}
	if abs != "" {
		if b, err := os.ReadFile(abs); err == nil {
			return string(b), true
		}
	}
	return "", false
}

func (c *Controller) linkPlanLocked(convID uint64, msgIdx int) {
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		return
	}
	plan := conv.Messages[msgIdx].Plan
	if plan == nil || plan.Status != "pending" || len(plan.Ops) == 0 {
		return
	}
	op := plan.Ops[0]
	before := plan.Expected[op.Page]
	after := op.New
	for _, o := range plan.Ops {
		if o.Page == op.Page {
			after = o.New
		}
	}
	abs := op.Page + ".md"
	if plan.Root != "" {
		abs = filepath.Join(plan.Root, filepath.FromSlash(op.Page)+".md")
	}
	isCreate := op.Kind == model.PlanOpCreateNote

	c.activePlan = &ActivePlanPreview{
		ConvID:     convID,
		MsgIndex:   msgIdx,
		Plan:       plan,
		Page:       op.Page,
		Path:       abs,
		BeforeText: before,
		AfterText:  after,
		IsCreate:   isCreate,
	}
	c.computePlanDiffLocked(convID, msgIdx, op.Page, before, after)
	if c.host != nil {
		_ = c.host.PreviewPlanInEditor(plan)
	}
}

func (c *Controller) linkLatestPendingPlanLocked() {
	conv := c.findConvLocked(c.currentID)
	if conv == nil {
		return
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		p := conv.Messages[i].Plan
		if p != nil && p.Status == "pending" && len(p.Ops) > 0 {
			c.linkPlanLocked(conv.ID, i)
			return
		}
	}
}

func (c *Controller) clearActivePlanLocked() {
	c.activePlan = nil
}

// ActivePlan returns the currently linked editor plan preview, if any.
func (c *Controller) ActivePlan() *ActivePlanPreview {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.activePlan == nil {
		return nil
	}
	cp := *c.activePlan
	return &cp
}

// PlanDiffLines returns the selective diff lines for the specified plan page.
func (c *Controller) PlanDiffLines(convID uint64, msgIdx int, page, before, after string) []PlanDiffLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.computePlanDiffLocked(convID, msgIdx, page, before, after)
}

func (c *Controller) computePlanDiffLocked(convID uint64, msgIdx int, page, before, after string) []PlanDiffLine {
	key := planSelKey{convID: convID, msgIdx: msgIdx, page: page}
	sel, ok := c.planSelections[key]
	if !ok || sel.before != before || sel.after != after {
		sel = &diffSelection{
			before:   before,
			after:    after,
			excluded: make(map[int]bool),
			raw:      computeLCSLines(before, after),
		}
		c.planSelections[key] = sel
	}
	groups := groupDiffLines(sel.raw)
	out := make([]PlanDiffLine, len(sel.raw))
	for i, r := range sel.raw {
		g := groups[i]
		out[i] = PlanDiffLine{
			Index:          i,
			SelectionIndex: g,
			Change:         r.change,
			Text:           r.text,
			Included:       !sel.excluded[g],
		}
	}
	return out
}

// TogglePlanDiffLine toggles inclusion of a diff hunk/row in a pending plan.
func (c *Controller) TogglePlanDiffLine(convID uint64, msgIdx int, page string, selectionIndex int) {
	c.mu.Lock()
	key := planSelKey{convID: convID, msgIdx: msgIdx, page: page}
	if sel, ok := c.planSelections[key]; ok {
		if sel.excluded[selectionIndex] {
			delete(sel.excluded, selectionIndex)
		} else {
			sel.excluded[selectionIndex] = true
		}
	}
	c.mu.Unlock()
	c.requestRedraw()
}

// SelectedPlanText returns the reconstructed Markdown text applying only included diff lines.
func (c *Controller) SelectedPlanText(convID uint64, msgIdx int, page, before, after string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selectedPlanTextLocked(convID, msgIdx, page, before, after)
}

func (c *Controller) selectedPlanTextLocked(convID uint64, msgIdx int, page, before, after string) string {
	key := planSelKey{convID: convID, msgIdx: msgIdx, page: page}
	sel, ok := c.planSelections[key]
	if !ok || len(sel.excluded) == 0 {
		return after
	}
	lines := c.computePlanDiffLocked(convID, msgIdx, page, before, after)
	keep := make([]bool, len(lines))
	for i, l := range lines {
		keep[i] = l.Change == 0 || (l.Change < 0 && !l.Included) || (l.Change > 0 && l.Included)
	}
	// Keep markdown pipe table separator only when at least one data row survives.
	for i := 0; i < len(lines); {
		trim := strings.TrimSpace(lines[i].Text)
		if strings.HasPrefix(trim, "|") {
			start := i
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i].Text), "|") && lines[i].Change == lines[start].Change {
				i++
			}
			anyRow := false
			for j := start; j < i; j++ {
				if !isPipeSeparatorLine(strings.TrimSpace(lines[j].Text)) && keep[j] {
					anyRow = true
					break
				}
			}
			for j := start; j < i; j++ {
				if isPipeSeparatorLine(strings.TrimSpace(lines[j].Text)) {
					keep[j] = anyRow
				}
			}
			continue
		}
		i++
	}
	var sb strings.Builder
	for i, l := range lines {
		if !keep[i] {
			continue
		}
		if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteByte('\n')
		}
		sb.WriteString(l.Text)
	}
	return sb.String()
}

// AcceptPlan applies the pending plan to disk/host and records receipts for Undo.
func (c *Controller) AcceptPlan(convID uint64, msgIdx int) error {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		c.mu.Unlock()
		return errors.New("plan not found")
	}
	plan := conv.Messages[msgIdx].Plan
	if plan == nil || plan.Status != "pending" {
		c.mu.Unlock()
		return errors.New("plan is not pending")
	}

	pageWork := make(map[string]struct {
		before   string
		after    string
		isCreate bool
	})
	var order []string
	for _, op := range plan.Ops {
		w, ok := pageWork[op.Page]
		if !ok {
			order = append(order, op.Page)
			w.before = plan.Expected[op.Page]
			w.isCreate = op.Kind == model.PlanOpCreateNote
		}
		w.after = op.New
		pageWork[op.Page] = w
	}

	var receipts []planReceipt
	var firstCreatedPath string
	for _, page := range order {
		w := pageWork[page]
		selected := c.selectedPlanTextLocked(convID, msgIdx, page, w.before, w.after)
		if !checkMarkdownPreflight(w.before, selected) {
			c.turnError = theme.Tr("所选修改包含新增的不支持 Markdown 结构，请调整后重试。", "Selected changes introduce unsupported Markdown.")
			c.mu.Unlock()
			c.requestRedraw()
			return errors.New(c.turnError)
		}
		abs := page + ".md"
		if plan.Root != "" {
			abs = filepath.Join(plan.Root, filepath.FromSlash(page)+".md")
		}
		if err := c.writeNoteFileLocked(abs, selected); err != nil {
			c.turnError = err.Error()
			c.mu.Unlock()
			c.requestRedraw()
			return err
		}
		if w.isCreate && firstCreatedPath == "" {
			firstCreatedPath = abs
		}
		receipts = append(receipts, planReceipt{
			Page:    page,
			AbsPath: abs,
			Before:  w.before,
			After:   selected,
			Created: w.isCreate,
		})
	}

	if c.host != nil {
		if err := c.host.ApplyBatchPlan(plan); err != nil {
			c.turnError = err.Error()
			c.mu.Unlock()
			c.requestRedraw()
			return err
		}
		if firstCreatedPath != "" {
			_ = c.host.OpenNote(firstCreatedPath, 0)
		}
	}

	planReceiptsMu[plan.ID] = receipts
	plan.Status = "applied"
	c.clearActivePlanLocked()
	delete(c.planSelections, planSelKey{convID: convID, msgIdx: msgIdx})
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

// RejectPlan cancels a pending plan without touching any files on disk.
func (c *Controller) RejectPlan(convID uint64, msgIdx int) error {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		c.mu.Unlock()
		return errors.New("plan not found")
	}
	plan := conv.Messages[msgIdx].Plan
	if plan == nil || plan.Status != "pending" {
		c.mu.Unlock()
		return nil
	}
	plan.Status = "rejected"
	if c.activePlan != nil && c.activePlan.ConvID == convID && c.activePlan.MsgIndex == msgIdx {
		c.clearActivePlanLocked()
	}
	if c.host != nil {
		_ = c.host.RejectBatchPlan(plan.ID)
	}
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

// UndoPlan reverts an applied plan back to its pre-plan snapshot.
func (c *Controller) UndoPlan(convID uint64, msgIdx int) error {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		c.mu.Unlock()
		return errors.New("plan not found")
	}
	plan := conv.Messages[msgIdx].Plan
	if plan == nil || plan.Status != "applied" {
		c.mu.Unlock()
		return errors.New("plan is not applied")
	}
	receipts := planReceiptsMu[plan.ID]
	for _, r := range receipts {
		cur, exists := c.readPageWithExistsLocked(plan.Root, r.Page)
		if !exists || cur != r.After {
			plan.Status = "undo_conflict"
			c.persistChatsLocked()
			c.mu.Unlock()
			c.requestRedraw()
			return errors.New(theme.Tr("笔记已被修改，存在撤回冲突", "Note changed since apply; undo conflict"))
		}
	}
	for i := len(receipts) - 1; i >= 0; i-- {
		r := receipts[i]
		if r.Created {
			_ = os.Remove(r.AbsPath)
		} else if err := c.writeNoteFileLocked(r.AbsPath, r.Before); err != nil {
			c.turnError = err.Error()
			c.mu.Unlock()
			c.requestRedraw()
			return err
		}
	}
	if c.host != nil {
		_ = c.host.UndoLastPlan(plan)
	}
	plan.Status = "undone"
	delete(planReceiptsMu, plan.ID)
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()
	return nil
}

func (c *Controller) writeNoteFileLocked(absPath, content string) error {
	if c.host != nil {
		return c.host.WriteNote(absPath, content)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte(content), 0o644)
}

func computeLCSLines(before, after string) []rawDiffLine {
	left := splitInclusiveLines(before)
	right := splitInclusiveLines(after)
	if len(left)*len(right) > 4_000_000 {
		var out []rawDiffLine
		for _, l := range left {
			out = append(out, rawDiffLine{change: -1, text: l})
		}
		for _, r := range right {
			out = append(out, rawDiffLine{change: 1, text: r})
		}
		return out
	}
	w := len(right) + 1
	dp := make([]int, (len(left)+1)*w)
	for r := len(left) - 1; r >= 0; r-- {
		for c := len(right) - 1; c >= 0; c-- {
			if left[r] == right[c] {
				dp[r*w+c] = 1 + dp[(r+1)*w+c+1]
			} else {
				a, b := dp[(r+1)*w+c], dp[r*w+c+1]
				if a >= b {
					dp[r*w+c] = a
				} else {
					dp[r*w+c] = b
				}
			}
		}
	}
	var out []rawDiffLine
	r, c := 0, 0
	for r < len(left) || c < len(right) {
		if r < len(left) && c < len(right) && left[r] == right[c] {
			out = append(out, rawDiffLine{change: 0, text: left[r]})
			r++
			c++
		} else if r < len(left) && (c == len(right) || dp[(r+1)*w+c] >= dp[r*w+c+1]) {
			out = append(out, rawDiffLine{change: -1, text: left[r]})
			r++
		} else {
			out = append(out, rawDiffLine{change: 1, text: right[c]})
			c++
		}
	}
	return out
}

func splitInclusiveLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func isPipeSeparatorLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	for _, r := range trimmed {
		switch r {
		case '|', ' ', '-', ':':
		default:
			return false
		}
	}
	return true
}

func groupDiffLines(raw []rawDiffLine) []int {
	groups := make([]int, len(raw))
	hasPrev := false
	prevChange := 0
	prevOrdinary := false
	prevGroup := 0

	for i, r := range raw {
		trim := strings.TrimSpace(r.text)
		if r.change == 0 {
			hasPrev = false
			groups[i] = i
		} else if trim == "" {
			if hasPrev && prevChange == r.change {
				groups[i] = prevGroup
			} else {
				groups[i] = i
			}
		} else {
			ordinary := !strings.HasPrefix(trim, "|") && !strings.HasPrefix(strings.ToLower(trim), "<tr")
			if hasPrev && prevChange == r.change && prevOrdinary && ordinary {
				groups[i] = prevGroup
			} else {
				groups[i] = i
			}
			hasPrev = true
			prevChange = r.change
			prevOrdinary = ordinary
			prevGroup = groups[i]
		}
	}
	return groups
}

func parseQuestionCard(reqID string, args map[string]any) *model.QuestionCard {
	rawList, _ := args["questions"].([]any)
	if len(rawList) == 0 && (args["q"] != nil || args["question"] != nil) {
		rawList = []any{args}
	}
	var items []model.QuestionItem
	for i, raw := range rawList {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		prompt, _ := m["q"].(string)
		if prompt == "" {
			prompt, _ = m["question"].(string)
		}
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			continue
		}
		multi, _ := m["multiSelect"].(bool)
		if !multi {
			if t, _ := m["type"].(string); t == "check" || t == "checkbox" || t == "multi" {
				multi = true
			}
		}
		var opts []model.QuestionOption
		if rawOpts, ok := m["options"].([]any); ok {
			for _, o := range rawOpts {
				switch v := o.(type) {
				case string:
					if s := strings.TrimSpace(v); s != "" {
						opts = append(opts, model.QuestionOption{Label: s})
					}
				case map[string]any:
					lbl, _ := v["label"].(string)
					desc, _ := v["description"].(string)
					if s := strings.TrimSpace(lbl); s != "" {
						opts = append(opts, model.QuestionOption{Label: s, Description: desc})
					}
				}
			}
		}
		items = append(items, model.QuestionItem{
			ID:          fmt.Sprintf("q-%d", i),
			Prompt:      prompt,
			Options:     opts,
			MultiSelect: multi,
		})
	}
	return &model.QuestionCard{
		RequestID: reqID,
		Questions: items,
	}
}

// SubmitQuestion submits or skips an interactive question card and resumes builtin turns when applicable.
func (c *Controller) SubmitQuestion(convID uint64, msgIdx int, skipped bool) {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		c.mu.Unlock()
		return
	}
	qcard := conv.Messages[msgIdx].Question
	if qcard == nil || qcard.Submitted {
		c.mu.Unlock()
		return
	}
	qKey := fmt.Sprintf("q-%d-%d", convID, msgIdx)
	st := c.questionUI[qKey]
	answersMap := make(map[string]string)
	var items []map[string]any

	for qi := range qcard.Questions {
		q := &qcard.Questions[qi]
		var chosenLabels []string
		q.Chosen = nil
		if !skipped && st != nil {
			for idx, on := range st.answers[qi] {
				if on && idx >= 0 && idx < len(q.Options) {
					q.Chosen = append(q.Chosen, idx)
					chosenLabels = append(chosenLabels, q.Options[idx].Label)
				}
			}
			if custom := strings.TrimSpace(st.customText[qi]); custom != "" {
				q.CustomText = custom
				chosenLabels = append(chosenLabels, custom)
			}
		}
		joined := strings.Join(chosenLabels, ", ")
		if joined != "" {
			answersMap[q.Prompt] = joined
		}
		items = append(items, map[string]any{
			"question": q.Prompt,
			"selected": chosenLabels,
			"answer":   joined,
		})
	}
	qcard.Submitted = true
	if st != nil {
		st.submitted = true
	}

	status := "answered"
	if skipped && len(answersMap) == 0 {
		status = "skipped"
	}
	payload := marshalJSON(map[string]any{
		"status":  status,
		"answers": answersMap,
		"items":   items,
	})
	toolCallID := conv.Messages[msgIdx].ToolCallID
	isMock := c.mockResponder
	isBuiltin := conv.Backend == "" || conv.Backend == model.BackendBuiltin
	if toolCallID != "" {
		c.pushMessageLocked(model.ChatMessage{
			Role:       "tool",
			Content:    payload,
			ToolCallID: toolCallID,
		})
	} else {
		c.pushMessageLocked(model.ChatMessage{
			Role:    "assistant",
			Content: theme.Tr("已收到你的选择。", "Preferences recorded."),
		})
	}
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()

	if !isMock && isBuiltin && toolCallID != "" {
		c.startBuiltinTurn(convID, 1)
	}
}

// RespondApproval records an approval card decision (Allow/Deny/Skip) with retry on failure.
func (c *Controller) RespondApproval(convID uint64, msgIdx int, decision model.ApprovalDecision) bool {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		c.mu.Unlock()
		return false
	}
	card := conv.Messages[msgIdx].Approval
	if card == nil {
		c.mu.Unlock()
		return false
	}
	if c.mockApprovalFailOnce {
		c.mockApprovalFailOnce = false
		card.Failed = true
		card.ErrorText = theme.Tr("审批提交失败，请重试", "Approval submission failed, please retry")
		aKey := fmt.Sprintf("a-%d-%d", convID, msgIdx)
		if st := c.approvalUI[aKey]; st != nil {
			st.submitted = false
		}
		c.mu.Unlock()
		c.requestRedraw()
		return false
	}
	ext := c.activeExternal
	reqID := card.RequestID
	c.mu.Unlock()

	if ext != nil {
		ext.mu.Lock()
		fn := ext.respond
		ext.mu.Unlock()
		if fn != nil {
			if err := fn(reqID, decision); err != nil {
				c.mu.Lock()
				card.Failed = true
				card.ErrorText = err.Error()
				aKey := fmt.Sprintf("a-%d-%d", convID, msgIdx)
				if st := c.approvalUI[aKey]; st != nil {
					st.submitted = false
				}
				c.mu.Unlock()
				c.requestRedraw()
				return false
			}
		}
	}

	c.mu.Lock()
	card.Decision = decision
	card.Failed = false
	card.ErrorText = ""
	aKey := fmt.Sprintf("a-%d-%d", convID, msgIdx)
	if st := c.approvalUI[aKey]; st != nil {
		st.submitted = true
	}
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()
	return true
}
