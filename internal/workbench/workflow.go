package workbench

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed flow_edges.json
var flowEdgesJSON []byte

// 子流程拓扑随后端发布，浏览器和状态机使用同一版本的流程定义。
var FlowEdges = func() map[string][][]string {
	var edges map[string][][]string
	if err := json.Unmarshal(flowEdgesJSON, &edges); err != nil {
		panic(err)
	}
	return edges
}()

type Step struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	AI    bool   `json:"ai"`
	Human bool   `json:"human"`
}
type Node struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
}

func steps(id string, names ...string) []Step {
	out := []Step{}
	for i, n := range names {
		ai := strings.HasPrefix(n, "*")
		out = append(out, Step{ID: fmt.Sprint(id, i+1), Name: strings.TrimLeft(n, "*!"), AI: ai, Human: strings.HasPrefix(n, "!")})
	}
	return out
}

var Workflow = []Node{
	{"A", "初始化", "载入完整章节、结构和当前有效版本", steps("A", "读取配置", "校验编号与方向", "确认数据有效", "报告校验问题", "建立完整章节索引", "载入版本", "设置运行方向", "输出初始状态")},
	{"B", "定位与检索", "读取前后各两章及关联章节，再寻找有出处的原文", steps("B", "读取章号", "定位前后两章及关联章", "区分事实与计划", "检查段落绑定", "读取绑定原文", "检索目标章节", "*判断材料缺口", "!补充必要资料", "补充检索", "组织引用材料")},
	{"C", "人工创意", "每章由你输入或确认，原样交付必留要求与可选想法", steps("C", "准备本章创意", "!等待你输入并确认", "记录人工提交", "采用已确认创意", "区分必留与可选", "交付创作材料")},
	{"D", "编制大纲", "从后向前编纲，伏笔模糊提示随大纲一起审核", steps("D", "*按需补齐伏笔提示", "*自由生成大纲", "保存完整候选", "*整理情节与伏笔提示", "格式化大纲及修订提议", "转入复查")},
	{"E", "写作正文", "从前向后生成正文，读取伏笔两端大纲及模糊提示", steps("E", "*按需补齐伏笔提示", "*自由生成正文", "保存完整候选", "*识别新情节与差异", "汇总修订提议", "转入复查")},
	{"F", "跨章复查", "确认实际影响后修订，按变化传播", steps("F", "接收候选与问题", "查找直接依赖", "*补查未记录关联", "建立去重复查队列", "*对照原文核查", "判断实际影响", "!提出方案与人工取舍", "*合并修订相关内容", "*核验意见落实并传播", "检查剩余队列", "输出本批次结果")},
	{"G", "保存与同步", "原子启用完整批次，失效旧派生资料", steps("G", "保存候选批次", "定位改变片段", "标记派生资料失效", "*重建并核对摘要", "更新有效版本索引", "原子切换完整版本", "完成保存")},
	{"H", "共同审核", "按正序通读，依据原文决定修改", steps("H", "正序阅读与人工意见", "*检查因果认知伏笔节奏", "整理问题及证据", "核验引用版本", "!等待人工选择", "检查选定修改", "返回修订队列", "记录审核版本", "返回进度调度")},
	{"J", "进度调度", "按完成状态及审核有效性推进", steps("J", "读取进度", "检查未完成审核点", "进入审核", "查找未生成章节", "选择下一章", "进入定位检索", "检查最终审核有效性", "完成当前模式")},
}

// 操作由后端发布；界面只展示当前状态允许执行的按钮。
var RunActions = map[string][]string{
	"idle": {"start", "step"}, "running": {"pause", "stop"},
	"paused": {"resume", "step", "stop"}, "failed": {"resume", "step", "stop"},
	"waiting_input": {"resume", "step", "stop", "decide"},
	"completed":     {"start", "step"}, "stopped": {"start", "step"},
}
var promptDefaults = map[string]string{
	"A":  "按照项目结构与当前有效版本初始化。",
	"B":  "你负责小说材料检索。只根据提供的原文判断材料是否充足，不把未来大纲当成已发生事实。",
	"C":  "本节点由用户输入并确认，不调用模型，不自动补写。提交内容原样交付后续创作。",
	"D":  "你是小说大纲作者，正在倒序编纲。以后续已确认章节为终点，向前推导本章的事件与因果，遵守用户最新创意与决定。",
	"E":  "你是小说作者。根据当前大纲、前文和人工创意，自由写出完整章节。用具体场景、动作和对话表现人物，避免重复解释。只输出正文。",
	"F":  "你负责跨章核查。先确认实际影响再修改。必须提供原文证据，区分矛盾、疑点和审美建议。重要创意取舍交由人工决定。",
	"G":  "你负责资料同步。摘要忠实于原文，保留可核验的引用，不添加未发生的事件。",
	"H":  "你负责共同审核。按正序检查因果、人物认知、伏笔、节奏和重复。每个具体问题附原文证据；审美意见不当成事实错误。",
	"J":  "依照当前版本的生成进度和审核状态推进。",
	"B7": "判断当前材料是否缺少必要内容。返回 JSON：{\"queries\":[\"需要补查的关键词\"]}。不缺材料时返回空数组。仅当必须由用户决定、无法从材料获得时，额外返回 inputs 字符串数组，逐项写出需要用户补充的问题；可自由创作的内容不要提问。",
	"D1": "从已有回收章的大纲为旧作品补齐缺失的伏笔提示。" + foreshadowInstruction + foreshadowJSONInstruction + "只填写本次缺失清单；没有缺项时返回{\"foreshadows\":[]}。不重写已有大纲。",
	"E1": "读取大纲里的伏笔提示；缺失时依据回收章大纲补齐。" + foreshadowInstruction + foreshadowJSONInstruction + "只填写本次缺失清单；没有缺项时返回{\"foreshadows\":[]}。不写正文。",
	"D2": "阅读后续已确认的大纲，回答：为了让这些事情自然发生，这之前应该发生什么？把答案写成第 {{chapter}} 章大纲：本章发生什么、人物为什么这样做、造成什么变化，章末如何接到下一章开头。为后章的关键事件安排前因和铺垫，不把揭晓、高潮或关系确认提前重复演一遍。若没有后续大纲，则依据全书创意先确定本章落点。用简洁事件要点表达，对话概述意图即可，不展开成正文；篇幅随内容需要，不凑字数。只输出本章大纲，不输出推理过程。" + outlineFormatInstruction,
	"E2": "写第 {{chapter}} 章完整候选正文。默认约 2000–3000 中文字；若本节点另有篇幅要求则按其要求。只输出正文。",
	"D4": "写后整理候选大纲。顶层JSON必须包含queries、links、issues、foreshadows四个数组；没有相应内容时使用[]。queries是检索词字符串数组；links的每项为{\"from\":起始章号,\"to\":目标章号,\"reason\":\"关联内容\"}；issues使用核查问题格式。foreshadows只填本章回收的给定伏笔，每项为{\"threadId\":\"本次允许的实际编号\",\"text\":\"一句模糊提示\"}。本章设定或提醒、相邻章节收回的伏笔只作参考，不输出到这个数组。本次编号清单为空时，foreshadows必须为[]；不要因参考材料中出现T编号而补入。" + foreshadowJSONInstruction + foreshadowInstruction + "提示放入大纲一起审核，不把未写前章当成漏埋；不重写候选大纲。",
	"E4": "本节点不是写作正文，只做候选正文的结构化整理。无论公共提示词如何要求，本节点唯一输出必须是一个合法 JSON 对象，不得输出正文、解释、Markdown 或代码围栏。写后整理候选正文中的新发展及与已有计划的差异。返回 JSON：{\"queries\":[],\"links\":[{\"from\":1,\"to\":2,\"reason\":\"关联内容\"}],\"issues\":[]}。issues 使用核查问题格式。",
	"F3": "从候选变化中提取需要补查的实体或事件关键词。返回 JSON：{\"queries\":[]}。",
	"F5": "只核查本批次变化造成的实际矛盾。区分已写事实、未来计划和未知信息：倒序编纲时前章尚未生成不是漏埋伏笔，未交代不等于冲突。不要把人物的情感反应或可选写法当成错误。已确认的人工取舍不得重新质疑，未落实则按原决定自动纠正。普通衔接和措辞修正常规自动处理；只有必须推翻明确必留要求、改变关键剧情且用户尚未决定时才设critical=true，并在reason中写出相冲突的具体要求。无问题返回 {\"issues\":[]}；证据必须来自当前有效材料。",
	"F8": "合并落实本次全部已采用要求，保留此前人工决定。新人工要求覆盖冲突的旧创意；同步删除原文中已被否定的表述，不能只在旁边加一句。目标是outline时只写简洁事件要点和因果，不展开对白和场景；篇幅随内容自然增减，不为压缩字数删掉必要内容。draft才写完整正文。只输出修订后的完整目标文本，不输出说明。",
	"F9": "核验修订是否真正落实用户要求。按最新人工决定理解目标，检查旧的冲突说法是否仍残留。只核验已确认的要求，不新增创意建议，不因大纲超过建议字数而判失败。原文引用与解释分别放在quote和reason中；quote逐字连续摘录，不加编号、说明或省略号。返回任务指定的JSON。",
	"G4": "为给定内容编写简洁忠实摘要。返回 JSON：{\"summary\":\"摘要\"}。",
	"H2": "通读本次审核范围的全部内容，提出需要处理的问题。按核查问题格式返回 {\"issues\":[]}。所有意见由人工选定。",
}
var variables = map[string]bool{"chapter": true, "mode": true, "ideas": true, "materials": true, "candidate": true, "issues": true, "style": true, "foreshadows": true}
var variablePattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func ValidatePrompt(t string) error {
	for _, m := range variablePattern.FindAllStringSubmatch(t, -1) {
		if !variables[strings.TrimSpace(m[1])] {
			return fmt.Errorf("未知模板变量：%s", m[1])
		}
	}
	return nil
}
func promptText(p Project, key string) (string, string) {
	h := p.Prompts[key]
	if len(h) > 0 {
		return h[len(h)-1].Text, h[len(h)-1].ID
	}
	return promptDefaults[key], "default:" + key
}
func RenderPrompt(p Project, node string, values map[string]string) (string, string) {
	parent := node[:1]
	base, bv := promptText(p, parent)
	step, sv := promptText(p, node)
	if node == parent {
		step = ""
		sv = bv
	} else {
		// 公共提示词提供角色和风格背景；子步骤决定当前输出类型。
		// 否则 E 的“只输出正文”会覆盖 E4 的 JSON 整理契约，模型会把候选正文原样吐回，
		// 随后结构化解析从正文第一个汉字开始报 invalid character。
		step = "当前子步骤提示词（优先执行；若与公共提示词冲突，以本提示为准）：\n" + step
		base = "公共节点提示词（仅作为角色背景，不改变当前子步骤的输出格式）：\n" + base
	}
	text := base + "\n\n" + step
	return variablePattern.ReplaceAllStringFunc(text, func(m string) string {
		parts := variablePattern.FindStringSubmatch(m)
		return values[strings.TrimSpace(parts[1])]
	}), bv + "/" + sv
}
func defaults() Settings {
	m := ModelConfig{BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4.1-flash", Credential: "opencode-go", MaxTokens: 8192, Temperature: 0.8, ContextChars: 160000, TimeoutSeconds: 300}
	kimi := m
	kimi.Model = "kimi-k3"
	kimi.ReasoningEffort = "high"
	kimi.MaxTokens = 16384
	return Settings{ModelDefaultsVersion: 1, Models: map[string]ModelConfig{"outline": kimi, "draft": kimi, "orchestrator": kimi, "check": m, "summary": m}, CheckConcurrency: 2}
}

// 旧作品补齐新角色，并将旧的 OpenCode 默认创作模型迁移一次；后续手动配置不被覆盖。
func normalizeSettings(s *Settings) {
	if s.Models == nil {
		s.Models = map[string]ModelConfig{}
	}
	d := defaults()
	for role, model := range d.Models {
		if _, ok := s.Models[role]; !ok {
			s.Models[role] = model
		}
	}
	if s.ModelDefaultsVersion < 1 {
		for _, role := range []string{"outline", "draft"} {
			m := s.Models[role]
			if strings.Contains(m.BaseURL, "opencode.ai/") && m.Model == "deepseek-v4.1-flash" {
				m.Model = "kimi-k3"
				m.ReasoningEffort = "high"
				m.MaxTokens = max(m.MaxTokens, 16384)
				s.Models[role] = m
			}
		}
		s.ModelDefaultsVersion = 1
	}
}
func NewProject(name string, s Structure) Project {
	p := Project{ID: ID(), Name: name, Revision: 1, Created: now(), Structure: s, Settings: defaults(), Prompts: map[string][]PromptVersion{}, Chapters: []Chapter{}, Versions: []Version{}, Ideas: []Idea{}, References: []Reference{}, Links: []Link{}, Reviews: []Review{}, PendingEdits: []PendingEdit{}}
	p.StructureVersions = []StructureVersion{{ID: ID(), Created: now(), Structure: s}}
	for i := 1; i <= s.Chapters; i++ {
		p.Chapters = append(p.Chapters, Chapter{Number: i})
	}
	return p
}
func asJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
