package workbench

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// 50字只通过提示词约束：不截断文本，不用字数触发压缩或重试。
const foreshadowInstruction = "每条伏笔只留一句不超过50字的模糊提示，越模糊越好，仅指出需要一点什么铺垫；不展开事件、场景、对白、动作或人物动机，不替前章写大纲。保留已确定的关键事物，具体如何发生留给前章自由创作。不同伏笔可各有一句，提示属于待落实计划，不是人工创意或已经发生的事实。"

const outlineFormatInstruction = "只输出 Markdown 大纲，不加代码围栏、JSON或格式说明。正文分为“## 事件要点”和末尾的“## 伏笔提示”两部分，标题各出现一次。事件要点简述本章事件与因果；伏笔提示每条独占一行，使用任务给定的实际编号、起止章号和提醒章号，每个编号恰好出现一次。只填写本章设定、提醒或收回的伏笔，不照抄其他章节的提示。本章没有预设伏笔时，该部分只写“本章无预设伏笔。”。" + foreshadowInstruction

const foreshadowJSONInstruction = "只输出一个合法JSON对象，不加Markdown代码围栏、解释或尾随文本。foreshadows必须是数组，不能省略或写null；数组每项只含threadId和text两个字符串字段。threadId必须从本次允许输出的编号中原样复制，禁止新增、改写、重复或遗漏编号；text必须是非空的一句提示，不要把编号、章号或整条Markdown提示行放进text。参考材料中的编号不自动获得输出资格。"

func foreshadowOutputContract(threads []Thread) string {
	ids := make([]string, 0, len(threads))
	for _, thread := range threads {
		ids = append(ids, thread.ID)
	}
	contract := fmt.Sprintf("本次允许输出的伏笔编号：%s；foreshadows必须恰好%d项。", asJSON(ids), len(ids))
	if len(ids) == 0 {
		return contract + "本次无需生成任何伏笔提示，必须返回\"foreshadows\":[]。即使候选或相邻章节材料包含伏笔，也不得复制进此数组。"
	}
	return contract + "每个给定编号恰好一项；同一起止章号的不同编号仍须分别填写，不得合并。"
}

func outlineChapterFormat(p Project, chapter int) string {
	lines := []string{}
	for _, thread := range p.Structure.Threads {
		if threadTouches(thread, chapter) {
			lines = append(lines, hintLinePrefix(thread)+"（填写一句模糊提示，已有提示优先沿用）")
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("第%d章没有预设伏笔，末尾必须为：\n## 伏笔提示\n\n本章无预设伏笔。\n相邻章的伏笔仅供推导事件，不得放入本章伏笔提示区。", chapter)
	}
	return "本章伏笔提示区仅使用以下各行，保留编号及章号，把括号内填写说明替换为提示内容；禁止增加其他编号：\n" + strings.Join(lines, "\n")
}

func threadTouches(t Thread, ch int) bool {
	if t.Plant == ch || t.Payoff == ch {
		return true
	}
	for _, c := range t.Reminds {
		if c == ch {
			return true
		}
	}
	return false
}

// 伏笔材料按明确依赖读取，不依赖关键词/向量召回，两端和提醒点都能看见原文。
func structuralChapters(p Project, ch int) map[int]bool {
	wanted := nearbyChapters(p, ch)
	for _, t := range p.Structure.Threads {
		if threadTouches(t, ch) {
			wanted[t.Plant], wanted[t.Payoff] = true, true
			for _, reminder := range t.Reminds {
				wanted[reminder] = true
			}
		}
	}
	// 兼容只有连线的旧快照，双向读取，不受运行方向影响。
	for _, edge := range p.Structure.Lines {
		if edge[0] == ch {
			wanted[edge[1]] = true
		}
		if edge[1] == ch {
			wanted[edge[0]] = true
		}
	}
	return wanted
}

func payoffThreads(s Structure, ch int) []Thread {
	items := []Thread{}
	for _, t := range s.Threads {
		if t.Payoff == ch {
			items = append(items, t)
		}
	}
	return items
}

func currentForeshadow(p Project, r Run, t Thread) (ForeshadowHint, bool) {
	version := p.ActiveID(t.Payoff, "outline")
	for _, b := range r.Data.Batch {
		if b.Kind == "outline" && b.Chapter == t.Payoff {
			// 候选已经变化时，不把旧大纲派生的提示继续传给核查模型。
			for _, h := range outlineForeshadows(p, b) {
				if h.ThreadID == t.ID && h.Text != "" {
					h.SourceVersion = "candidate:" + fingerprint(b.Text)
					return h, true
				}
			}
			return ForeshadowHint{}, false
		}
	}
	// 人工编辑/回滚过提示行时，立即读取可见大纲，缓存不能盖过用户修改。
	if v := p.Current(t.Payoff, "outline"); v != nil {
		for _, h := range outlineForeshadows(p, Change{Chapter: t.Payoff, Kind: "outline", Text: v.Text}) {
			if h.ThreadID == t.ID {
				h.SourceVersion = v.ID
				return h, true
			}
		}
	}
	var found ForeshadowHint
	for _, hints := range [][]ForeshadowHint{p.Foreshadows, r.Data.Foreshadows} {
		for _, h := range hints {
			if version != "" && h.ThreadID == t.ID && h.SourceChapter == t.Payoff && h.SourceVersion == version && h.Text != "" {
				found = h
			}
		}
	}
	return found, found.Text != ""
}

type foreshadowMaterial struct {
	Thread
	Hint          string `json:"hint"`
	SourceVersion string `json:"sourceVersion,omitempty"`
	PlantState    string `json:"plantState"`
	PayoffState   string `json:"payoffState"`
}

func foreshadowMaterials(p Project, r Run, refs []Citation) []foreshadowMaterial {
	wanted := map[int]bool{r.Chapter: true}
	// 跨章修订和审核也须接收实际目标的伏笔，不能只按运行章号筛选。
	for _, ref := range refs {
		wanted[ref.Chapter] = true
	}
	state := func(ch int) string {
		for _, b := range r.Data.Batch {
			if b.Chapter == ch && b.Kind == "outline" {
				return "有未采用大纲候选，不能当作已写事实"
			}
		}
		if p.Current(ch, "outline") != nil {
			return "有已采用大纲，属于计划"
		}
		return "尚未编写，留待倒序推进，不据此报漏埋伏笔"
	}
	out := []foreshadowMaterial{}
	for _, t := range p.Structure.Threads {
		relevant := false
		for ch := range wanted {
			relevant = relevant || threadTouches(t, ch)
		}
		if relevant {
			h, _ := currentForeshadow(p, r, t)
			out = append(out, foreshadowMaterial{Thread: t, Hint: h.Text, SourceVersion: h.SourceVersion, PlantState: state(t.Plant), PayoffState: state(t.Payoff)})
		}
	}
	return out
}

func missingForeshadows(p Project, r Run) []Thread {
	missing := []Thread{}
	for _, t := range p.Structure.Threads {
		if threadTouches(t, r.Chapter) && p.Current(t.Payoff, "outline") != nil {
			if _, ok := currentForeshadow(p, r, t); !ok {
				missing = append(missing, t)
			}
		}
	}
	return missing
}

func foreshadowTask(threads []Thread) string {
	return "根据每条伏笔回收章的大纲，向埋线章和提醒章留下一点模糊内容。" + foreshadowInstruction +
		"\n需要提示的伏笔：" + asJSON(threads) +
		"\n" + foreshadowJSONInstruction + "\n" + foreshadowOutputContract(threads)
}

// 只验证编号和缺项。表达的模糊程度和50字目标交给可编辑提示词，不作程序硬限制。
func normalizeForeshadows(hints []ForeshadowHint, threads []Thread) ([]ForeshadowHint, error) {
	expected := map[string]Thread{}
	for _, t := range threads {
		expected[t.ID] = t
	}
	seen := map[string]ForeshadowHint{}
	for i, h := range hints {
		h.ThreadID = strings.TrimSpace(h.ThreadID)
		if h.ThreadID == "" {
			return nil, fmt.Errorf("foreshadows第%d项缺少非空threadId", i+1)
		}
		t, ok := expected[h.ThreadID]
		if !ok {
			return nil, fmt.Errorf("伏笔提示编号%s不在本次允许输出的清单中；%s", h.ThreadID, foreshadowOutputContract(threads))
		}
		if _, duplicate := seen[h.ThreadID]; duplicate {
			return nil, fmt.Errorf("伏笔提示编号重复：%s；每个编号只能出现一次", h.ThreadID)
		}
		if strings.TrimSpace(h.Text) == "" {
			return nil, fmt.Errorf("伏笔提示%s的text为空，必须填写非空提示", h.ThreadID)
		}
		// 只合并换行使提示保持一行，保留完整措辞，不按50字裁剪。
		line := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(strings.TrimSpace(h.Text))
		seen[h.ThreadID] = ForeshadowHint{ThreadID: t.ID, Text: line, SourceChapter: t.Payoff}
	}
	out := make([]ForeshadowHint, 0, len(threads))
	missing := []string{}
	for _, thread := range threads {
		if hint, ok := seen[thread.ID]; ok {
			out = append(out, hint)
		} else {
			missing = append(missing, thread.ID)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("伏笔提示缺少编号：%s；需要%d条，收到%d条", strings.Join(missing, "、"), len(threads), len(out))
	}
	return out, nil
}

// 格式纠错仅重试当前整理请求；不回到D2重写大纲，也不复用旧格式的成功缓存。
func (e *Engine) callForeshadowJSON(ctx context.Context, p *Project, r *Run, node, role string, refs []Citation, task string, threads []Thread) (Analysis, error) {
	base := fingerprint([]any{task, threads, refs, r.Epoch})
	var result Analysis
	_, err := e.call(ctx, p, r, node, "foreshadow-json-v3:"+base, role, refs, task, func(answer string) error {
		result = Analysis{}
		var invalid error
		if node == "D4" {
			result, invalid = parseAnalysis(answer, *p, node)
			if invalid == nil && (result.Queries == nil || result.Links == nil || result.Issues == nil) {
				invalid = fmt.Errorf("整理结果必须包含queries、links、issues三个数组，无内容时使用[]，不能省略或写null")
			}
		} else {
			invalid = parseJSON(answer, &result)
		}
		if invalid == nil && result.Foreshadows == nil {
			invalid = fmt.Errorf("缺少foreshadows数组或值为null；%s", foreshadowOutputContract(threads))
		}
		if invalid == nil {
			result.Foreshadows, invalid = normalizeForeshadows(result.Foreshadows, threads)
		}
		return invalid
	})
	if err != nil {
		r.NextNode = node
	}
	return result, err
}

// 老作品按需补齐提示，只读取既有大纲；不重写已采用章节，不把模型计划混入人工创意。
func (e *Engine) prepareForeshadows(ctx context.Context, p *Project, r *Run) error {
	missing := missingForeshadows(*p, *r)
	if len(missing) == 0 {
		return nil
	}
	refs := materials(*p, r.Chapter, "backward", nil)
	node := r.Stage + "1"
	result, err := e.callForeshadowJSON(ctx, p, r, node, "summary", refs, foreshadowTask(missing), missing)
	if err != nil {
		return err
	}
	for _, h := range result.Foreshadows {
		h.SourceVersion = p.ActiveID(h.SourceChapter, "outline")
		r.Data.Foreshadows = append(r.Data.Foreshadows, h)
	}
	return nil
}

func outlineAnalysisTask(p Project, r Run) string {
	generated := r.Data.Generated
	if generated == "" {
		for _, c := range r.Data.Batch {
			if c.Chapter == r.Chapter && c.Kind == kindFor(r.Mode) {
				generated = c.Text
				break
			}
		}
	}
	task := "候选内容：\n" + generated
	if r.Mode == "backward" {
		task += "\n整理本章回收的伏笔，已有提示优先沿用，没有时只补一句模糊安排。" +
			foreshadowTask(payoffThreads(p.Structure, r.Chapter)) +
			"\nforeshadows 与 queries、links、issues 合并在同一个 JSON 中。提示将直接放进本章大纲末尾，一起审核；不要提出修改尚未编写章节的问题。"
	}
	return task
}

func hintLinePrefix(t Thread) string {
	label := fmt.Sprintf("- %s（第%d章 → 第%d章", t.ID, t.Plant, t.Payoff)
	if len(t.Reminds) > 0 {
		parts := []string{}
		for _, ch := range t.Reminds {
			parts = append(parts, fmt.Sprint(ch))
		}
		label += "；提醒第" + strings.Join(parts, "、") + "章"
	}
	return label + "）："
}

func outlineParts(text string) (string, string) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "## 伏笔提示" {
			return strings.TrimSpace(strings.Join(lines[:i], "\n")), strings.Join(lines[i+1:], "\n")
		}
	}
	return strings.TrimSpace(text), ""
}

// 格式化只增加大纲内的可见提示区，不动正文；同一内容参与审核、版本、回滚和导出。
func formatOutline(p Project, r Run, c Change) string {
	body, _ := outlineParts(c.Text)
	if !strings.HasPrefix(body, "## 事件要点") {
		body = "## 事件要点\n\n" + body
	}
	lines := []string{}
	for _, t := range p.Structure.Threads {
		if !threadTouches(t, c.Chapter) {
			continue
		}
		h, _ := currentForeshadow(p, r, t)
		for _, pending := range c.Foreshadows {
			if pending.ThreadID == t.ID {
				h = pending
			}
		}
		text := h.Text
		if text == "" {
			text = "留待后续大纲形成。"
		}
		lines = append(lines, hintLinePrefix(t)+text)
	}
	if len(lines) == 0 {
		lines = append(lines, "本章无预设伏笔。")
	}
	return body + "\n\n## 伏笔提示\n\n" + strings.Join(lines, "\n")
}

// 只把回收章中可见的提示行作为跨章来源。编辑这些行等于修改提示，不另存一份隐藏正文。
func outlineForeshadows(p Project, c Change) []ForeshadowHint {
	_, section := outlineParts(c.Text)
	hints := []ForeshadowHint{}
	for _, t := range payoffThreads(p.Structure, c.Chapter) {
		pattern := `(?m)^\s*[-*]\s*` + regexp.QuoteMeta(t.ID) + `(?:[（(][^\r\n]*?[）)])?\s*[：:]\s*([^\r\n]+)`
		if match := regexp.MustCompile(pattern).FindStringSubmatch(section); len(match) == 2 && strings.TrimSpace(match[1]) != "" {
			hints = append(hints, ForeshadowHint{ThreadID: t.ID, Text: strings.TrimSpace(match[1]), SourceChapter: t.Payoff})
		}
	}
	return hints
}

// 与正文/大纲在同一事务采用。被替换的来源提示自动失效，历史仍在项目与检查点中。
func commitForeshadows(p *Project, r Run, batch []Change) {
	add := func(h ForeshadowHint) {
		if h.SourceVersion == "" || h.SourceVersion != p.ActiveID(h.SourceChapter, "outline") {
			return
		}
		for _, old := range p.Foreshadows {
			if old.ThreadID == h.ThreadID && old.SourceVersion == h.SourceVersion {
				return
			}
		}
		p.Foreshadows = append(p.Foreshadows, h)
	}
	for _, h := range r.Data.Foreshadows {
		add(h)
	}
	for _, c := range batch {
		if c.Kind == "outline" {
			for _, h := range outlineForeshadows(*p, c) {
				h.SourceVersion = p.ActiveID(c.Chapter, "outline")
				add(h)
			}
		}
	}
}
