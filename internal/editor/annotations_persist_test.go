package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"block/internal/model"
	"block/internal/theme"
	appUi "block/internal/ui"
	"github.com/egoist/mygo/ui"
)

func TestAnnotationsRustJSONCompatibilityAndPersistence(t *testing.T) {
	appUi.ResetSmoke()
	dataDir := t.TempDir()
	notePath := filepath.Join(t.TempDir(), "rust-note.md")

	// Fixture matching thinkly-fast's exact annotations.json schema (UTF-8 byte offsets).
	// "你好世界Hello" -> "你好"(0..6), "世界"(6..12), "Hello"(12..17).
	rustJSON := map[string]any{
		"notes": map[string]any{
			notePath: []map[string]any{
				{
					"id":          1731880000101,
					"block_index": 1,
					"start":       6,
					"end":         12,
					"quote":       "世界",
					"body":        "",
					"reactions":   []string{"✅"},
					"applied":     false,
				},
				{
					"id":          1731880000102,
					"block_index": 1,
					"start":       12,
					"end":         17,
					"quote":       "Hello",
					"body":        "Clarify greeting",
					"reactions":   []string{},
					"applied":     false,
				},
				{
					"id":          1731880000103,
					"block_index": 1,
					"start":       0,
					"end":         6,
					"quote":       "你好",
					"body":        "Old applied note",
					"reactions":   []string{},
					"applied":     true,
				},
			},
		},
	}
	rawBytes, err := json.MarshalIndent(rustJSON, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "annotations.json"), rawBytes, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "Title"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "你好世界Hello"}}},
		},
	}
	s := NewSession(notePath, doc, nil, nil)
	s.SetDataDir(dataDir)
	s.FocusBlock(0) // Leave block 1 unfocused so RichText renders highlights

	anns := s.Annotations()
	if len(anns) != 3 {
		t.Fatalf("expected 3 loaded annotations, got %d", len(anns))
	}
	// Verify byte->rune offset mapping for CJK ("世界" is rune [2,4))
	if anns[0].StartOffset != 2 || anns[0].EndOffset != 4 {
		t.Fatalf("expected rune range [2,4) for 世界, got [%d,%d)", anns[0].StartOffset, anns[0].EndOffset)
	}

	// Verify span splitting highlights only the annotated sub-runs with AnnotationBG.
	pal := theme.Ocean()
	display := s.buildDisplaySpans(1, &doc.Blocks[1], 16, 400)
	if len(display) != 3 {
		t.Fatalf("expected 3 split spans (你好, 世界, Hello), got %d: %+v", len(display), display)
	}
	if display[0].Background.A != 0 {
		t.Fatalf("expected applied annotation without reactions (你好) to have no highlight, got %+v", display[0].Background)
	}
	if display[1].Background != pal.AnnotationBG || display[2].Background != pal.AnnotationBG {
		t.Fatalf("expected AnnotationBG on 世界 and Hello, got %+v and %+v", display[1].Background, display[2].Background)
	}

	// Render wide rail: reaction-only pill renders without card; applied without reactions is hidden.
	tt := ui.NewTester(makeSessionView(s, false), 1200, 700)
	tt.Frame()
	if _, ok := appUi.TargetBounds("reaction-pill-1731880000101-0"); !ok {
		t.Fatalf("expected reaction-pill for reaction-only annotation")
	}
	if _, ok := appUi.TargetBounds("annotation-card-1731880000101"); ok {
		t.Fatalf("reaction-only annotation must not render full card")
	}
	if _, ok := appUi.TargetBounds("annotation-card-1731880000102"); !ok {
		t.Fatalf("expected annotation-card for body annotation")
	}
	if _, ok := appUi.TargetBounds("annotation-card-1731880000103"); ok {
		t.Fatalf("applied annotation without reactions must be hidden")
	}

	// Add a new annotation and verify persisted JSON matches Rust schema (numeric u64 id, byte offsets).
	s.SelectRange(1, 0, 1, 2) // "你好"
	created := s.AddAnnotation("New note")
	if created == nil {
		t.Fatalf("AddAnnotation failed")
	}
	savedRaw, err := os.ReadFile(filepath.Join(dataDir, "annotations.json"))
	if err != nil {
		t.Fatalf("read saved annotations.json: %v", err)
	}
	var parsed struct {
		Notes map[string][]struct {
			ID         uint64   `json:"id"`
			BlockIndex int      `json:"block_index"`
			Start      int      `json:"start"`
			End        int      `json:"end"`
			Quote      string   `json:"quote"`
			Body       string   `json:"body"`
			Reactions  []string `json:"reactions"`
			Applied    *bool    `json:"applied"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(savedRaw, &parsed); err != nil {
		t.Fatalf("saved annotations.json not compatible with Rust u64 schema: %v\nraw:\n%s", err, string(savedRaw))
	}
	list := parsed.Notes[notePath]
	if len(list) != 4 {
		t.Fatalf("expected 4 saved annotations, got %d", len(list))
	}
	last := list[3]
	if last.ID == 0 || last.BlockIndex != 1 || last.Start != 0 || last.End != 6 || last.Quote != "你好" || last.Body != "New note" {
		t.Fatalf("unexpected persisted Rust-compatible entry: %+v", last)
	}

	// Reload into fresh Session and verify persistence survives.
	s2 := NewSession(notePath, doc.Clone(), nil, nil)
	s2.SetDataDir(dataDir)
	if got := len(s2.Annotations()); got != 4 {
		t.Fatalf("expected 4 annotations on reload, got %d", got)
	}

	// Verify narrow mode renders inline foot chip instead of right rail.
	appUi.ResetSmoke()
	tt.SetSize(820, 700)
	if _, ok := appUi.TargetBounds("annotation-rail"); ok {
		t.Fatalf("expected annotation-rail hidden in narrow 820px viewport")
	}
	if !tt.HasText("Clarify greeting") {
		t.Fatalf("expected narrow inline annotation capsule text")
	}
}

func TestFindBarParityLabelsAndMatchColors(t *testing.T) {
	appUi.ResetSmoke()
	doc := &model.Doc{
		Blocks: []model.Block{
			{ID: 1, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "Header"}}},
			{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "foo bar foo"}}},
		},
	}
	s := NewSession("", doc, nil, nil)
	s.FocusBlock(0)
	tt := ui.NewTester(makeSessionView(s, false), 900, 600)

	// Empty query -> no "0/0" and no "无结果"/"No results"
	s.OpenFind()
	tt.Frame()
	if tt.HasText("0/0") || tt.HasText("无结果") || tt.HasText("No results") {
		t.Fatalf("expected empty count label for empty find query")
	}

	// Non-empty query with 0 matches -> "无结果" / "No results"
	s.SetFindQuery("zzzzz")
	tt.Frame()
	if !tt.HasText(theme.Tr("无结果", "No results")) {
		t.Fatalf("expected No results label for 0 matches")
	}

	// 2 matches -> "1 / 2" and split spans use FindActive / FindMatch
	s.SetFindQuery("foo")
	s.FocusBlock(0) // keep block 1 unfocused so RichText spans are evaluated
	tt.Frame()
	if !tt.HasText("1 / 2") {
		t.Fatalf("expected '1 / 2' match label")
	}
	pal := theme.Ocean()
	spans := s.buildDisplaySpans(1, &doc.Blocks[1], 16, 400)
	if len(spans) != 3 {
		t.Fatalf("expected 3 split spans for 'foo bar foo', got %d", len(spans))
	}
	if spans[0].Background != pal.FindActive || spans[2].Background != pal.FindMatch {
		t.Fatalf("expected FindActive on first match and FindMatch on second, got %+v / %+v", spans[0].Background, spans[2].Background)
	}
	_ = strings.TrimSpace("")
}

func TestUnresolvedAnnotationsSurviveSave(t *testing.T) {
	appUi.ResetSmoke()
	dataDir := t.TempDir()
	notePath := filepath.Join(t.TempDir(), "n.md")
	fixture := `{"notes":{"` + notePath + `":[` +
		`{"id":11,"block_index":1,"start":0,"end":5,"quote":"Hello","body":"","reactions":["👍"]},` +
		`{"id":12,"block_index":7,"start":0,"end":4,"quote":"gone","body":"keep me","reactions":[]}` +
		`]}}`
	if err := os.WriteFile(filepath.Join(dataDir, "annotations.json"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	doc := &model.Doc{Blocks: []model.Block{
		{ID: 1, Kind: model.BlockHeading1, Spans: []model.Span{{Text: "Title"}}},
		{ID: 2, Kind: model.BlockParagraph, Spans: []model.Span{{Text: "Hello world"}}},
	}}
	s := NewSession(notePath, doc, nil, nil)
	s.SetDataDir(dataDir)
	s.ensureAnnotationsLoaded()
	s.persistAnnotations()
	b, _ := os.ReadFile(filepath.Join(dataDir, "annotations.json"))
	if !strings.Contains(string(b), "keep me") {
		t.Fatalf("unresolved annotation dropped on save: %s", b)
	}
}
