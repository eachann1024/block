package mindmap

import (
	"archive/zip"
	"bytes"
	"image/png"
	"strings"
	"testing"

	"block/internal/theme"
)

func buildTestTree() *Node {
	root := &Node{
		Text: "中心主题：架构设计",
		Children: []*Node{
			{
				Text:   "核心模块",
				Task:   BoolPtr(true),
				Link:   "https://example.com/core",
				Folded: true,
				Children: []*Node{
					{Text: "渲染引擎", Task: BoolPtr(false)},
					{Text: "数据同步", Link: "https://example.com/sync"},
				},
			},
			{
				Text: "插件生态",
				Children: []*Node{
					{Text: "官方插件"},
				},
			},
		},
	}
	root.RepairParents(nil)
	root.AssignIDs(0)
	return root
}

func assertTreesEqual(t *testing.T, want, got *Node, checkFolded bool) {
	t.Helper()
	if want == nil || got == nil {
		t.Fatalf("nil node: want=%v got=%v", want, got)
	}
	if want.Text != got.Text {
		t.Errorf("text mismatch: want %q, got %q", want.Text, got.Text)
	}
	if want.Link != got.Link {
		t.Errorf("node %q link mismatch: want %q, got %q", want.Text, want.Link, got.Link)
	}
	switch {
	case want.Task == nil && got.Task != nil:
		t.Errorf("node %q task mismatch: want nil, got %v", want.Text, *got.Task)
	case want.Task != nil && got.Task == nil:
		t.Errorf("node %q task mismatch: want %v, got nil", want.Text, *want.Task)
	case want.Task != nil && got.Task != nil && *want.Task != *got.Task:
		t.Errorf("node %q task mismatch: want %v, got %v", want.Text, *want.Task, *got.Task)
	}
	if checkFolded && want.Folded != got.Folded {
		t.Errorf("node %q folded mismatch: want %v, got %v", want.Text, want.Folded, got.Folded)
	}
	if len(want.Children) != len(got.Children) {
		t.Fatalf("node %q children count mismatch: want %d, got %d", want.Text, len(want.Children), len(got.Children))
	}
	for i := range want.Children {
		if got.Children[i].Parent != got {
			t.Errorf("child %q parent pointer not repaired", got.Children[i].Text)
		}
		assertTreesEqual(t, want.Children[i], got.Children[i], checkFolded)
	}
}

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		file string
		want Format
	}{
		{"map.md", FormatMarkdown},
		{"MAP.MARKDOWN", FormatMarkdown},
		{"outline.opml", FormatOPML},
		{"mind.mm", FormatFreeMind},
		{"deck.xmind", FormatXMind},
		{"shot.png", FormatPNG},
		{"doc.pdf", FormatPDF},
		{"unknown.txt", ""},
	}
	for _, tc := range cases {
		if got := DetectFormat(tc.file); got != tc.want {
			t.Errorf("DetectFormat(%q) = %q, want %q", tc.file, got, tc.want)
		}
	}
}

func TestFormatRoundTrips(t *testing.T) {
	pal := theme.Ocean()
	want := buildTestTree()

	cases := []struct {
		name        string
		format      Format
		filename    string
		checkFolded bool
	}{
		{"Markdown", FormatMarkdown, "test.md", false},
		{"OPML", FormatOPML, "test.opml", true},
		{"FreeMind", FormatFreeMind, "test.mm", true},
		{"XMind", FormatXMind, "test.xmind", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := ExportBytes(want, LayoutBoth, tc.format, pal)
			if err != nil {
				t.Fatalf("ExportBytes(%s) failed: %v", tc.format, err)
			}
			got, report, err := ImportBytes(tc.filename, data)
			if err != nil {
				t.Fatalf("ImportBytes(%s) failed: %v", tc.filename, err)
			}
			if report.Format != string(tc.format) {
				t.Errorf("report.Format = %q, want %q", report.Format, tc.format)
			}
			if report.Nodes != 6 || report.Tasks != 2 || report.Links != 2 {
				t.Errorf("unexpected stats: %+v", report)
			}
			if report.HasDropped() {
				t.Errorf("unexpected dropped items on clean roundtrip: %+v", report)
			}
			assertTreesEqual(t, want, got, tc.checkFolded)
		})
	}
}

func TestXMindImportReportDroppedFeatures(t *testing.T) {
	contentJSON := `[{
		"id": "sheet-1",
		"title": "Sheet 1",
		"relationships": [{"id": "rel-1"}, {"id": "rel-2"}],
		"rootTopic": {
			"id": "root",
			"title": "主图",
			"notes": {"plain": {"content": "根节点备注"}},
			"boundaries": [{"id": "b-1"}],
			"summaries": [{"id": "s-1"}],
			"children": {
				"attached": [
					{
						"id": "c-1",
						"title": "子节点",
						"image": {"src": "xap:resources/img.png"},
						"notes": {"plain": {"content": "子节点备注"}}
					}
				],
				"summary": [{"id": "sum-1", "title": "汇总"}],
				"callout": [{"id": "call-1", "title": "标注"}],
				"detached": [{"id": "det-1", "title": "自由主题"}]
			}
		}
	}]`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("content.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(contentJSON)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	root, report, err := ImportBytes("complex.xmind", buf.Bytes())
	if err != nil {
		t.Fatalf("ImportBytes failed: %v", err)
	}
	if root.Text != "主图" || len(root.Children) != 1 || root.Children[0].Text != "子节点" {
		t.Fatalf("unexpected tree: %+v", root)
	}
	if report.Relationships != 2 {
		t.Errorf("Relationships = %d, want 2", report.Relationships)
	}
	if report.Summaries != 1 {
		t.Errorf("Summaries = %d, want 1", report.Summaries)
	}
	if report.Boundaries != 1 {
		t.Errorf("Boundaries = %d, want 1", report.Boundaries)
	}
	if report.Notes != 2 {
		t.Errorf("Notes = %d, want 2", report.Notes)
	}
	if report.Callouts != 2 {
		t.Errorf("Callouts = %d, want 2", report.Callouts)
	}
	if report.Images != 1 {
		t.Errorf("Images = %d, want 1", report.Images)
	}
	if !report.HasDropped() {
		t.Error("expected HasDropped() to be true")
	}
}

func TestXMind8LegacyXMLImport(t *testing.T) {
	contentXML := `<?xml version="1.0" encoding="UTF-8"?>
<xmap-content>
  <sheet id="s1">
    <topic id="t1">
      <title>旧版主题</title>
      <children>
        <topics type="attached">
          <topic id="t2" branch="folded" xlink:href="https://example.org">
            <title>分支A</title>
            <marker-refs><marker-ref marker-id="task-done"/></marker-refs>
          </topic>
        </topics>
      </children>
    </topic>
    <relationships><relationship id="r1"/></relationships>
  </sheet>
</xmap-content>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("content.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(contentXML)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	root, report, err := ImportBytes("legacy.xmind", buf.Bytes())
	if err != nil {
		t.Fatalf("ImportBytes failed: %v", err)
	}
	if root.Text != "旧版主题" || len(root.Children) != 1 {
		t.Fatalf("unexpected root: %+v", root)
	}
	ch := root.Children[0]
	if ch.Text != "分支A" || !ch.Folded || ch.Task == nil || !*ch.Task {
		t.Errorf("unexpected child: %+v", ch)
	}
	if report.Relationships != 1 {
		t.Errorf("Relationships = %d, want 1", report.Relationships)
	}
}

func TestOPMLMubuAndFreeMindDroppedFeatures(t *testing.T) {
	mubu := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>幕布测试</title></head>
  <body>
    <outline text="" _mubu_text="%3Cspan%3E%E6%A0%B9%E8%8A%82%E7%82%B9%3C%2Fspan%3E" _note="备注内容">
      <outline text="任务项" _status="checked" _link="https://mubu.com"/>
    </outline>
  </body>
</opml>`
	root, report, err := ImportBytes("mubu.opml", []byte(mubu))
	if err != nil {
		t.Fatalf("ImportBytes(opml) failed: %v", err)
	}
	if root.Text != "根节点" || report.Notes != 1 || report.Tasks != 1 || report.Links != 1 {
		t.Errorf("unexpected mubu result: root=%+v report=%+v", root, report)
	}

	mm := `<?xml version="1.0" encoding="UTF-8"?>
<map version="1.0.1">
  <node TEXT="根">
    <cloud/>
    <arrowlink DESTINATION="x"/>
    <richcontent TYPE="NOTE"><html><body><p>笔记</p></body></html></richcontent>
    <node><richcontent TYPE="NODE"><html><body><p>富文本子项</p></body></html></richcontent></node>
  </node>
</map>`
	mmRoot, mmReport, err := ImportBytes("note.mm", []byte(mm))
	if err != nil {
		t.Fatalf("ImportBytes(mm) failed: %v", err)
	}
	if mmRoot.Children[0].Text != "富文本子项" {
		t.Errorf("richcontent NODE text = %q", mmRoot.Children[0].Text)
	}
	if mmReport.Boundaries != 1 || mmReport.Relationships != 1 || mmReport.Notes != 1 {
		t.Errorf("unexpected mm report: %+v", mmReport)
	}
}

func TestExportPNGAndPDF(t *testing.T) {
	pal := theme.Ocean()
	root := buildTestTree()

	pngBytes, err := ExportBytes(root, LayoutRight, FormatPNG, pal)
	if err != nil {
		t.Fatalf("ExportPNG failed: %v", err)
	}
	if len(pngBytes) < 8 || string(pngBytes[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("invalid PNG header")
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode failed: %v", err)
	}
	if img.Bounds().Empty() {
		t.Fatalf("decoded PNG has empty bounds")
	}

	pdfBytes, err := ExportBytes(root, LayoutBoth, FormatPDF, pal)
	if err != nil {
		t.Fatalf("ExportPDF failed: %v", err)
	}
	pdfStr := string(pdfBytes)
	if !strings.HasPrefix(pdfStr, "%PDF-1.4") {
		t.Errorf("PDF missing %%PDF-1.4 header")
	}
	if !strings.HasSuffix(strings.TrimSpace(pdfStr), "%%EOF") {
		t.Errorf("PDF missing %%EOF trailer")
	}
}

func TestAdversarialImportEdgeCases(t *testing.T) {
	// 1. Markdown file wrapping a ```mindmap fenced block should unwrap cleanly.
	fenced := "说明段落\n\n```mindmap\n# 中心\n- 分支 A\n```\n"
	root, _, err := ImportBytes("note.md", []byte(fenced))
	if err != nil {
		t.Fatalf("fenced md import failed: %v", err)
	}
	if root.Text != "中心" || len(root.Children) != 1 || root.Children[0].Text != "分支 A" {
		t.Fatalf("unexpected fenced md tree: %+v", root)
	}

	// 2. Root task round-trip (e.g. OPML single root with _complete="true").
	opmlRootTask := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><head><title>T</title></head><body><outline text="主任务" _complete="true"><outline text="子项"/></outline></body></opml>`
	rtRoot, _, err := ImportBytes("root-task.opml", []byte(opmlRootTask))
	if err != nil {
		t.Fatalf("opml root task import failed: %v", err)
	}
	if rtRoot.Task == nil || !*rtRoot.Task {
		t.Fatalf("expected root task done, got %v", rtRoot.Task)
	}
	md := SerializeMarkdown(rtRoot)
	reparsed := ParseMarkdown(md)
	if reparsed.Task == nil || !*reparsed.Task || reparsed.Text != "主任务" {
		t.Fatalf("root task did not round-trip via markdown: %q -> %+v", md, reparsed)
	}

	// 3. XMind content.json containing null must fail instead of overwriting with default root.
	var nullBuf bytes.Buffer
	zw := zip.NewWriter(&nullBuf)
	w, _ := zw.Create("content.json")
	_, _ = w.Write([]byte("null"))
	_ = zw.Close()
	if _, _, err := ImportBytes("null.xmind", nullBuf.Bytes()); err == nil {
		t.Errorf("expected error for null XMind content.json")
	}

	// 4. Multi-sheet XMind must count ExtraSheets and relationships from skipped sheets.
	multiJSON := `[
		{"id":"s1","rootTopic":{"id":"r1","title":"画布一","children":{"attached":[{"id":"c1","title":"A"}]}}},
		{"id":"s2","rootTopic":{"id":"r2","title":"画布二"},"relationships":[{"id":"rel1"},{"id":"rel2"}]}
	]`
	var multiBuf bytes.Buffer
	zw2 := zip.NewWriter(&multiBuf)
	w2, _ := zw2.Create("content.json")
	_, _ = w2.Write([]byte(multiJSON))
	_ = zw2.Close()
	mRoot, mRep, err := ImportBytes("multi.xmind", multiBuf.Bytes())
	if err != nil {
		t.Fatalf("multi-sheet xmind import failed: %v", err)
	}
	if mRoot.Text != "画布一" || mRep.ExtraSheets != 1 || mRep.Relationships != 2 || !mRep.HasDropped() {
		t.Errorf("unexpected multi-sheet result: root=%+v report=%+v", mRoot, mRep)
	}
}
