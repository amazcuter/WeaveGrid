package workbench

import (
	"strings"
	"testing"
	"time"
)

// 长流程测试显式扮演用户调用提交接口，生产状态机始终保留人工关卡。
func submitFixtureCreativity(t *testing.T, s *Store, e *Engine, r Run) Run {
	t.Helper()
	p, err := s.Project(r.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	ideas := chapterIdeas(p, r.Chapter)
	if len(ideas) == 0 {
		ideas = []Idea{{Chapter: r.Chapter, Text: "由测试用户填写：保留主角的选择。", Active: true}}
	}
	status, body := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", map[string]any{"id": r.Data.CreativeInput.ID, "ideas": ideas, "projectRevision": p.Revision}, r.Revision)
	if status != 200 {
		t.Fatalf("人工提交失败 %d: %s", status, body)
	}
	r, err = s.Run(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func awaitWithHumanInputs(t *testing.T, s *Store, e *Engine, id string, pred func(Run) bool) Run {
	t.Helper()
	return awaitRun(t, s, id, func(r Run) bool {
		if r.Status == "waiting_input" && r.Stage == "C" {
			submitFixtureCreativity(t, s, e, r)
			if _, err := e.Control(id, "resume", 0); err != nil {
				t.Fatal(err)
			}
			return false
		}
		return pred(r)
	})
}

func TestCreativeInputRequiresHumanAndPreservesExactOutput(t *testing.T) {
	s, e, f, p := setup(t)
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Ideas = []Idea{{ID: ID(), Chapter: 1, Text: "其他章节的创意", Active: true}, {ID: ID(), Chapter: 0, Text: "预存的全书想法", Active: true}}
		return nil
	})
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.NextNode != "C2" || r.Data.CreativeInput == nil || len(r.Data.ContextIdeas) != 0 {
		t.Fatalf("没有等待人工：%+v", r)
	}
	// 继续、单步和预存创意均不得绕过输入。
	for _, action := range []string{"resume", "step"} {
		if _, err = e.Control(r.ID, action, r.Revision); err != nil {
			t.Fatal(err)
		}
		r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
		if r.NextNode != "C2" {
			t.Fatal("绕过人工输入", r.NextNode)
		}
	}
	original := "  女主亲手烧掉信。\n\n不要解释动机，也不要替我补写创意。  "
	body := map[string]any{"id": r.Data.CreativeInput.ID, "projectRevision": p.Revision, "ideas": []Idea{{Chapter: 3, Text: original, Required: true, Active: true}}}
	status, out := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", body, r.Revision)
	if status != 200 {
		t.Fatal(status, string(out))
	}
	r, _ = s.Run(r.ID)
	p, _ = s.Project(p.ID)
	if len(r.CreativeInputs) != 1 || r.CreativeInputs[0].Ideas[0].Text != original || r.Data.ContextIdeas[0].Text != original {
		t.Fatal("人工输出被改写", r.CreativeInputs)
	}
	if len(p.Ideas) != 2 || p.Ideas[0].Text != "其他章节的创意" {
		t.Fatal("影响其他章节", p.Ideas)
	}
	if status, _ = requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", body, r.Revision); status != 409 {
		t.Fatal("接受重复或过期提交", status)
	}
	checkpoint, err := s.CreateCheckpoint(p.ID, "人工创意已提交", "", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Chapter != 2 || r.NextNode != "C2" || len(r.CreativeInputs) != 1 || r.Data.CreativeInput.SubmittedAt != "" {
		t.Fatal("下一章未重新询问或历史丢失", r)
	}
	f.mu.Lock()
	calls := append([]ModelRequest(nil), f.calls...)
	f.mu.Unlock()
	found := false
	for _, c := range calls {
		if strings.HasPrefix(c.Node, "C") || strings.HasPrefix(c.Node, "I") {
			t.Fatal("人工输入或已移除文风调用模型", c.Node)
		}
		if c.Node == "D2" && strings.Contains(c.User, `女主亲手烧掉信。\n\n不要解释动机`) {
			found = true
		}
	}
	if !found {
		t.Fatal("创作请求未收到原样人工创意")
	}
	p, _ = s.Project(p.ID)
	_, restored, err := s.RestoreCheckpoint(p.ID, checkpoint.ID, p.Revision)
	if err != nil || restored == nil || restored.Data.CreativeInput.SubmittedAt == "" || len(restored.CreativeInputs) != 1 {
		t.Fatal("回退丢失人工交付", err)
	}
}

func TestCreativeInputEmptyAndStaleProjectRejected(t *testing.T) {
	s, e, _, p := setup(t)
	r, _ := e.Start(p.ID, "backward")
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	req := map[string]any{"id": r.Data.CreativeInput.ID, "projectRevision": p.Revision, "ideas": []Idea{}}
	code, _ := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", req, r.Revision)
	if code == 200 {
		t.Fatal("空输入被自动确认")
	}
	req["emptyConfirmed"] = true
	p, _ = s.UpdateProject(p.ID, p.Revision, func(p *Project) error { p.Name = "更新的作品名"; return nil })
	code, _ = requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", req, r.Revision)
	if code != 409 {
		t.Fatal("旧项目版本覆盖", code)
	}
	req["projectRevision"] = p.Revision
	code, out := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/creative-input", req, r.Revision)
	if code != 200 {
		t.Fatal(code, string(out))
	}
	saved, _ := s.Run(r.ID)
	if !saved.Data.CreativeInput.EmptyConfirmed || len(saved.Data.ContextIdeas) != 0 {
		t.Fatal("没有记录人工空创意决定")
	}
}

func TestForwardSkipsRemovedStyleWorkflow(t *testing.T) {
	s, e, f, p := setup(t)
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Settings.StyleVerified = false
		p.Settings.StyleSamples = "旧文风内容不得再注入"
		p.Settings.EvalScenes = ""
		for ch := 1; ch <= 3; ch++ {
			adopt(p, Change{Chapter: ch, Kind: "outline", Text: "人工大纲"}, "manual")
		}
		return nil
	})
	for _, n := range Workflow {
		if n.ID == "I" {
			t.Fatal("仍发布文风子流")
		}
	}
	r, err := e.Start(p.ID, "forward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.NextNode != "C2" {
		t.Fatal("正文仍被文风准备阻挡", r.NextNode)
	}
	submitFixtureCreativity(t, s, e, r)
	e.Control(r.ID, "resume", 0)
	awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.calls {
		if strings.HasPrefix(req.Node, "I") || strings.Contains(req.User, "旧文风内容不得再注入") {
			t.Fatal("仍执行文风准备", req.Node)
		}
	}
}

func TestLegacyCreativeCursorCannotSkipInput(t *testing.T) {
	s, e, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Stage: "C", Node: "C4", NextNode: "C4", Status: "paused", Epoch: 1, Revision: 1, Chapter: 3, Created: now(), Data: freshData()}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.NextNode != "C2" {
		t.Fatal("旧游标绕过输入")
	}
	time.Sleep(20 * time.Millisecond)
	p, _ = s.Project(p.ID)
	if len(p.Versions) != 0 {
		t.Fatal("无人确认却生成内容")
	}
}

func TestRetiredStyleCursorResumesAtScheduler(t *testing.T) {
	s, e, f, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Stage: "I", Node: "I5", NextNode: "I5", Status: "paused", Epoch: 1, Revision: 1, Chapter: 3, Created: now(), Data: freshData()}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Control(r.ID, "step", 0); err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "paused" })
	if r.Node != "J1" || r.NextNode != "J2" {
		t.Fatal("旧文风游标没有迁移到调度", r.Node, r.NextNode)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 {
		t.Fatal("恢复旧文风检查点调用了模型")
	}
}
