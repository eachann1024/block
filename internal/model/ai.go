package model

import "encoding/json"

type BackendKind string

const (
	BackendBuiltin BackendKind = "builtin"
	BackendCodex   BackendKind = "codex"
	BackendClaude  BackendKind = "claude"
	BackendDsh     BackendKind = "dsh"
	BackendPi      BackendKind = "pi"
	BackendAgy     BackendKind = "agy"
)

var AllBackends = []BackendKind{
	BackendBuiltin,
	BackendCodex,
	BackendClaude,
	BackendDsh,
	BackendPi,
	BackendAgy,
}

func (b BackendKind) DisplayName() string {
	switch b {
	case BackendBuiltin:
		return "Built-in AI"
	case BackendCodex:
		return "Codex"
	case BackendClaude:
		return "Claude"
	case BackendDsh:
		return "dsh"
	case BackendPi:
		return "Pi"
	case BackendAgy:
		return "agy"
	default:
		return string(b)
	}
}

func (b BackendKind) AssetPath() string {
	switch b {
	case BackendCodex, BackendClaude, BackendDsh, BackendPi, BackendAgy:
		return "agents/" + string(b) + ".svg"
	default:
		return "agents/builtin.svg"
	}
}

type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

type ProviderProfile struct {
	Name     string   `json:"name,omitempty"`
	BaseURL  string   `json:"base_url,omitempty"`
	APIKey   string   `json:"api_key,omitempty"`
	Model    string   `json:"model,omitempty"`
	Protocol Protocol `json:"protocol,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
	Custom   bool     `json:"custom,omitempty"`
}

// AIProviderConfig matches ~/Library/Application Support/Block/ai.json.
type AIProviderConfig struct {
	Enabled       bool                       `json:"enabled"`
	Provider      string                     `json:"provider"`
	Protocol      Protocol                   `json:"protocol"`
	BaseURL       string                     `json:"base_url"`
	APIKey        string                     `json:"api_key,omitempty"`
	Model         string                     `json:"model"`
	ProviderOrder []string                   `json:"provider_order,omitempty"`
	Providers     map[string]ProviderProfile `json:"providers,omitempty"`
}

// AIBackendAdapterOptions stores per-CLI-adapter preferences in ai-backends.json.
type AIBackendAdapterOptions struct {
	Added         bool   `json:"added,omitempty"`
	Name          string `json:"name,omitempty"`
	BinaryPath    string `json:"binaryPath,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"` // "low", "medium", "high"
	NativeTools   *bool  `json:"nativeTools,omitempty"`   // nil uses the default of full local tools.
	Locked        bool   `json:"locked,omitempty"`
}

func (opt AIBackendAdapterOptions) AllowsNativeTools() bool {
	return opt.NativeTools == nil || *opt.NativeTools
}

// AIBackendsConfig matches ~/Library/Application Support/Block/ai-backends.json.
type AIBackendsConfig struct {
	Selected BackendKind                        `json:"selected"`
	Priority []BackendKind                      `json:"priority,omitempty"`
	Adapters map[string]AIBackendAdapterOptions `json:"adapters,omitempty"`
}

type PlanOpKind string

const (
	PlanOpReplace    PlanOpKind = "replace"
	PlanOpCreateNote PlanOpKind = "createNote"
)

type PlanOp struct {
	Kind         PlanOpKind `json:"kind,omitempty"`
	Page         string     `json:"page"`
	Old          string     `json:"old"`
	New          string     `json:"new"`
	LineSelected []bool     `json:"-"` // Per-changed-hunk/row selection in editor diff review
}

type BatchPlan struct {
	ID       string            `json:"id,omitempty"`
	Summary  string            `json:"summary,omitempty"`
	Root     string            `json:"root"`
	Ops      []PlanOp          `json:"ops"`
	Expected map[string]string `json:"expected,omitempty"` // Full snapshot before modification
	Status   string            `json:"status,omitempty"`   // "pending", "applied", "rejected"
}

type ToolKind string

const (
	ToolKindThink ToolKind = "think"
	ToolKindRead  ToolKind = "read"
	ToolKindWrite ToolKind = "write"
	ToolKindRun   ToolKind = "run"
)

type ProcessStep struct {
	ID       string   `json:"id"`
	Kind     ToolKind `json:"kind"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	ToolName string   `json:"toolName,omitempty"`
	FileRef  string   `json:"fileRef,omitempty"`
	Done     bool     `json:"done"`
}

type SourceNote struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionItem struct {
	ID          string           `json:"id"`
	Prompt      string           `json:"prompt"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect,omitempty"`
	Chosen      []int            `json:"chosen,omitempty"`
	CustomText  string           `json:"customText,omitempty"`
}

type QuestionCard struct {
	RequestID string         `json:"requestId"`
	Questions []QuestionItem `json:"questions"`
	Submitted bool           `json:"submitted,omitempty"`
}

type ApprovalDecision string

const (
	ApprovalPending ApprovalDecision = ""
	ApprovalAllow   ApprovalDecision = "allow"
	ApprovalDeny    ApprovalDecision = "deny"
	ApprovalSkip    ApprovalDecision = "skip"
)

type ApprovalCard struct {
	RequestID string           `json:"requestId"`
	ToolName  string           `json:"toolName"`
	Command   string           `json:"command"`
	Reason    string           `json:"reason,omitempty"`
	Decision  ApprovalDecision `json:"decision,omitempty"`
	Failed    bool             `json:"failed,omitempty"`
	ErrorText string           `json:"errorText,omitempty"`
}

type ChatMessage struct {
	Role        string          `json:"role"` // "user", "assistant", "tool"
	Content     string          `json:"content"`
	Context     string          `json:"context,omitempty"`
	ToolCalls   json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Plan        *BatchPlan      `json:"plan,omitempty"`
	Process     []ProcessStep   `json:"process,omitempty"`
	Sources     []SourceNote    `json:"sources,omitempty"`
	Question    *QuestionCard   `json:"question,omitempty"`
	Approval    *ApprovalCard   `json:"approval,omitempty"`
	DurationMs  int64           `json:"durationMs,omitempty"`
	Refs                []string        `json:"refs,omitempty"`
	Skills              []string        `json:"skills,omitempty"`
	ActivePage          string          `json:"active_page,omitempty"`
	Attachments         []Attachment    `json:"attachments,omitempty"`
	AttachmentsSeparate bool            `json:"attachmentsSeparate,omitempty"`
	Error               string          `json:"error,omitempty"`
	NeedSetting bool            `json:"needSetting,omitempty"`
}

// Attachment is a file or image the user pasted into the AI composer. Path is
// Block's own copy under the data directory, so the original may move.
type Attachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Mime string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// IsImage reports whether the attachment is a raster image a vision model can read.
func (a Attachment) IsImage() bool {
	switch a.Mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

type Conversation struct {
	ID              uint64        `json:"id"`
	Title           string        `json:"title"`
	Notebook        string        `json:"notebook,omitempty"`
	OriginPage      string        `json:"origin_page,omitempty"`
	Messages        []ChatMessage `json:"messages"`
	Backend         BackendKind   `json:"backend,omitempty"`
	NativeSessionID string        `json:"native_session_id,omitempty"`
}

type SentDraft struct {
	Text         string       `json:"text"`
	Refs         []string     `json:"refs"`
	Skills       []string     `json:"skills"`
	Attachments  []Attachment `json:"attachments,omitempty"`
	Conversation uint64       `json:"conversation"`
	MessageIndex int          `json:"message_index"`
}

type NotebookSessionState struct {
	Conversation *uint64      `json:"conversation,omitempty"`
	Text         string       `json:"text,omitempty"`
	Refs         []string     `json:"refs,omitempty"`
	Skills       []string     `json:"skills,omitempty"`
	Attachments  []Attachment `json:"attachments,omitempty"`
	Retry        *SentDraft   `json:"retry,omitempty"`
}

type ActiveDraft struct {
	Conversation *uint64                         `json:"conversation"`
	Text         string                          `json:"text"`
	Refs         []string                        `json:"refs"`
	Skills       []string                        `json:"skills"`
	Attachments  []Attachment                    `json:"attachments,omitempty"`
	Retry        *SentDraft                      `json:"retry,omitempty"`
	ByNotebook   map[string]NotebookSessionState `json:"by_notebook,omitempty"`
}

type SkillSpec struct {
	ID          string
	Name        string
	Description string
	Content     string
}

type AnnotationConfig struct {
	Backend  BackendKind `json:"backend"`
	Provider string      `json:"provider"`
	Model    string      `json:"model"`
	Thinking string      `json:"thinking"`
}
