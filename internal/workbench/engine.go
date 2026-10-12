package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type worker struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Engine struct {
	Store    *Store
	Provider ModelProvider
	mu       sync.Mutex
	workers  map[string]worker
}

func NewEngine(s *Store, p ModelProvider) *Engine {
	return &Engine{Store: s, Provider: p, workers: map[string]worker{}}
}
func (e *Engine) Close() {
	e.mu.Lock()
	ws := []worker{}
	for _, w := range e.workers {
		w.cancel()
		ws = append(ws, w)
	}
	e.mu.Unlock()
	for _, w := range ws {
		<-w.done
	}
}
func (e *Engine) Start(project, mode string) (Run, error) {
	return e.StartMode(project, mode, false)
}
func (e *Engine) StartMode(project, mode string, singleStep bool) (Run, error) {
	if mode != "backward" && mode != "forward" {
		return Run{}, fmt.Errorf("模式必须为 backward 或 forward")
	}
	p, err := e.Store.Project(project)
	if err != nil {
		return Run{}, err
	}
	if p.Archived {
		return Run{}, fmt.Errorf("请先从作品管理取消归档")
	}
	if mode == "forward" {
		for _, c := range p.Chapters {
			if c.Outline == "" {
				return Run{}, fmt.Errorf("请先完成全部章节大纲，缺少第 %d 章", c.Number)
			}
		}
	}
	r := Run{ID: ID(), ProjectID: project, Mode: mode, Status: "running", Stage: "A", Node: "A1", NextNode: "A1", SingleStep: singleStep, Chapter: 1, Epoch: 1, Revision: 1, Created: now(), Updated: now(), Data: freshData()}
	r.HumanDecisions = append([]Proposal{}, p.HumanDecisions...)
	if mode == "backward" {
		r.Chapter = len(p.Chapters)
	}
	if err = e.Store.CreateRun(r); err != nil {
		return r, err
	}
	e.Store.Event(r.ID, "run", r)
	e.launch(r.ID)
	return r, nil
}
func (e *Engine) launch(id string) {
	e.mu.Lock()
	if _, ok := e.workers[id]; ok {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := worker{cancel: cancel, done: make(chan struct{})}
	e.workers[id] = w
	e.mu.Unlock()
	go func() {
		defer func() { e.mu.Lock(); delete(e.workers, id); close(w.done); e.mu.Unlock() }()
		e.work(ctx, id)
	}()
}

// Control 先使 epoch 失效再取消网络请求，旧回调因此无法覆盖暂停后的状态。
func (e *Engine) Control(id, action string, revision int) (Run, error) {
	e.mu.Lock()
	w, active := e.workers[id]
	e.mu.Unlock()
	r, err := e.Store.UpdateRun(id, 0, func(r *Run) error {
		if revision > 0 && r.Revision != revision {
			return ErrConflict
		}
		switch action {
		case "pause":
			if r.Status != "running" {
				return fmt.Errorf("当前运行不能暂停")
			}
			r.Status = "paused"
			r.Epoch++
			r.Message = "已暂停，部分草稿已保留"
		case "stop":
			if r.Status == "completed" || r.Status == "stopped" {
				return fmt.Errorf("运行已经结束")
			}
			r.Status = "stopped"
			r.Epoch++
			r.Message = "本次运行已终止"
		case "resume", "step":
			if r.Status != "paused" && r.Status != "failed" && r.Status != "waiting_input" {
				return fmt.Errorf("当前运行不能继续")
			}
			// 旧版把引用格式和900字限制混入修订失败；升级后先核验现有候选，避免再重写一遍。
			if r.Status == "failed" && r.Node == "F9" && len(r.Data.RevisionIDs) > 0 &&
				(strings.Contains(r.Error, "请提供修订后原文中的逐字证据") || strings.Contains(r.Error, "将大纲压缩为300–600字")) {
				r.NextNode = "F9"
				r.Data.RevisionFeedback = ""
				r.Data.RevisionTries = 0
			}
			if r.Status == "failed" && r.Node == "D2" && r.Data.Generated != "" && strings.HasPrefix(r.Error, "大纲自动压缩后仍超过900字") {
				r.NextNode = "D3"
			}
			r.Status = "running"
			r.Epoch++
			r.Error = ""
			r.Message = "继续执行"
			r.SingleStep = action == "step"
		default:
			return fmt.Errorf("未知运行操作")
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	if active {
		w.cancel()
		<-w.done
	}
	e.Store.interruptAttempts(id)
	e.Store.Event(id, "run", r)
	if action == "resume" || action == "step" {
		e.launch(id)
	}
	return r, nil
}
func (e *Engine) save(r *Run) error {
	saved, err := e.Store.UpdateRun(r.ID, r.Epoch, func(current *Run) error { rev := current.Revision; *current = *r; current.Revision = rev; return nil })
	if err == nil {
		*r = saved
		e.Store.Event(r.ID, "run", r)
	}
	return err
}
func (e *Engine) node(r *Run, id string) error {
	r.Node = id
	saved, err := e.Store.UpdateRun(r.ID, r.Epoch, func(current *Run) error { current.Node = id; return nil })
	if err != nil {
		return err
	}
	r.Revision = saved.Revision
	return e.Store.Event(r.ID, "node", map[string]any{"node": id, "chapter": r.Chapter, "status": "running", "cycle": r.Cycle})
}
func (e *Engine) mark(r *Run, ids ...string) error {
	for _, id := range ids {
		if err := e.node(r, id); err != nil {
			return err
		}
		e.Store.Event(r.ID, "node", map[string]any{"node": id, "chapter": r.Chapter, "status": "succeeded", "cycle": r.Cycle})
	}
	return nil
}
func (e *Engine) wait(r *Run, message string) error {
	r.Status = "waiting_input"
	r.Message = message
	if p, err := e.Store.Project(r.ProjectID); err == nil && p.Settings.OrchestratorEnabled && !r.SingleStep {
		gate := r.NextNode
		if gate == "C2" || gate == "B8" || gate == "F7" || gate == "H5" {
			r.Status = "running"
			r.OrchestratorGate = gate
			r.Message = "统筹正在处理 " + gate
		}
	}
	return e.save(r)
}
func (e *Engine) work(ctx context.Context, id string) {
	retries := 0
	for ctx.Err() == nil {
		r, err := e.Store.Run(id)
		if err != nil || r.Status != "running" {
			return
		}
		p, err := e.Store.Project(r.ProjectID)
		if err == nil {
			if r.OrchestratorGate != "" {
				err = e.orchestrate(ctx, &p, &r)
				if err == nil {
					e.Store.Event(r.ID, "run", r)
					e.Store.Event(r.ID, "project", map[string]string{"projectId": p.ID})
				}
			} else {
				err = e.step(ctx, &p, &r)
			}
		}
		if err != nil {
			if errors.Is(err, ErrStopped) || ctx.Err() != nil {
				return
			}
			if errors.Is(err, ErrConflict) && r.OrchestratorGate != "" {
				continue
			}
			var exhausted *RetriesExhausted
			if !errors.As(err, &exhausted) && retries < 3 {
				retries++
				delay := time.Duration(1<<(retries-1)) * time.Second
				e.Store.Event(id, "retry", map[string]any{"node": r.Node, "retry": retries, "maxRetries": 3, "delaySeconds": delay.Seconds(), "message": fmt.Sprintf("%s 执行中断，自动重试（%d/3）", r.Node, retries), "error": err.Error()})
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				continue
			}
			r.Status = "failed"
			if !errors.As(err, &exhausted) {
				err = &RetriesExhausted{Cause: err}
			}
			r.Error = err.Error()
			r.Message = "自动重试已用尽，进度已保留；修正原因后可继续"
			e.save(&r)
			return
		}
		retries = 0
	}
}

// 人工编辑在安全点先进入同一复查管道；实际内容与相关派生资料一起原子采用。
func (e *Engine) applyPending(p *Project, r *Run) (bool, error) {
	if len(p.PendingEdits) == 0 {
		return false, nil
	}
	next := freshData()
	for _, edit := range p.PendingEdits {
		if p.ActiveID(edit.Chapter, edit.Kind) != edit.Base {
			return false, fmt.Errorf("待采用编辑与第 %d 章当前版本冲突，请在编辑器重新保存", edit.Chapter)
		}
		next.Batch = append(next.Batch, Change{Chapter: edit.Chapter, Kind: edit.Kind, Text: edit.Text, Base: edit.Base, Reason: "人工编辑"})
		next.PendingEditIDs = append(next.PendingEditIDs, edit.ID)
	}
	for _, idea := range p.Ideas {
		if idea.Active {
			next.ContextIdeas = append(next.ContextIdeas, idea)
		}
	}
	r.Data = next
	r.Stage = "F"
	r.NextNode = "F1"
	r.Cycle++
	return true, e.save(r)
}

func fingerprint(v any) string {
	sum := sha256.Sum256([]byte(asJSON(v)))
	return hex.EncodeToString(sum[:8])
}
func completeScope(p Project, scope []int, mode string) bool {
	for _, c := range scope {
		if p.ActiveID(c, kindFor(mode)) == "" {
			return false
		}
	}
	return true
}
func reviewValid(p Project, scope []int, mode string) bool {
	for _, v := range p.Reviews {
		if !v.Valid || v.Mode != mode || asJSON(v.Chapters) != asJSON(scope) {
			continue
		}
		ok := true
		for _, c := range scope {
			if v.Versions[fmt.Sprintf("%d:%s", c, kindFor(mode))] != p.ActiveID(c, kindFor(mode)) {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}
func capQueries(q []string) []string {
	out := []string{}
	for _, s := range q {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
		if len(out) == 6 {
			break
		}
	}
	return out
}
func mergeCitations(a, b []Citation) []Citation {
	seen := map[string]bool{}
	out := []Citation{}
	for _, list := range [][]Citation{a, b} {
		for _, c := range list {
			k := fmt.Sprintf("%s:%d", c.Version, c.Paragraph)
			if !seen[k] {
				seen[k] = true
				out = append(out, c)
			}
		}
	}
	return out
}
func citations(v Version, plan bool) []Citation {
	out := []Citation{}
	for i, t := range paragraphs(v.Text) {
		out = append(out, Citation{v.Chapter, v.Kind, v.ID, i + 1, t, plan})
	}
	return out
}

// 两个方向共用相邻两章窗口：倒序编第48章能读49、50章，正序写作能读前两章。
func nearbyChapters(p Project, ch int) map[int]bool {
	wanted := map[int]bool{}
	for c := ch - 2; c <= ch+2; c++ {
		if c >= 1 && c <= len(p.Chapters) {
			wanted[c] = true
		}
	}
	return wanted
}

func materials(p Project, ch int, mode string, batch []Change) []Citation {
	wanted := structuralChapters(p, ch)
	for _, l := range p.Links {
		if l.Valid {
			if l.From == ch {
				wanted[l.To] = true
			}
			if l.To == ch {
				wanted[l.From] = true
			}
		}
	}
	order := []int{}
	for c := range wanted {
		order = append(order, c)
	}
	sort.Ints(order)
	out := []Citation{}
	for _, c := range order {
		for _, kind := range []string{"outline", "draft"} {
			if mode == "backward" && kind == "draft" {
				continue
			}
			v := p.Current(c, kind)
			for _, b := range batch {
				if b.Chapter == c && b.Kind == kind {
					v = &Version{ID: "candidate:" + fingerprint(b.Text), Chapter: c, Kind: kind, Text: b.Text}
				}
			}
			if v != nil {
				out = append(out, citations(*v, kind == "outline")...)
			}
		}
	}
	return out
}

// 预览与实际调用共享组装逻辑，材料、参考资料与结构不会在预览中缺失。
func requestText(latest Project, r *Run, node string, refs []Citation, task string) (string, string, string) {
	if node == "O1" || node == "O2" {
		return orchestratorPrompt, task, "default:orchestrator-v1"
	}
	values := map[string]string{"chapter": fmt.Sprint(r.Chapter), "mode": r.Mode, "ideas": asJSON(r.Data.ContextIdeas), "materials": asJSON(refs), "candidate": asJSON(r.Data.Batch), "issues": asJSON(r.Data.Proposals), "style": "", "foreshadows": asJSON(foreshadowMaterials(latest, *r, refs))}
	system, pv := RenderPrompt(latest, node, values)
	user := "当前章节：" + fmt.Sprint(r.Chapter) + "；模式：" + r.Mode + "\n人工创意：" + values["ideas"] + "\n原文材料（outline 是计划，draft 是已写正文，candidate 是未采用候选）：\n" + asJSON(refs) + "\n参考资料：" + asJSON(latest.References) + "\n结构计划：" + asJSON(relevantStructure(latest.Structure, r.Chapter)) + "\n" + task
	user += "\n伏笔提示与两端状态（参考材料，不是本次输出清单）：" + values["foreshadows"] + "\n伏笔提示是大纲中的模糊安排，写作时结合两端章节原文理解，具体情节自由形成；埋线与提醒不提前演出回收。同一句提示在两端出现只是大纲标注，不是重复演出事件。对面章节尚未编写时只能视为待安排，不能臆造原文，也不因空白要求人工处理。"
	if node == "D2" || node == "F8" {
		user += "\n目标为大纲时使用以下格式：" + outlineFormatInstruction
	}
	if node == "D2" || node == "E2" {
		neighbors := []string{}
		for c := r.Chapter - 2; c <= r.Chapter+2; c++ {
			if c < 1 || c > len(latest.Chapters) || c == r.Chapter {
				continue
			}
			kinds := []string{}
			if latest.Current(c, "outline") != nil {
				kinds = append(kinds, "大纲计划")
			}
			if r.Mode == "forward" && latest.Current(c, "draft") != nil {
				kinds = append(kinds, "已写正文")
			}
			state := "尚无已采用内容，不得臆测成既有事实"
			if len(kinds) > 0 {
				state = strings.Join(kinds, "、") + "见原文材料"
			}
			neighbors = append(neighbors, fmt.Sprintf("第%d章：%s", c, state))
		}
		user += "\n前后两章材料状态：\n" + strings.Join(neighbors, "\n")
		if r.Mode == "backward" {
			user += "\n章节承接：正在倒序编制第" + fmt.Sprint(r.Chapter) + "章。编号更大的已采用大纲规定后续事件和揭示时点；本章安排通向它们的前因、铺垫和阶段变化，不把后章已安排的身份揭晓、关系确认或高潮再写一遍，也不提前完成后章的目标。全书创意描述的是整本书，不代表每一章都必须兑现全部创意。"
		} else {
			user += "\n章节承接：正在正序写第" + fmt.Sprint(r.Chapter) + "章。承接前两章结尾的人物认知、关系和事件进度，不重演已经完成的情节。后续大纲只约束未来安排，不能当作已发生事实，也不要提前兑现后章的揭示。"
		}
	}
	discovered := []Link{}
	for _, ls := range [][]Link{latest.Links, r.Data.NewLinks} {
		for _, l := range ls {
			if (l.From == r.Chapter || l.To == r.Chapter) && (l.Valid || l.ID == "") {
				discovered = append(discovered, l)
			}
		}
	}
	user += "\n创作中发现的关联（待落实的部分仍是计划）：" + asJSON(discovered)
	// 人工决定优先于旧创意和旧原文；跨章核查也必须看到已做出的取舍。
	if decisions := decisionContext(*r); len(decisions) > 0 {
		user += "\n已提交的人工决定与统筹决定（source=orchestrator 是统筹判断，空值是人工决定；人工明确要求优先，其他同来源的决定按后项优先；只在相关章节与情节适用）：\n" + asJSON(decisions) + "\naccepted 的 instruction 是已确认修订方向，rejected 表示已否决的方向；不得换个说法重复提交。已决定的问题若尚未落实，按已有要求自动纠正。统筹建议不能覆盖人工必留要求。"
	}
	// 将本次输出边界放在材料和人工决定之后；默认和自定义提示词都采用同一份真实清单。
	switch node {
	case "D2":
		user += "\n本次大纲输出格式：\n" + outlineChapterFormat(latest, r.Chapter)
	case "D1", "E1":
		user += "\n本次输出格式：\n" + foreshadowJSONInstruction + "\n" + foreshadowOutputContract(missingForeshadows(latest, *r))
	case "D4":
		user += "\n本次输出格式（只整理本章回收的伏笔，禁止从参考清单扩展编号）：\n" + foreshadowJSONInstruction + "\n" + foreshadowOutputContract(payoffThreads(latest.Structure, r.Chapter)) + "\n顶层必须同时包含queries、links、issues、foreshadows四个数组，无内容时使用[]。"
	}
	return system, user, pv
}

func (e *Engine) call(ctx context.Context, p *Project, r *Run, node, key, role string, refs []Citation, task string, validators ...func(string) error) (string, error) {
	validate := func(text string) error {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("模型返回空内容")
		}
		for _, check := range validators {
			if err := check(text); err != nil {
				return err
			}
		}
		return nil
	}
	taskKey := fmt.Sprintf("%d:%d:%s:%s", r.Cycle, r.Chapter, node, key)
	if a, ok := e.Store.SuccessfulAttempt(r.ID, taskKey); ok {
		if err := validate(a.Output); err == nil {
			return a.Output, nil
		} else {
			e.rejectAttempt(r, node, key, err)
		}
	}
	if err := e.node(r, node); err != nil {
		return "", err
	}
	// 每个请求获取最新配置；在途请求持有自己的不可变副本。
	latest, err := e.Store.Project(p.ID)
	if err != nil {
		return "", err
	}
	model := latest.Settings.Models[role]
	system, user, pv := requestText(latest, r, node, refs, task)
	a := Attempt{ID: ID(), RunID: r.ID, TaskKey: taskKey, Node: node, Chapter: r.Chapter, Status: "running", Prompt: system + "\n\n" + user, PromptVersion: pv, Model: model, Inputs: refs, Created: now()}
	if err = e.Store.SaveAttempt(a, r.Epoch); err != nil {
		return "", err
	}
	for retry := 0; retry <= 3; retry++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lastSave := time.Time{}
		a.Output = ""
		a.Reasoning = ""
		res, er := e.Provider.Generate(ctx, ModelRequest{Node: node, Config: model, System: system, User: user, Session: r.ID, FreshConnection: retry > 0}, func(t, reason string) {
			a.Output += t
			a.Reasoning += reason
			if time.Since(lastSave) >= 120*time.Millisecond {
				if e.Store.SaveAttempt(a, r.Epoch) == nil {
					e.Store.Event(r.ID, "stream", map[string]any{"attemptId": a.ID, "node": node, "text": a.Output})
				}
				lastSave = time.Now()
			}
		})
		invalid := false
		if er == nil {
			er = validate(res.Text)
			invalid = er != nil
		}
		if er == nil {
			a.Output = res.Text
			a.Reasoning = res.Reasoning
			a.Usage = res.Usage
			a.Status = "succeeded"
			a.Finished = now()
			if err = e.Store.SaveAttempt(a, r.Epoch); err != nil {
				return "", err
			}
			e.Store.Event(r.ID, "attempt", a)
			e.Store.Event(r.ID, "node", map[string]any{"node": node, "chapter": r.Chapter, "status": "succeeded", "cycle": r.Cycle})
			return res.Text, nil
		}
		a.Output = res.Text
		a.Reasoning = res.Reasoning
		a.Usage = res.Usage
		a.Status = "failed"
		a.Error = er.Error()
		a.Finished = now()
		e.Store.SaveAttempt(a, r.Epoch)
		e.Store.Event(r.ID, "attempt", a)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		delay := time.Duration(1<<retry) * time.Second
		var me *ModelError
		if errors.As(er, &me) {
			if me.RetryAfter > delay {
				delay = me.RetryAfter
			}
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
		if retry == 3 {
			return "", &RetriesExhausted{Cause: er}
		}
		if invalid {
			// 使用同一个重试预算处理传输和验收错误，不嵌套四轮网络重试。
			delay = 0
			_, originalUser, _ := requestText(latest, r, node, refs, task)
			user = originalUser + "\n上次输出未通过格式校验：" + er.Error() + "\n请纠正本次输出格式，重新输出完整结果；保持已有候选和本次任务边界。"
		}
		message := fmt.Sprintf("%s 请求中断，%g 秒后自动重试（%d/3）", node, delay.Seconds(), retry+1)
		e.Store.Event(r.ID, "retry", map[string]any{"node": node, "retry": retry + 1, "maxRetries": 3, "delaySeconds": delay.Seconds(), "message": message, "error": er.Error()})
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
		// 重试有独立记录，保留之前失败的部分输出。
		a.ID = ID()
		a.Status = "running"
		a.Error = ""
		a.Created = now()
		a.Finished = ""
		a.Output = ""
		a.Reasoning = ""
		a.Usage = Usage{}
		a.Prompt = system + "\n\n" + user
		if err = e.Store.SaveAttempt(a, r.Epoch); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("模型重试已耗尽")
}
func relevantStructure(s Structure, ch int) any {
	ts := []Thread{}
	for _, t := range s.Threads {
		hit := t.Plant == ch || t.Payoff == ch
		for _, r := range t.Reminds {
			hit = hit || r == ch
		}
		if hit {
			ts = append(ts, t)
		}
	}
	return map[string]any{"chapterCount": s.Chapters, "arcs": s.Arcs, "threads": ts}
}

type Analysis struct {
	Queries     []string         `json:"queries"`
	Links       []Link           `json:"links"`
	Issues      []Proposal       `json:"issues"`
	Foreshadows []ForeshadowHint `json:"foreshadows,omitempty"`
}

const issueFormat = `核查问题格式：{"chapter":章号,"kind":"outline或draft","problem":"具体问题","instruction":"修订要求","critical":true或false,"reason":"只有必须人工取舍时说明冲突的具体要求","evidence":[{"chapter":章号,"kind":"outline或draft","version":"材料中原样版本号","paragraph":段号,"text":"该段的逐字引用"}]}。仅对已经存在或本批次候选内容提问题；没有原文证据不要杜撰。critical仅用于尚未决定的关键创意取舍，不能因为章节有必留要求就标记；遵守已经确认的决定属于普通修订。`

func (e *Engine) analysis(ctx context.Context, p *Project, r *Run, node, key string, refs []Citation, task string) (Analysis, error) {
	var a Analysis
	_, err := e.call(ctx, p, r, node, key, "check", refs, issueFormat+"\n"+task, func(text string) error {
		var invalid error
		a, invalid = parseAnalysis(text, *p, node)
		return invalid
	})
	return a, err
}

func parseAnalysis(text string, p Project, node string) (Analysis, error) {
	var a Analysis
	err := parseJSON(text, &a)
	if err == nil && ((node == "F5" || node == "H2") && a.Issues == nil) {
		err = fmt.Errorf("核查结果缺少 issues 数组，无法确认是否完成检查")
	}
	if err == nil && node == "F3" && a.Queries == nil {
		err = fmt.Errorf("补查结果缺少 queries 数组")
	}
	if err == nil && node == "E4" && (a.Queries == nil || a.Links == nil || a.Issues == nil) {
		err = fmt.Errorf("正文整理必须包含queries、links、issues数组")
	}
	if err == nil {
		for _, v := range a.Issues {
			if v.Chapter < 1 || v.Chapter > len(p.Chapters) || (v.Kind != "outline" && v.Kind != "draft") || v.Problem == "" || v.Instruction == "" {
				err = fmt.Errorf("核查问题缺少有效章节、内容类型或修订要求")
				break
			}
		}
	}
	return a, err
}

// 结构化解析也是请求验收的一部分；失败后重试必须发出新请求。
func (e *Engine) rejectAttempt(r *Run, node, key string, err error) {
	taskKey := fmt.Sprintf("%d:%d:%s:%s", r.Cycle, r.Chapter, node, key)
	if a, ok := e.Store.SuccessfulAttempt(r.ID, taskKey); ok {
		a.Status = "failed"
		a.Error = err.Error()
		e.Store.SaveAttempt(a, r.Epoch)
		e.Store.Event(r.ID, "attempt", a)
	}
}
func validLinks(ls []Link, n int) []Link {
	out := []Link{}
	for _, l := range ls {
		if l.From >= 1 && l.To >= 1 && l.From <= n && l.To <= n && l.From != l.To && l.Reason != "" {
			out = append(out, l)
		}
	}
	return out
}
func validateEvidence(p Project, r Run, c Citation) bool {
	v := p.Current(c.Chapter, c.Kind)
	for _, b := range r.Data.Batch {
		if b.Chapter == c.Chapter && b.Kind == c.Kind {
			candidate := Version{ID: "candidate:" + fingerprint(b.Text), Chapter: b.Chapter, Kind: b.Kind, Text: b.Text}
			if c.Version == candidate.ID {
				v = &candidate
			}
		}
	}
	if v == nil || v.ID != c.Version || c.Paragraph < 1 {
		return false
	}
	ps := paragraphs(v.Text)
	return c.Paragraph <= len(ps) && strings.TrimSpace(c.Text) != "" && strings.Contains(ps[c.Paragraph-1], c.Text)
}
func (e *Engine) prepareProposals(p Project, r Run, issues []Proposal, allHuman bool) []Proposal {
	out := []Proposal{}
	for _, v := range issues {
		if v.Chapter < 1 || v.Chapter > len(p.Chapters) || (v.Kind != "outline" && v.Kind != "draft") || v.Problem == "" || v.Instruction == "" {
			continue
		}
		v.ID = ID()
		v.Decision = "pending"
		valid := len(v.Evidence) > 0
		for _, c := range v.Evidence {
			valid = valid && validateEvidence(p, r, c)
		}
		if !valid {
			// 无法核验的模型意见不能转嫁成人工待办，也不能自动改写原文。
			continue
		}
		duplicate := false
		for _, old := range append(append([]Proposal{}, r.Data.Proposals...), out...) {
			if sameIssue(old, v) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if !v.Critical && !allHuman {
			v.Decision = "accepted"
		}
		out = append(out, v)
	}
	return out
}
