package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// ID 使用随机标识隔离一次运行、一次请求和一个内容版本。
func ID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Turning struct {
	Ch   int    `json:"ch"`
	Arc  int    `json:"arc"`
	Kind string `json:"kind"`
}
type Thread struct {
	ID      string `json:"id"`
	Plant   int    `json:"plant"`
	Payoff  int    `json:"payoff"`
	Reminds []int  `json:"reminds"`
	Span    int    `json:"span"`
	Tag     string `json:"tag"`
}

// 伏笔提示是模型从回收章推导的模糊计划，不进入人工创意或已发生事实。
// 与回收章的大纲版本绑定；旧版本提示保留历史，但不能继续约束新版本。
type ForeshadowHint struct {
	ThreadID      string `json:"threadId"`
	Text          string `json:"text"`
	SourceChapter int    `json:"sourceChapter"`
	SourceVersion string `json:"sourceVersion"`
}
type Structure struct {
	Chapters         int       `json:"chapters"`
	Seed             int64     `json:"seed"`
	MinGap           int       `json:"minGap"`
	GeneratorVersion string    `json:"generatorVersion"`
	Arcs             [][2]int  `json:"arcs"`
	Nodes            []Turning `json:"nodes"`
	Threads          []Thread  `json:"threads"`
	Lines            [][2]int  `json:"lines"`
	FixedReminders   []string  `json:"fixedReminders"`
}
type Idea struct {
	ID       string `json:"id"`
	Chapter  int    `json:"chapter"`
	Text     string `json:"text"`
	Required bool   `json:"required"`
	Active   bool   `json:"active"`
}
type Reference struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}
type Version struct {
	ID           string `json:"id"`
	Chapter      int    `json:"chapter"`
	Kind         string `json:"kind"`
	Text         string `json:"text"`
	Summary      string `json:"summary"`
	Created      string `json:"created"`
	Source       string `json:"source"`
	Parent       string `json:"parent"`
	SummaryValid bool   `json:"summaryValid"`
}
type Chapter struct {
	Number  int    `json:"number"`
	Outline string `json:"outline"`
	Draft   string `json:"draft"`
}
type Link struct {
	ID            string `json:"id"`
	From          int    `json:"from"`
	To            int    `json:"to"`
	Reason        string `json:"reason"`
	SourceVersion string `json:"sourceVersion"`
	TargetVersion string `json:"targetVersion"`
	Valid         bool   `json:"valid"`
}
type PromptVersion struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created string `json:"created"`
}
type ModelConfig struct {
	ReasoningEffort string  `json:"reasoningEffort,omitempty"`
	BaseURL         string  `json:"baseUrl"`
	Model           string  `json:"model"`
	Credential      string  `json:"credential"`
	MaxTokens       int     `json:"maxTokens"`
	Temperature     float64 `json:"temperature"`
	ContextChars    int     `json:"contextChars"`
	TimeoutSeconds  int     `json:"timeoutSeconds"`
	Thinking        bool    `json:"thinking"`
}
type Settings struct {
	ModelDefaultsVersion int                    `json:"modelDefaultsVersion"`
	OrchestratorEnabled  bool                   `json:"orchestratorEnabled"`
	OrchestratorGuidance string                 `json:"orchestratorGuidance"`
	Models               map[string]ModelConfig `json:"models"`
	ArcReviews           bool                   `json:"arcReviews"`
	CheckConcurrency     int                    `json:"checkConcurrency"`
	// 以下文风字段仅兼容历史项目；当前流程不读取、不注入请求。
	StyleSamples          string `json:"styleSamples"`
	EvalScenes            string `json:"evalScenes"`
	StyleVerified         bool   `json:"styleVerified"`
	StyleModelFingerprint string `json:"styleModelFingerprint"`
	EmbeddingURL          string `json:"embeddingUrl"`
	EmbeddingModel        string `json:"embeddingModel"`
	EmbeddingCredential   string `json:"embeddingCredential"`
}
type PendingEdit struct {
	ID      string `json:"id"`
	Chapter int    `json:"chapter"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Base    string `json:"base"`
}
type Review struct {
	ID       string            `json:"id"`
	Mode     string            `json:"mode"`
	Chapters []int             `json:"chapters"`
	Versions map[string]string `json:"versions"`
	Valid    bool              `json:"valid"`
	Created  string            `json:"created"`
}
type Note struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	Chapter int    `json:"chapter"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

type InputRequest struct {
	ID        string   `json:"id"`
	Questions []string `json:"questions"`
	Answer    string   `json:"answer"`
}

// CreativeInput 保存人工或用户启用的统筹交付；Source 区分来源。
// 提交记录独立于章节临时数据保存在 Run 中，切章和检查点恢复不会丢失。
type CreativeInput struct {
	Source         string `json:"source,omitempty"`
	ID             string `json:"id"`
	Chapter        int    `json:"chapter"`
	Cycle          int    `json:"cycle"`
	Ideas          []Idea `json:"ideas"`
	EmptyConfirmed bool   `json:"emptyConfirmed"`
	SubmittedAt    string `json:"submittedAt"`
}

type Project struct {
	OrchestratorMemory OrchestratorMemory         `json:"orchestratorMemory"`
	Foreshadows        []ForeshadowHint           `json:"foreshadows,omitempty"`
	HumanDecisions     []Proposal                 `json:"humanDecisions,omitempty"`
	Archived           bool                       `json:"archived"`
	Notes              []Note                     `json:"notes"`
	ID                 string                     `json:"id"`
	Name               string                     `json:"name"`
	Revision           int                        `json:"revision"`
	Created            string                     `json:"created"`
	Structure          Structure                  `json:"structure"`
	StructureVersions  []StructureVersion         `json:"structureVersions"`
	Chapters           []Chapter                  `json:"chapters"`
	Versions           []Version                  `json:"versions"`
	Ideas              []Idea                     `json:"ideas"`
	References         []Reference                `json:"references"`
	Links              []Link                     `json:"links"`
	Prompts            map[string][]PromptVersion `json:"prompts"`
	Settings           Settings                   `json:"settings"`
	PendingEdits       []PendingEdit              `json:"pendingEdits"`
	Reviews            []Review                   `json:"reviews"`
}
type StructureVersion struct {
	ID        string    `json:"id"`
	Created   string    `json:"created"`
	Structure Structure `json:"structure"`
}
type Citation struct {
	Chapter   int    `json:"chapter"`
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	Paragraph int    `json:"paragraph"`
	Text      string `json:"text"`
	Plan      bool   `json:"plan"`
}
type Change struct {
	Foreshadows []ForeshadowHint `json:"foreshadows,omitempty"`
	Chapter     int              `json:"chapter"`
	Kind        string           `json:"kind"`
	Base        string           `json:"base"`
	Text        string           `json:"text"`
	Summary     string           `json:"summary"`
	Reason      string           `json:"reason"`
	Embeddings  [][]float64      `json:"embeddings,omitempty"`
}
type Proposal struct {
	Source      string     `json:"source,omitempty"`
	ID          string     `json:"id"`
	Chapter     int        `json:"chapter"`
	Kind        string     `json:"kind"`
	Problem     string     `json:"problem"`
	Instruction string     `json:"instruction"`
	Evidence    []Citation `json:"evidence"`
	Critical    bool       `json:"critical"`
	Decision    string     `json:"decision"`
	Reason      string     `json:"reason"`
	Applied     bool       `json:"applied"`
}
type CheckItem struct {
	Chapter int    `json:"chapter"`
	Kind    string `json:"kind"`
	Cause   string `json:"cause"`
}
type RunData struct {
	Foreshadows      []ForeshadowHint `json:"foreshadows,omitempty"`
	RevisionIDs      []string         `json:"revisionIds,omitempty"`
	RevisionFeedback string           `json:"revisionFeedback,omitempty"`
	RevisionTries    int              `json:"revisionTries,omitempty"`
	CreativeInput    *CreativeInput   `json:"creativeInput,omitempty"`
	Input            *InputRequest    `json:"input,omitempty"`
	Generated        string           `json:"generated,omitempty"`
	Analysis         *Analysis        `json:"analysis,omitempty"`
	NextChapter      int              `json:"nextChapter,omitempty"`
	Materials        []Citation       `json:"materials"`
	Batch            []Change         `json:"batch"`
	Queue            []CheckItem      `json:"queue"`
	Seen             map[string]bool  `json:"seen"`
	Rounds           map[string]int   `json:"rounds"`
	Proposals        []Proposal       `json:"proposals"`
	NewLinks         []Link           `json:"newLinks"`
	ReviewChapters   []int            `json:"reviewChapters"`
	ReviewReturn     bool             `json:"reviewReturn"`
	ReviewChecked    bool             `json:"reviewChecked"`
	ReviewApproved   bool             `json:"reviewApproved"`
	ContextIdeas     []Idea           `json:"contextIdeas"`
	FutureIdeas      []Idea           `json:"futureIdeas"`
	PendingEditIDs   []string         `json:"pendingEditIds"`
	AfterCommit      string           `json:"afterCommit"`
	SearchTerms      []string         `json:"searchTerms"`
	CheckIndex       int              `json:"checkIndex"`
	RevisionIndex    int              `json:"revisionIndex"`
}
type Run struct {
	OrchestratorGate string          `json:"orchestratorGate,omitempty"`
	HumanDecisions   []Proposal      `json:"humanDecisions,omitempty"`
	CreativeInputs   []CreativeInput `json:"creativeInputs"`
	NextNode         string          `json:"nextNode"`
	SingleStep       bool            `json:"singleStep"`
	ID               string          `json:"id"`
	ProjectID        string          `json:"projectId"`
	Mode             string          `json:"mode"`
	Status           string          `json:"status"`
	Stage            string          `json:"stage"`
	Node             string          `json:"node"`
	Chapter          int             `json:"chapter"`
	Epoch            int             `json:"epoch"`
	Revision         int             `json:"revision"`
	Cycle            int             `json:"cycle"`
	Created          string          `json:"created"`
	Updated          string          `json:"updated"`
	Error            string          `json:"error"`
	Message          string          `json:"message"`
	Data             RunData         `json:"data"`
}
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"total"`
}
type Attempt struct {
	ID            string      `json:"id"`
	RunID         string      `json:"runId"`
	TaskKey       string      `json:"taskKey"`
	Node          string      `json:"node"`
	Chapter       int         `json:"chapter"`
	Status        string      `json:"status"`
	Prompt        string      `json:"prompt"`
	PromptVersion string      `json:"promptVersion"`
	Model         ModelConfig `json:"modelConfig"`
	Inputs        []Citation  `json:"inputs"`
	Output        string      `json:"output"`
	Reasoning     string      `json:"reasoning"`
	Error         string      `json:"error"`
	Usage         Usage       `json:"usage"`
	Created       string      `json:"created"`
	Finished      string      `json:"finished"`
}
type Event struct {
	Seq     int64  `json:"seq"`
	RunID   string `json:"runId"`
	Type    string `json:"type"`
	Data    any    `json:"data"`
	Created string `json:"created"`
}

func (p *Project) Current(ch int, kind string) *Version {
	if ch < 1 || ch > len(p.Chapters) {
		return nil
	}
	id := p.Chapters[ch-1].Outline
	if kind == "draft" {
		id = p.Chapters[ch-1].Draft
	}
	for i := len(p.Versions) - 1; i >= 0; i-- {
		if p.Versions[i].ID == id {
			return &p.Versions[i]
		}
	}
	return nil
}
func (p *Project) ActiveID(ch int, kind string) string {
	v := p.Current(ch, kind)
	if v == nil {
		return ""
	}
	return v.ID
}
func freshData() RunData {
	return RunData{Seen: map[string]bool{}, Rounds: map[string]int{}, Batch: []Change{}, Queue: []CheckItem{}, Proposals: []Proposal{}, NewLinks: []Link{}}
}
