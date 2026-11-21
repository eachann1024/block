package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"
	bui "block/internal/ui"
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
	if strings.TrimSpace(page) == "" || !filepath.IsLocal(filepath.FromSlash(page)) || strings.ContainsAny(page, "\\:\x00") {
		return errors.New(theme.Tr("无效的笔记路径", "Invalid note path"))
	}
	for _, part := range strings.Split(page, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return errors.New(theme.Tr("无效的笔记路径", "Invalid note path"))
		}
	}
	return nil
}

const maxPlanPages = 12

func (c *Controller) buildMockPlan(args map[string]any, root string) (string, *model.BatchPlan, []model.SourceNote) {
	scope := captureNotebook(c.host)
	if scope.root == "" || root == "" {
		return marshalJSON(map[string]any{"error": "没有可用的授权笔记本"}), nil, nil
	}
	dir, err := openNotebook(scope.root)
	if err != nil {
		return marshalJSON(map[string]any{"error": theme.Tr("没有可用的授权笔记本", "No authorized notebook is available")}), nil, nil
	}
	defer dir.Close()
	return buildBatchPlan(context.Background(), scope, dir, args)
}

func buildBatchPlan(ctx context.Context, scope notebookScope, root *os.Root, args map[string]any) (string, *model.BatchPlan, []model.SourceNote) {
	fail := func(message string) (string, *model.BatchPlan, []model.SourceNote) {
		return marshalJSON(map[string]any{"error": message}), nil, nil
	}
	rawOps, _ := args["operations"].([]any)
	if len(rawOps) == 0 || len(rawOps) > 32 {
		return fail(theme.Tr("每份计划须有 1–32 个操作", "Each plan needs 1–32 operations"))
	}
	var ops []model.PlanOp
	byPage := make(map[string]int)
	expected := make(map[string]string)
	for _, raw := range rawOps {
		if ctx.Err() != nil {
			return fail(theme.Tr("本轮已结束", "This turn has ended"))
		}
		item, ok := raw.(map[string]any)
		if !ok {
			return fail(theme.Tr("无效的计划操作", "Invalid plan operation"))
		}
		kind, _ := item["type"].(string)
		page, _ := item["pageId"].(string)
		if page == "" {
			page = scope.page
		}
		if kind == "create" {
			page, _ = item["title"].(string)
			if strings.TrimSpace(page) == "" {
				page = theme.Tr("未命名", "Untitled")
			}
		}
		name, err := noteFilename(page)
		if err != nil {
			return fail(err.Error())
		}
		page = displayPageID(name)
		idx, found := byPage[name]
		if !found {
			if len(ops) >= maxPlanPages {
				return fail(theme.Tr("每份计划最多修改 12 篇笔记", "A plan can change at most 12 notes"))
			}
			before, exists, err := snapshotNotebookNote(ctx, root, name)
			if err != nil || exists == (kind == "create") {
				return fail(theme.Tr("笔记无法用于该操作，请核对原文及目标是否存在", "This note cannot be used. Check the original text and that the target exists."))
			}
			opKind := model.PlanOpReplace
			if !exists {
				opKind = model.PlanOpCreateNote
			}
			idx = len(ops)
			byPage[name] = idx
			expected[page] = before
			ops = append(ops, model.PlanOp{Kind: opKind, Page: page, Old: before, New: before})
		} else if kind == "create" {
			return fail(theme.Tr("同一笔记只能创建一次", "Each note can only be created once"))
		}
		op := &ops[idx]
		switch kind {
		case "create", "edit":
			md, ok := item["markdown"].(string)
			if !ok {
				return fail(theme.Tr("缺少 Markdown 内容", "Missing Markdown content"))
			}
			op.New = md
		case "search_replace":
			oldText, _ := item["oldString"].(string)
			newText, ok := item["newString"].(string)
			if !ok || oldText == "" || !strings.Contains(op.New, oldText) {
				return fail(theme.Tr("操作无法按顺序应用，请核对原文", "Operations cannot be applied in order. Check the original text."))
			}
			count := 1
			if all, _ := item["replaceAll"].(bool); all {
				count = strings.Count(op.New, oldText)
			}
			// Check the size before Replace can allocate the expanded document.
			if int64(len(op.New))+int64(count)*int64(len(newText)-len(oldText)) > maxToolNoteBytes {
				return fail(theme.Tr("计划内容超过 AI 写入上限", "Plan content exceeds the AI write limit"))
			}
			op.New = strings.Replace(op.New, oldText, newText, count)
		default:
			return fail(theme.Tr("暂不支持该计划操作", "This plan operation is not supported"))
		}
		if len(op.New) > maxToolNoteBytes {
			return fail(theme.Tr("计划内容超过 AI 写入上限", "Plan content exceeds the AI write limit"))
		}
	}
	for _, op := range ops {
		if !checkMarkdownPreflight(op.Old, op.New) {
			return fail(theme.Tr("最终内容包含新增的未闭合代码围栏或 HTML 表格", "The result adds an unclosed code fence or HTML table"))
		}
	}
	summary, _ := args["summary"].(string)
	if summary == "" {
		summary, _ = args["title"].(string)
	}
	plan := &model.BatchPlan{
		ID: fmt.Sprintf("plan-%d", time.Now().UnixNano()), Summary: truncateRunes(summary, 300),
		Root: scope.root, Ops: ops, Expected: expected, Status: "pending",
	}
	return marshalJSON(map[string]any{
		"status":  "awaiting_approval",
		"message": theme.Tr("方案等待用户确认后写入，请勿重复提交。", "The plan waits for your confirmation before writing. Do not submit it again."),
	}), plan, nil
}

func (c *Controller) linkPlanLocked(convID uint64, msgIdx int) *model.BatchPlan {
	conv := c.findConvLocked(convID)
	if conv == nil || msgIdx < 0 || msgIdx >= len(conv.Messages) {
		return nil
	}
	plan := conv.Messages[msgIdx].Plan
	if plan == nil || plan.Status != "pending" || len(plan.Ops) == 0 {
		return nil
	}
	ops, err := collapsePlanOps(plan)
	if err != nil || len(ops) == 0 {
		return nil
	}
	op := ops[0]
	before, after := op.Old, op.New
	name, err := noteFilename(op.Page)
	if err != nil || plan.Root == "" {
		return nil
	}
	abs := filepath.Join(plan.Root, name)
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
	preview := *plan
	preview.Ops = ops
	return &preview
}

func (c *Controller) linkLatestPendingPlanLocked() *model.BatchPlan {
	conv := c.findConvLocked(c.currentID)
	if conv == nil {
		return nil
	}
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		p := conv.Messages[i].Plan
		if p != nil && p.Status == "pending" && len(p.Ops) > 0 {
			return c.linkPlanLocked(conv.ID, i)
		}
	}
	return nil
}

func (c *Controller) previewTurnPlan(turn *assistantTurn, plan *model.BatchPlan) {
	if turn.ctx.Err() == nil {
		c.previewPlan(turn.ctx, plan)
	}
}

func (c *Controller) previewPlanInEditor(plan *model.BatchPlan) {
	c.previewPlan(context.Background(), plan)
}

func (c *Controller) previewPlan(ctx context.Context, plan *model.BatchPlan) {
	if plan == nil || ctx.Err() != nil {
		return
	}
	c.mu.RLock()
	host := c.host
	c.mu.RUnlock()
	for _, op := range plan.Ops {
		// The editor's per-line preview has a quadratic diff. Large plans stay reviewable in chat.
		if int64(strings.Count(op.Old, "\n")+2)*int64(strings.Count(op.New, "\n")+2) > 4_000_000 {
			return
		}
	}
	if previewHost, ok := host.(interface {
		PreviewBatchPlanContext(context.Context, *model.BatchPlan) error
	}); ok {
		_ = previewHost.PreviewBatchPlanContext(ctx, plan)
	} else if host != nil {
		_ = host.PreviewPlanInEditor(plan)
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
	// Hidden table separators (and deleted lines that would merge two tables) are
	// settled from the written text before it is assembled.
	texts := make([]string, len(lines))
	changes := make([]int, len(lines))
	applied := make([]bool, len(lines))
	for i, l := range lines {
		texts[i], changes[i], applied[i] = l.Text, l.Change, l.Included
	}
	bui.SyncDiffTableSeps(texts, changes, applied)
	keep := make([]bool, len(lines))
	for i, l := range lines {
		keep[i] = l.Change == 0 || (l.Change < 0) != applied[i]
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
func collapsePlanOps(plan *model.BatchPlan) ([]model.PlanOp, error) {
	var merged []model.PlanOp
	index := make(map[string]int)
	for _, op := range plan.Ops {
		name, err := noteFilename(op.Page)
		if err != nil {
			return nil, err
		}
		if i, ok := index[name]; ok {
			if merged[i].New != op.Old || op.Kind != model.PlanOpReplace {
				return nil, errors.New(theme.Tr("计划的中间版本无法验证", "An intermediate plan version cannot be verified"))
			}
			merged[i].New = op.New
			continue
		}
		if before, ok := plan.Expected[op.Page]; !ok || before != op.Old {
			return nil, errors.New(theme.Tr("计划缺少可验证的原文", "The plan is missing verifiable original text"))
		}
		index[name] = len(merged)
		merged = append(merged, op)
	}
	return merged, nil
}

func (c *Controller) AcceptPlan(convID uint64, msgIdx int) error {
	return c.applyReviewedPlan(convID, msgIdx, nil, false)
}

// AcceptEditorPlan applies only the diff selection from the linked editor preview.
func (c *Controller) AcceptEditorPlan(review *model.BatchPlan) error {
	c.mu.RLock()
	active := c.activePlan
	c.mu.RUnlock()
	if review == nil || active == nil || active.Plan.ID != review.ID {
		return errors.New(theme.Tr("计划预览已失效", "The plan preview is no longer valid"))
	}
	return c.applyReviewedPlan(active.ConvID, active.MsgIndex, review, false)
}

func (c *Controller) UndoPlan(convID uint64, msgIdx int) error {
	return c.applyReviewedPlan(convID, msgIdx, nil, true)
}

func (c *Controller) applyReviewedPlan(convID uint64, msgIdx int, review *model.BatchPlan, undo bool) error {
	c.mu.Lock()
	conv := c.findConvLocked(convID)
	if conv == nil || c.currentID != convID || msgIdx < 0 || msgIdx >= len(conv.Messages) || !c.providerCfg.Enabled {
		c.mu.Unlock()
		return errors.New(theme.Tr("计划已失效", "The plan is no longer valid"))
	}
	plan := conv.Messages[msgIdx].Plan
	wantStatus := "pending"
	if undo {
		wantStatus = "applied"
	}
	if plan == nil || plan.Status != wantStatus || c.planInFlight != "" ||
		(review != nil && (c.activePlan == nil || c.activePlan.ConvID != convID || c.activePlan.MsgIndex != msgIdx || review.ID != plan.ID)) {
		c.mu.Unlock()
		return errors.New(theme.Tr("计划无法在当前状态下执行", "The plan cannot run in its current state"))
	}
	selected, err := collapsePlanOps(plan)
	if err == nil && review != nil && len(review.Ops) != len(selected) {
		err = errors.New(theme.Tr("计划预览已失效", "The plan preview is no longer valid"))
	}
	if err == nil && !undo {
		for i := range selected {
			op := &selected[i]
			if review == nil {
				op.New = c.selectedPlanTextLocked(convID, msgIdx, op.Page, op.Old, op.New)
			} else {
				preview := review.Ops[i]
				if preview.Page != op.Page || preview.Old != op.Old || preview.Kind != op.Kind {
					err = errors.New(theme.Tr("计划预览已失效", "The plan preview is no longer valid"))
					break
				}
				op.New = preview.New
			}
		}
	}
	writePlan := *plan
	writePlan.Ops = selected
	host := c.host
	c.planInFlight = plan.ID
	c.mu.Unlock()

	if err == nil {
		switch {
		case host == nil:
			err = errors.New(theme.Tr("没有可用的工作区", "No workspace is available"))
		case undo:
			err = host.UndoLastPlan(&writePlan)
		default:
			err = host.ApplyBatchPlan(&writePlan)
		}
	}

	c.mu.Lock()
	c.planInFlight = ""
	if err != nil {
		c.turnError = err.Error()
	} else {
		if undo {
			plan.Status = "undone"
		} else {
			plan.Ops = selected
			plan.Status = "applied"
		}
		if c.activePlan != nil && c.activePlan.Plan.ID == plan.ID {
			c.clearActivePlanLocked()
		}
		c.turnError = ""
		c.persistChatsLocked()
	}
	c.mu.Unlock()
	c.requestRedraw()
	return err
}

// DismissEditorPlan updates the controller when the review was rejected from the editor.
func (c *Controller) DismissEditorPlan(planID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conv := c.findConvLocked(c.currentID)
	if conv == nil || planID == "" || c.planInFlight == planID {
		return
	}
	for _, message := range conv.Messages {
		if plan := message.Plan; plan != nil && plan.ID == planID && plan.Status == "pending" {
			plan.Status = "rejected"
			if c.activePlan != nil && c.activePlan.Plan.ID == planID {
				c.clearActivePlanLocked()
			}
			c.persistChatsLocked()
			return
		}
	}
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
	if plan == nil || plan.Status != "pending" || c.planInFlight == plan.ID {
		c.mu.Unlock()
		return nil
	}
	plan.Status = "rejected"
	if c.activePlan != nil && c.activePlan.ConvID == convID && c.activePlan.MsgIndex == msgIdx {
		c.clearActivePlanLocked()
	}
	host := c.host
	c.persistChatsLocked()
	c.mu.Unlock()
	if host != nil {
		_ = host.RejectBatchPlan(plan.ID)
	}
	c.requestRedraw()
	return nil
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
	turn := c.turn
	if qcard == nil || qcard.Submitted || c.currentID != convID ||
		(!c.mockResponder && (!c.currentTurnLocked(turn) || turn.questionIndex != msgIdx)) {
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
	step := 0
	if !isMock && isBuiltin && toolCallID != "" {
		step = turn.nextStep
		turn.questionIndex = -1
		c.streaming = true
	}
	c.persistChatsLocked()
	c.mu.Unlock()
	c.requestRedraw()

	if step > 0 {
		c.startBuiltinTurn(turn, step)
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
	if card == nil || card.Decision != model.ApprovalPending || !c.mockResponder || c.currentID != convID ||
		(decision != model.ApprovalAllow && decision != model.ApprovalDeny && decision != model.ApprovalSkip) {
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
