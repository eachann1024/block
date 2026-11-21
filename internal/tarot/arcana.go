package tarot

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"block/internal/model"
	"block/internal/theme"
)

// Arcana describes one of the 22 Major Arcana cards in the Tarot deck.
type Arcana struct {
	ID      int
	Key     string
	Num     string
	NameZh  string
	NameEn  string
	RoleZh  string
	RoleEn  string
	RevZh   string
	RevEn   string
}

func (a Arcana) Name() string {
	return theme.Tr(a.NameZh, a.NameEn)
}

func (a Arcana) RoleHint(reversed bool) string {
	if reversed {
		return theme.Tr(a.RevZh, a.RevEn)
	}
	return theme.Tr(a.RoleZh, a.RoleEn)
}

// MajorArcana lists all 22 Major Arcana in canonical order (0..XXI).
var MajorArcana = [22]Arcana{
	{0, "fool", "0", "愚者", "The Fool", "尚未落笔的新起点", "An unwritten beginning", "迟迟没有迈出的第一步", "A first step still hesitated"},
	{1, "magician", "I", "魔术师", "The Magician", "把想法落地的行动与计划", "Turning intent into plan", "目标铺得太开，精力分散", "Goals spread too thin"},
	{2, "priestess", "II", "女祭司", "The High Priestess", "静默观察、记录与直觉", "Quiet record and intuition", "记下来却尚未消化", "Recorded but not yet absorbed"},
	{3, "empress", "III", "女皇", "The Empress", "丰饶生长与从容积累", "Abundant growth and craft", "堆叠过多，缺少修剪", "Overgrown and due for pruning"},
	{4, "emperor", "IV", "皇帝", "The Emperor", "秩序、边界与长期框架", "Structure and clear bounds", "规则过紧，压住了灵活性", "Structure turning rigid"},
	{5, "hierophant", "V", "教皇", "The Hierophant", "方法论、经验与传承", "Method and craft discipline", "沿用惯例却忘了为什么", "Habit without questioning why"},
	{6, "lovers", "VI", "恋人", "The Lovers", "取舍、对齐与重要决定", "Choice and alignment", "两头都想要，反而停住", "Wanting both sides at once"},
	{7, "chariot", "VII", "战车", "The Chariot", "推进、冲刺与突破瓶颈", "Momentum and breakthrough", "跑得太快，方向偏了半度", "Moving fast off course"},
	{8, "strength", "VIII", "力量", "Strength", "克制、韧性与持久习惯", "Quiet persistence", "用力过猛，容易半途疲惫", "Pushing harder than sustainable"},
	{9, "hermit", "IX", "隐士", "The Hermit", "独处沉淀与旧日回望", "Solitude and looking back", "关起门太久，缺一个反面声音", "Too long alone without pushback"},
	{10, "wheel", "X", "命运之轮", "Wheel of Fortune", "流转、清单与阶段切换", "Turning cycles and checklists", "被琐事推着转，忘了主线", "Carried along by routine"},
	{11, "justice", "XI", "正义", "Justice", "复盘、权衡与诚实判断", "Honest review and balance", "替自己找了太顺耳的解释", "An explanation too comfortable"},
	{12, "hanged", "XII", "倒吊人", "The Hanged Man", "停在半路，换个角度看", "Paused mid-way for perspective", "卡在同一个结上反复打转", "Stuck circling the same knot"},
	{13, "death", "XIII", "死神", "Death", "断舍离与彻底翻篇", "Clean endings and renewal", "舍不得删掉已经过时的段落", "Holding onto outdated lines"},
	{14, "temperance", "XIV", "节制", "Temperance", "节奏调和与少即是多", "Measured pace and harmony", "节奏失衡，急于一次写完", "Rushing what needs balance"},
	{15, "devil", "XV", "恶魔", "The Devil", "执念、惯性与隐藏成本", "Hidden attachments and traps", "被旧假设绑住手脚", "Bound by an old assumption"},
	{16, "tower", "XVI", "高塔", "The Tower", "推翻重来与意外洞见", "Sudden rethink and clarity", "早看出裂缝却一直回避", "Ignoring a visible crack"},
	{17, "star", "XVII", "星星", "The Star", "远景、愿景与写给未来", "Distant horizon and hope", "停留在想象，还没落下第一句", "A vision waiting for its first line"},
	{18, "moon", "XVIII", "月亮", "The Moon", "漫游、记忆与幽微感受", "Memory, drift, and nuance", "思绪朦胧，需要一句直白的话", "Hazy thoughts needing plain words"},
	{19, "sun", "XIX", "太阳", "The Sun", "明朗、喜悦与清晰答案", "Clarity and warm certainty", "过于乐观，漏掉了边界条件", "Too sunny to spot edge cases"},
	{20, "judgement", "XX", "审判", "Judgement", "阶段总结与重新出发", "Awakening and synthesis", "总结写完却没改变下一步", "Reviewing without changing action"},
	{21, "world", "XXI", "世界", "The World", "闭环、圆满与想清楚再开口", "Completion and wholeness", "只差最后收尾却迟迟不落幕", "One final stroke left undone"},
}

// ArcanaByID safely returns the Major Arcana for id (wrapped into 0..21).
func ArcanaByID(id int) Arcana {
	idx := id % len(MajorArcana)
	if idx < 0 {
		idx += len(MajorArcana)
	}
	return MajorArcana[idx]
}

// ArcanaByKey looks up a Major Arcana by key, defaulting to The Magician.
func ArcanaByKey(key string) Arcana {
	for _, a := range MajorArcana {
		if a.Key == key {
			return a
		}
	}
	return MajorArcana[1]
}

// NoteCard binds a real notebook Markdown note to its Major Arcana identity.
type NoteCard struct {
	Path     string
	Title    string
	ArcanaID int
	ModUnix  int64
	Size     int64
	IsEmpty  bool
	IsDirty  bool
	IsActive bool
	When     string
}

func (n NoteCard) Arcana() Arcana {
	return ArcanaByID(n.ArcanaID)
}

// SpreadCard represents one of the three positions (Past / Present / Future) on the table.
type SpreadCard struct {
	Position   string // "过去" / "现在" / "未来"
	Note       NoteCard
	Reversed   bool
	ActionText string // "重读" / "续写" / "动笔"
	MetaText   string // e.g. "四个月前 · 正位" / "未保存 · 正位"
}

// Spread holds a 3-card Past/Present/Future spread and its interpretations.
type Spread struct {
	Cards    [3]SpreadCard
	HasCards [3]bool
	Lines    []string
	CTATitle string
	CTAPath  string
}

// DailyCard represents today's single invitation card.
type DailyCard struct {
	Date           string
	DateDisplay    string
	Note           NoteCard
	Reversed       bool
	Excerpt        string
	DoubtQuestions []string
	LastOpened     string
}

// AssignArcana maps a real note to a fitting Major Arcana (0..21) based on title,
// content clues, age, and size, falling back to a stable hash so every note keeps a
// consistent card identity.
func AssignArcana(notePath, title, content string, modUnix, size int64, now time.Time) int {
	lower := strings.ToLower(title + "\n" + content)
	trimmed := strings.TrimSpace(content)
	if size <= 24 || len(trimmed) < 12 {
		if containsAny(lower, "未来", "十年", "愿望", "梦想", "future", "dream", "letter", "信") {
			return 17 // 星星
		}
		return 0 // 愚者
	}
	switch {
	case containsAny(lower, "计划", "目标", "年度", "okr", "roadmap", "plan", "goal"):
		return 1 // 魔术师
	case containsAny(lower, "会议", "纪要", "评审", "笔记", "meeting", "review", "spec", "design"):
		return 2 // 女祭司
	case containsAny(lower, "草稿", "待补充", "todo", "wip", "draft", "周报", "weekly"):
		return 12 // 倒吊人
	case containsAny(lower, "清单", "checklist", "搬家", "打包", "list", "步骤"):
		return 10 // 命运之轮
	case containsAny(lower, "旅行", "游记", "札记", "散步", "京都", "梦", "travel", "trip", "diary", "日记"):
		return 18 // 月亮
	case containsAny(lower, "书架", "家", "阳光", "快乐", "美食", "home", "idea", "灵感"):
		return 19 // 太阳
	case containsAny(lower, "复盘", "总结", "回顾", "反思", "retro", "summary", "postmortem"):
		return 20 // 审判
	case containsAny(lower, "读书", "书评", "摘录", "book", "reading", "置身事内"):
		return 9 // 隐士
	case containsAny(lower, "选择", "决定", "对比", "权衡", "tradeoff", "vs", "决策"):
		return 6 // 恋人
	case containsAny(lower, "架构", "规范", "原则", "rule", "system", "框架"):
		return 4 // 皇帝
	case containsAny(lower, "教程", "指南", "方法", "guide", "how to", "学习"):
		return 5 // 教皇
	case containsAny(lower, "习惯", "锻炼", "游泳", "跑步", "坚持", "habit", "workout"):
		return 8 // 力量
	case containsAny(lower, "发布", "上线", "冲刺", "launch", "ship", "release"):
		return 7 // 战车
	case containsAny(lower, "重构", "推翻", "故障", "bug", "incident", "重构"):
		return 16 // 高塔
	case containsAny(lower, "清理", "归档", "放弃", "删除", "archive", "cleanup"):
		return 13 // 死神
	case containsAny(lower, "清楚", "完结", "完成", "里程碑", "milestone", "done", "开口"):
		return 21 // 世界
	}
	if modUnix > 0 && now.Sub(time.Unix(modUnix, 0)) > 45*24*time.Hour {
		return 9 // 隐士 (long untouched note)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(filepath.Base(notePath) + "|" + title))
	return int(h.Sum32()) % len(MajorArcana)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// BuildDeckNotes flattens workspace FileEntries into Markdown NoteCards in file-tree order.
func BuildDeckNotes(
	entries []model.FileEntry,
	activePath string,
	activeDirty bool,
	overrides map[string]int,
	readSnippet func(string) string,
	now time.Time,
) []NoteCard {
	cleanActive := ""
	if activePath != "" {
		cleanActive = filepath.Clean(activePath)
	}
	var out []NoteCard
	var walk func([]model.FileEntry)
	walk = func(list []model.FileEntry) {
		for _, e := range list {
			if e.IsDir {
				walk(e.Children)
				continue
			}
			if !e.IsMarkdown {
				continue
			}
			clean := filepath.Clean(e.Path)
			title := strings.TrimSpace(e.Name)
			if title == "" {
				base := filepath.Base(clean)
				title = strings.TrimSuffix(base, filepath.Ext(base))
			}
			isActive := cleanActive != "" && clean == cleanActive
			arcID, hasOverride := overrides[clean]
			if !hasOverride {
				snippet := ""
				if readSnippet != nil {
					snippet = readSnippet(clean)
				}
				arcID = AssignArcana(clean, title, snippet, e.ModUnix, e.Size, now)
			}
			isEmpty := e.Size <= 24
			when := FormatRelativeTime(e.ModUnix, isEmpty, isActive && activeDirty, now)
			out = append(out, NoteCard{
				Path:     clean,
				Title:    title,
				ArcanaID: arcID,
				ModUnix:  e.ModUnix,
				Size:     e.Size,
				IsEmpty:  isEmpty,
				IsDirty:  isActive && activeDirty,
				IsActive: isActive,
				When:     when,
			})
		}
	}
	walk(entries)
	return out
}

// FormatRelativeTime returns a compact human-readable age label for a note.
func FormatRelativeTime(modUnix int64, isEmpty, isDirty bool, now time.Time) string {
	if isDirty {
		return theme.Tr("未保存", "Unsaved")
	}
	if isEmpty {
		return theme.Tr("尚未动笔", "Unwritten")
	}
	if modUnix <= 0 {
		return theme.Tr("最近", "Recently")
	}
	d := now.Sub(time.Unix(modUnix, 0))
	if d < 0 {
		d = 0
	}
	days := int(d.Hours() / 24)
	switch {
	case d < time.Hour:
		return theme.Tr("刚刚", "Just now")
	case days == 0:
		return theme.Tr("今天", "Today")
	case days == 1:
		return theme.Tr("昨天", "Yesterday")
	case days < 7:
		return fmt.Sprintf(theme.Tr("%d 天前", "%d days ago"), days)
	case days < 30:
		weeks := max(days/7, 1)
		return fmt.Sprintf(theme.Tr("%d 周前", "%d weeks ago"), weeks)
	case days < 365:
		months := max(days/30, 1)
		return fmt.Sprintf(theme.Tr("%d 个月前", "%d months ago"), months)
	default:
		years := max(days/365, 1)
		return fmt.Sprintf(theme.Tr("%d 年前", "%d years ago"), years)
	}
}

// FormatChineseDate returns "十一月二十日" in Chinese or "November 20" in English.
func FormatChineseDate(t time.Time) string {
	if theme.IsEn() {
		return t.Format("January 2")
	}
	months := [...]string{"", "一月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "十一月", "十二月"}
	m := int(t.Month())
	if m < 1 || m > 12 {
		m = 1
	}
	return months[m] + chineseDay(t.Day())
}

func chineseDay(d int) string {
	digits := [...]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	switch {
	case d <= 0:
		return "一日"
	case d < 10:
		return digits[d] + "日"
	case d == 10:
		return "十日"
	case d < 20:
		return "十" + digits[d-10] + "日"
	case d == 20:
		return "二十日"
	case d < 30:
		return "二十" + digits[d-20] + "日"
	case d == 30:
		return "三十日"
	default:
		return "三十一日"
	}
}

// DrawThreeSpread builds a Past / Present / Future spread from real notebook notes,
// always pinning the currently active note to the Present (middle) slot when available.
func DrawThreeSpread(notes []NoteCard, activePath string, prevPaths [3]string, rng *rand.Rand) Spread {
	var sp Spread
	posLabels := [3]string{
		theme.Tr("过去", "Past"),
		theme.Tr("现在", "Present"),
		theme.Tr("未来", "Future"),
	}
	if len(notes) == 0 {
		for i := 0; i < 3; i++ {
			sp.Cards[i].Position = posLabels[i]
		}
		sp.Lines = []string{
			theme.Tr("笔记本里还没有牌。先新建一篇笔记写下几行字，牌桌就会亮起来。", "Your deck is empty. Create a note to light up the tarot table."),
		}
		return sp
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	cleanActive := ""
	if activePath != "" {
		cleanActive = filepath.Clean(activePath)
	}

	// 1. Present (slot 1): pin to the active note if present, else most recent note.
	presentIdx := 0
	if cleanActive != "" {
		for i, n := range notes {
			if n.Path == cleanActive {
				presentIdx = i
				break
			}
		}
	}
	present := notes[presentIdx]

	// Build remaining pool (excluding Present when there are multiple notes).
	var others []NoteCard
	for i, n := range notes {
		if i == presentIdx && len(notes) > 1 {
			continue
		}
		others = append(others, n)
	}
	if len(others) == 0 {
		others = append(others, present)
	}

	// 2. Past (slot 0): prefer older non-empty notes, rotating away from prevPaths[0].
	pastPool := make([]NoteCard, len(others))
	copy(pastPool, others)
	sort.SliceStable(pastPool, func(i, j int) bool {
		if pastPool[i].IsEmpty != pastPool[j].IsEmpty {
			return !pastPool[i].IsEmpty
		}
		return pastPool[i].ModUnix < pastPool[j].ModUnix
	})
	past := pickPreferringFresh(pastPool, prevPaths[0], "", rng)

	// 3. Future (slot 2): prefer empty/short notes ("尚未动笔") or different note from Past.
	futurePool := make([]NoteCard, len(others))
	copy(futurePool, others)
	sort.SliceStable(futurePool, func(i, j int) bool {
		if futurePool[i].IsEmpty != futurePool[j].IsEmpty {
			return futurePool[i].IsEmpty
		}
		return futurePool[i].Size < futurePool[j].Size
	})
	future := pickPreferringFresh(futurePool, prevPaths[2], past.Path, rng)

	chosen := [3]NoteCard{past, present, future}
	for i := 0; i < 3; i++ {
		n := chosen[i]
		rev := false
		if i != 1 && rng.Intn(4) == 0 {
			rev = true
		}
		act := theme.Tr("重读", "Reread")
		if i == 1 {
			act = theme.Tr("续写", "Continue")
		} else if n.IsEmpty {
			act = theme.Tr("动笔", "Begin")
		}
		orient := theme.Tr("正位", "Upright")
		if rev {
			orient = theme.Tr("逆位", "Reversed")
		}
		sp.Cards[i] = SpreadCard{
			Position:   posLabels[i],
			Note:       n,
			Reversed:   rev,
			ActionText: act,
			MetaText:   n.When + " · " + orient,
		}
		sp.HasCards[i] = true
	}

	sp.CTATitle = present.Title
	sp.CTAPath = present.Path
	sp.Lines = BuildSpreadReadings(sp)
	return sp
}

func pickPreferringFresh(pool []NoteCard, prevPath, excludePath string, rng *rand.Rand) NoteCard {
	limit := min(len(pool), 6)
	var fresh []NoteCard
	for i := 0; i < limit; i++ {
		if pool[i].Path != prevPath && pool[i].Path != excludePath {
			fresh = append(fresh, pool[i])
		}
	}
	if len(fresh) > 0 {
		return fresh[rng.Intn(len(fresh))]
	}
	var fallback []NoteCard
	for _, n := range pool {
		if n.Path != excludePath {
			fallback = append(fallback, n)
		}
	}
	if len(fallback) > 0 {
		return fallback[rng.Intn(len(fallback))]
	}
	return pool[0]
}

// BuildSpreadReadings generates instant poetic Chinese/English interpretations tying
// the three real notes together (two distinct angles for "换一种读法").
func BuildSpreadReadings(sp Spread) []string {
	p := sp.Cards[0]
	c := sp.Cards[1]
	f := sp.Cards[2]
	pArc := p.Note.Arcana()
	cArc := c.Note.Arcana()
	fArc := f.Note.Arcana()

	if theme.IsEn() {
		l1 := fmt.Sprintf(
			"What you wrote in \"%s\" (%s) still echoes here: borrow its restraint to sharpen \"%s\", then carry one clear thread forward into \"%s\".",
			p.Note.Title, pArc.Name(), c.Note.Title, f.Note.Title,
		)
		l2 := fmt.Sprintf(
			"%s looks back while %s holds the pen today — finish one concrete section of \"%s\" before opening \"%s\" (%s).",
			pArc.Name(), cArc.Name(), c.Note.Title, f.Note.Title, fArc.Name(),
		)
		return []string{l1, l2}
	}

	pOrient := "正位"
	if p.Reversed {
		pOrient = "逆位"
	}
	l1 := fmt.Sprintf(
		"%s你在《%s》里留下的线索，正好能替《%s》删繁就简；把当下这一节落稳，剩下的留给《%s》。",
		p.Note.When, p.Note.Title, c.Note.Title, f.Note.Title,
	)
	l2 := fmt.Sprintf(
		"%s%s提醒你先回头看一眼：《%s》与《%s》其实牵着同一根线，写完手头这一段，再去翻开《%s》（%s）。",
		pArc.NameZh, pOrient, p.Note.Title, c.Note.Title, f.Note.Title, fArc.NameZh,
	)
	l3 := fmt.Sprintf(
		"此刻执笔的是%s——不必一次写满，先在《%s》里写清最关键的一条判断，再让《%s》接住你的下一步。",
		cArc.NameZh, c.Note.Title, f.Note.Title,
	)
	return []string{l1, l2, l3}
}

// PickDailyCard deterministically picks today's note card (or restores the saved daily card).
func PickDailyCard(notes []NoteCard, dateStr string, saved DailyRecord, readText func(string) string, now time.Time) DailyCard {
	dc := DailyCard{
		Date:        dateStr,
		DateDisplay: FormatChineseDate(now),
	}
	if len(notes) == 0 {
		dc.Note = NoteCard{
			Title:    theme.Tr("未命名笔记本", "Empty Notebook"),
			ArcanaID: 0,
			IsEmpty:  true,
			When:     theme.Tr("尚未动笔", "Unwritten"),
		}
		dc.Excerpt = theme.Tr("「写下今天的第一行字，牌面便会随之显现。」", "Write your first line today and the card will take shape.")
		dc.DoubtQuestions = BuildDoubtQuestions(dc.Note, "")
		dc.LastOpened = theme.Tr("尚未打开", "Not opened yet")
		return dc
	}

	var chosen NoteCard
	foundSaved := false
	if saved.Date == dateStr && saved.NotePath != "" {
		cleanSaved := filepath.Clean(saved.NotePath)
		for _, n := range notes {
			if n.Path == cleanSaved {
				chosen = n
				if saved.ArcanaID >= 0 && saved.ArcanaID < len(MajorArcana) {
					chosen.ArcanaID = saved.ArcanaID
				}
				dc.Reversed = saved.Reversed
				foundSaved = true
				break
			}
		}
	}
	if !foundSaved {
		// Prefer older non-empty notes for meaningful daily rediscovery.
		var pool []NoteCard
		for _, n := range notes {
			if !n.IsEmpty {
				pool = append(pool, n)
			}
		}
		if len(pool) == 0 {
			pool = notes
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte("block-tarot-daily:" + dateStr))
		for _, n := range pool {
			_, _ = h.Write([]byte("|" + filepath.Base(n.Path)))
		}
		seed := h.Sum64()
		chosen = pool[int(seed%uint64(len(pool)))]
		dc.Reversed = ((seed >> 16) % 3) == 0
	}

	dc.Note = chosen
	content := ""
	if readText != nil && chosen.Path != "" {
		content = readText(chosen.Path)
	}
	dc.Excerpt = ExtractExcerpt(content, chosen.Title)
	if len(saved.DoubtQuestions) >= 3 && saved.Date == dateStr && filepath.Clean(saved.NotePath) == chosen.Path {
		dc.DoubtQuestions = append([]string(nil), saved.DoubtQuestions[:3]...)
	} else {
		dc.DoubtQuestions = BuildDoubtQuestions(chosen, content)
	}
	dc.LastOpened = theme.Tr("上次修改 · ", "Last edited · ") + chosen.When
	return dc
}

// ExtractExcerpt pulls a clean representative sentence or quote from note Markdown.
func ExtractExcerpt(md, fallbackTitle string) string {
	lines := strings.Split(md, "\n")
	var firstQuote, firstPara string
	inFence := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "![") {
			continue
		}
		if strings.HasPrefix(line, ">") {
			q := strings.TrimSpace(strings.TrimLeft(line, "> "))
			if len([]rune(q)) >= 6 && firstQuote == "" {
				firstQuote = q
				break
			}
			continue
		}
		cleaned := strings.TrimLeft(line, "-*+0123456789. ")
		cleaned = strings.ReplaceAll(cleaned, "**", "")
		cleaned = strings.ReplaceAll(cleaned, "`", "")
		cleaned = strings.TrimSpace(cleaned)
		if len([]rune(cleaned)) >= 8 && firstPara == "" {
			firstPara = cleaned
		}
	}
	pick := firstQuote
	if pick == "" {
		pick = firstPara
	}
	if pick == "" {
		if theme.IsEn() {
			return fmt.Sprintf("\"%s\" is still waiting for its next paragraph.", fallbackTitle)
		}
		return fmt.Sprintf("「《%s》还在等你写下真正想留住的那句话。」", fallbackTitle)
	}
	runes := []rune(pick)
	if len(runes) > 64 {
		pick = string(runes[:63]) + "…"
	}
	if theme.IsEn() {
		return "“" + pick + "”"
	}
	if !strings.HasPrefix(pick, "「") {
		pick = "「" + pick + "」"
	}
	return pick
}

// BuildDoubtQuestions creates 3 sharp Devil's Advocate reflection questions for "质疑它".
func BuildDoubtQuestions(note NoteCard, content string) []string {
	excerpt := strings.Trim(ExtractExcerpt(content, note.Title), "「」“”")
	shortEx := excerpt
	if r := []rune(shortEx); len(r) > 22 {
		shortEx = string(r[:21]) + "…"
	}
	if theme.IsEn() {
		return []string{
			fmt.Sprintf("Does \"%s\" still hold up under your real constraints today, or can you name a clear counterexample?", shortEx),
			fmt.Sprintf("In \"%s\", which claim did you borrow from others instead of testing yourself?", note.Title),
			fmt.Sprintf("Since %s, what concrete decision did this note change — and if none, what needs to be rewritten or cut?", note.When),
		}
	}
	return []string{
		fmt.Sprintf("「%s」放到你现在的处境里还成立吗？你能举出一个反例吗？", shortEx),
		fmt.Sprintf("回头看《%s》，里面有哪些判断只是顺手复述，真正属于你自己的不同意见在哪里？", note.Title),
		fmt.Sprintf("距离上次写下这篇已经过去了一段时间，它改变了你哪一个具体决定？如果说不出来，这一篇该删掉还是重写？"),
	}
}
