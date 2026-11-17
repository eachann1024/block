package model

// MarkdownCodec decouples the Block Editor and AI subsystems from the concrete
// parser/serializer package so subsystems can compile and test independently.
type MarkdownCodec interface {
	Parse(src string) *Doc
	Serialize(doc *Doc) string
	NormalizePaste(raw string) string
}

// WorkspaceHost exposes notebook, note, search, history, and diff-plan hooks
// to the Editor, AI Panel, and Modals without creating circular imports.
type WorkspaceHost interface {
	ActiveNotebook() string
	Notebooks() []string
	ActiveNotePath() string
	ListNotebookFiles(root string) ([]FileEntry, error)
	SearchWorkspace(query string, allNotebooks bool) ([]SearchHit, error)
	ReadNote(absPath string) (string, error)
	WriteNote(absPath string, content string) error
	OpenNote(absPath string, lineHint int) error
	PreviewPlanInEditor(plan *BatchPlan) error
	ApplyBatchPlan(plan *BatchPlan) error
	RejectBatchPlan(planID string) error
	UndoLastPlan(plan *BatchPlan) error
	OpenSettings(tab string)
	RequestRedraw()
}
