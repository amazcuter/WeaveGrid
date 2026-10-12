package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func orchestrationFixture(req ModelRequest) (ModelResult, error, bool) {
	if req.Node == "O2" {
		return ModelResult{Text: `{"summary":"保留用户的仙侠主题与章节差异；主角已留下线索，后续按因果推进。"}`}, nil, true
	}
	if req.Node != "O1" {
		return ModelResult{}, nil, false
	}
	var input struct {
		Gate      string     `json:"gate"`
		Proposals []Proposal `json:"proposals"`
	}
	if err := json.NewDecoder(strings.NewReader(req.User)).Decode(&input); err != nil {
		return ModelResult{}, err, true
	}
	decisions := []OrchestratorDecision{}
	for _, prop := range input.Proposals {
		decision := "accepted"
		if strings.Contains(prop.Problem, "无证据") {
			decision = "rejected"
		}
		decisions = append(decisions, OrchestratorDecision{ID: prop.ID, Decision: decision, Reason: "依据本次原文与用户要求判断"})
	}
	return ModelResult{Text: asJSON(map[string]any{"reason": "让本章承担新的因果变化", "memory": "保留仙侠主题和既定选择，后续章不得重复本章事件。", "answer": "创作选择：主角名为江砚，未知资料不作为事实。", "creativeDirection": "通过行动呈现代价，不重复后章揭示。", "decisions": decisions})}, nil, true
}

func enableOrchestration(t *testing.T, s *Store, p Project) Project {
	t.Helper()
	p, err := s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Settings.OrchestratorEnabled = true
		p.Settings.OrchestratorGuidance = "仙侠，章节各有区别"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOrchestratorCompletesBothModesAndInheritsMemory(t *testing.T) {
	s, e, f, p := setup(t)
	p = enableOrchestration(t, s, p)
	var asked atomic.Bool
	var sawMemory atomic.Bool
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "B7" && asked.CompareAndSwap(false, true) {
			return ModelResult{Text: `{"queries":[],"inputs":["主角用什么名字？"]}`}, nil, true
		}
		if req.Node == "O1" {
			if req.Config.Model != "kimi-k3" {
				t.Error("统筹未使用独立Kimi角色")
			}
			if strings.Contains(req.User, `"mode":"forward"`) && strings.Contains(req.User, "保留仙侠主题") {
				sawMemory.Store(true)
			}
		}
		return orchestrationFixture(req)
	}
	for _, mode := range []string{"backward", "forward"} {
		r, err := e.Start(p.ID, mode)
		if err != nil {
			t.Fatal(err)
		}
		r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "completed" })
		if len(r.CreativeInputs) != 3 || r.OrchestratorGate != "" {
			t.Fatalf("未完整接管C2及最终审核：%+v", r)
		}
		for _, input := range r.CreativeInputs {
			if input.Source != "orchestrator" || input.SubmittedAt == "" {
				t.Fatal("统筹输入缺少来源或提交记录")
			}
		}
	}
	p, _ = s.Project(p.ID)
	if !sawMemory.Load() || p.OrchestratorMemory.Sequence < 9 || len(p.References) != 1 {
		t.Fatal("跨运行记忆或B8补充丢失", p.OrchestratorMemory.Sequence)
	}
	for _, ch := range p.Chapters {
		if ch.Outline == "" || ch.Draft == "" {
			t.Fatal("未写完", ch.Number)
		}
	}
}

func orchestratorGateFixture(t *testing.T, s *Store, p Project, gate string) Run {
	t.Helper()
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "running", Stage: gate[:1], Node: gate, NextNode: gate, OrchestratorGate: gate, Chapter: 1, Epoch: 1, Revision: 1, Data: freshData()}
	if gate == "C2" {
		r.Data.CreativeInput = &CreativeInput{ID: ID(), Chapter: 1, Ideas: []Idea{{ID: ID(), Chapter: 0, Required: true, Active: true, Text: "用户必留：不伤害同伴"}}}
	}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOrchestratorReviewProcessesBatchesAndPreservesChoices(t *testing.T) {
	s, e, f, p := setup(t)
	p = enableOrchestration(t, s, p)
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		return orchestrationFixture(r)
	}
	r := orchestratorGateFixture(t, s, p, "H5")
	r, _ = s.UpdateRun(r.ID, 0, func(r *Run) error {
		for i := 0; i < 12; i++ {
			problem := "因果缺失"
			if i%2 == 0 {
				problem = "无证据的审美建议"
			}
			r.Data.Proposals = append(r.Data.Proposals, Proposal{ID: ID(), Chapter: 1, Kind: "outline", Problem: problem, Instruction: "补足衔接", Decision: "pending"})
		}
		return nil
	})
	if err := e.orchestrate(context.Background(), &p, &r); err != nil {
		t.Fatal(err)
	}
	if r.Data.ReviewApproved || len(r.HumanDecisions) != 8 {
		t.Fatal("分批时提前批准或未记录决定")
	}
	if err := e.step(context.Background(), &p, &r); err != nil {
		t.Fatal(err)
	}
	if err := e.orchestrate(context.Background(), &p, &r); err != nil {
		t.Fatal(err)
	}
	if !r.Data.ReviewApproved || len(r.HumanDecisions) != 12 {
		t.Fatal("剩余审核未完成")
	}
	for i, d := range r.Data.Proposals {
		if d.Source != "orchestrator" || (i%2 == 0 && d.Decision != "rejected") || (i%2 == 1 && d.Decision != "accepted") {
			t.Fatal("意见取舍丢失", d)
		}
	}
}

func TestOrchestratorCompactsAtomicallyAndPreservesRecentTail(t *testing.T) {
	s, e, f, p := setup(t)
	p = enableOrchestration(t, s, p)
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		for i := 0; i < 10; i++ {
			p.OrchestratorMemory.Recent = append(p.OrchestratorMemory.Recent, OrchestratorEntry{ID: ID(), Chapter: i + 1, Decision: strings.Repeat("历史事件及用户约束。", 400)})
		}
		p.OrchestratorMemory.Sequence = 10
		return nil
	})
	last := p.OrchestratorMemory.Recent[9].ID
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		return orchestrationFixture(r)
	}
	r := orchestratorGateFixture(t, s, p, "C2")
	if err := e.orchestrate(context.Background(), &p, &r); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.Project(p.ID)
	if saved.OrchestratorMemory.Compactions < 1 || saved.OrchestratorMemory.Sequence != 11 || saved.OrchestratorMemory.Summary == "" {
		t.Fatal("记忆压缩未提交")
	}
	found := false
	for _, entry := range saved.OrchestratorMemory.Recent {
		found = found || entry.ID == last
	}
	if !found {
		t.Fatal("近期尾部被压缩掉")
	}
	if r.Data.CreativeInput.Ideas[0].Text != "用户必留：不伤害同伴" || !r.Data.CreativeInput.Ideas[0].Required {
		t.Fatal("统筹改动了用户要求")
	}
	events, _ := s.Events(r.ID, 0)
	journal := false
	for _, event := range events {
		journal = journal || event.Type == "orchestrator"
	}
	if !journal {
		t.Fatal("原始决定未进入持久化日志")
	}
}

func TestOrchestratorInvalidCompressionDoesNotReplaceMemory(t *testing.T) {
	s, e, f, p := setup(t)
	p = enableOrchestration(t, s, p)
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.OrchestratorMemory.Summary = "既有事实"
		for i := 0; i < 10; i++ {
			p.OrchestratorMemory.Recent = append(p.OrchestratorMemory.Recent, OrchestratorEntry{ID: ID(), Decision: strings.Repeat("完整历史", 1000)})
		}
		return nil
	})
	before := asJSON(p.OrchestratorMemory)
	var calls atomic.Int32
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if r.Node == "O2" {
			calls.Add(1)
			return ModelResult{Text: `{"summary":""}`}, nil, true
		}
		return orchestrationFixture(r)
	}
	r := orchestratorGateFixture(t, s, p, "C2")
	err := e.orchestrate(context.Background(), &p, &r)
	saved, _ := s.Project(p.ID)
	if err == nil || calls.Load() != 4 || asJSON(saved.OrchestratorMemory) != before {
		t.Fatal("失败压缩覆盖了记忆或重试无上限", calls.Load(), err)
	}
}

func TestOrchestratorMemoryAndGateSurviveRestartAndCheckpoint(t *testing.T) {
	path := t.TempDir() + "/orchestrator.db"
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := NewProject("统筹记忆重启", Structure{Chapters: 3})
	p.Settings.OrchestratorEnabled = true
	p.Settings.OrchestratorGuidance = "保留用户的必留要求"
	p.OrchestratorMemory = OrchestratorMemory{Summary: "已定事实", Sequence: 5, Compactions: 1, Recent: []OrchestratorEntry{{ID: ID(), Gate: "C2", Decision: "近期未压缩决定"}}}
	if err = s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	r := orchestratorGateFixture(t, s, p, "C2")
	if _, err = s.UpdateRun(r.ID, 0, func(r *Run) error { r.Status = "paused"; return nil }); err != nil {
		t.Fatal(err)
	}
	cp, err := s.CreateCheckpoint(p.ID, "统筹等待继续", "", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Project(p.ID)
	if err != nil || asJSON(saved.OrchestratorMemory) != asJSON(p.OrchestratorMemory) || saved.Settings.OrchestratorGuidance != p.Settings.OrchestratorGuidance {
		t.Fatal("重启后记忆或用户要求丢失", err)
	}
	if _, err = s.UpdateProject(p.ID, 0, func(p *Project) error { p.OrchestratorMemory.Summary = "检查点之后的改变"; return nil }); err != nil {
		t.Fatal(err)
	}
	saved, _ = s.Project(p.ID)
	restored, resumed, err := s.RestoreCheckpoint(p.ID, cp.ID, saved.Revision)
	if err != nil || resumed == nil {
		t.Fatal("恢复失败", err)
	}
	if asJSON(restored.OrchestratorMemory) != asJSON(p.OrchestratorMemory) || resumed.OrchestratorGate != "C2" || resumed.Status != "paused" {
		t.Fatal("检查点未同时恢复记忆和统筹关卡")
	}
	f := &fixtureModel{}
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "O1" && (!strings.Contains(req.User, "已定事实") || !strings.Contains(req.User, "近期未压缩决定")) {
			t.Error("恢复后的请求未继承记忆")
		}
		return orchestrationFixture(req)
	}
	e := NewEngine(s, f)
	defer e.Close()
	*resumed, err = s.UpdateRun(resumed.ID, 0, func(r *Run) error { r.Status = "running"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = e.orchestrate(context.Background(), &restored, resumed); err != nil {
		t.Fatal(err)
	}
	if restored.OrchestratorMemory.Sequence != 6 || resumed.OrchestratorGate != "" {
		t.Fatal("恢复后未正常续接统筹")
	}
}

func TestOrchestratorPauseAndDisableDiscardInflightDecision(t *testing.T) {
	for _, action := range []string{"pause", "disable"} {
		t.Run(action, func(t *testing.T) {
			s, e, f, p := setup(t)
			p = enableOrchestration(t, s, p)
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
				if r.Node == "O1" {
					entered <- struct{}{}
					select {
					case <-ctx.Done():
						return ModelResult{}, ctx.Err(), true
					case <-release:
					}
				}
				return orchestrationFixture(r)
			}
			r, err := e.Start(p.ID, "backward")
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			if action == "pause" {
				if _, err = e.Control(r.ID, "pause", 0); err != nil {
					t.Fatal(err)
				}
			} else {
				_, err = s.UpdateProject(p.ID, 0, func(p *Project) error { p.Settings.OrchestratorEnabled = false; return nil })
				if err != nil {
					t.Fatal(err)
				}
				close(release)
				awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
			}
			p, _ = s.Project(p.ID)
			saved, _ := s.Run(r.ID)
			if p.OrchestratorMemory.Sequence != 0 || len(saved.CreativeInputs) != 0 {
				t.Fatal("迟到的统筹输出覆盖暂停/关闭状态")
			}
		})
	}
}

func TestOrchestratorEnablingTakesOverWaitingGate(t *testing.T) {
	s, e, f, p := setup(t)
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		return orchestrationFixture(r)
	}
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	p.Settings.OrchestratorEnabled = true
	req := httptest.NewRequest("PUT", "/api/projects/"+p.ID, strings.NewReader(asJSON(map[string]any{"settings": p.Settings})))
	req.Header.Set("Idempotency-Key", ID())
	w := httptest.NewRecorder()
	NewServer(s, e).Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "completed" })
}

func TestUnifiedRetryBudgetForMixedFailures(t *testing.T) {
	s, e, f, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Status: "running", Chapter: 1, Epoch: 1, Data: freshData()}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		switch calls.Add(1) {
		case 1:
			return ModelResult{}, io.EOF, true
		case 2:
			return ModelResult{Text: `broken`}, nil, true
		default:
			return ModelResult{Text: `{"wrong":[]}`}, nil, true
		}
	}
	_, err := e.analysis(context.Background(), &p, &r, "H2", "mixed", nil, "检查")
	var exhausted *RetriesExhausted
	if !errors.As(err, &exhausted) || calls.Load() != 4 {
		t.Fatal("网络和格式重试预算发生叠加", calls.Load(), err)
	}
	attempts, _ := s.Attempts(r.ID)
	if len(attempts) != 4 {
		t.Fatal("没有逐次保存失败尝试")
	}
}

func TestKimiDefaultsAndOneTimeMigration(t *testing.T) {
	s := defaults()
	for _, role := range []string{"outline", "draft", "orchestrator"} {
		if s.Models[role].Model != "kimi-k3" {
			t.Fatal(role)
		}
	}
	s.ModelDefaultsVersion = 0
	delete(s.Models, "orchestrator")
	m := s.Models["outline"]
	m.Model = "deepseek-v4.1-flash"
	s.Models["outline"] = m
	normalizeSettings(&s)
	if s.Models["outline"].Model != "kimi-k3" || s.Models["orchestrator"].Model != "kimi-k3" {
		t.Fatal("旧项目没有迁移")
	}
	m.Model = "custom-user-model"
	s.Models["outline"] = m
	normalizeSettings(&s)
	if s.Models["outline"].Model != m.Model {
		t.Fatal("重复迁移覆盖用户配置")
	}
}
