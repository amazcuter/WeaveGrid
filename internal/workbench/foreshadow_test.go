package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestForeshadowValidationReportsExactProblem(t *testing.T) {
	threads := []Thread{{ID: "T054", Plant: 10, Payoff: 50}, {ID: "T055", Plant: 17, Payoff: 50}}
	for _, tc := range []struct {
		name, want string
		hints      []ForeshadowHint
	}{
		{"unknown", "T099不在本次允许输出", []ForeshadowHint{{ThreadID: "T099", Text: "提示"}}},
		{"duplicate", "编号重复：T054", []ForeshadowHint{{ThreadID: "T054", Text: "提示"}, {ThreadID: "T054", Text: "另一提示"}}},
		{"empty", "T054的text为空", []ForeshadowHint{{ThreadID: "T054", Text: " \n "}}},
		{"missing", "缺少编号：T055", []ForeshadowHint{{ThreadID: "T054", Text: "提示"}}},
		{"empty ID", "第1项缺少非空threadId", []ForeshadowHint{{Text: "提示"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeForeshadows(tc.hints, threads); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
	long := strings.Repeat("完整措辞", 20)
	hints, err := normalizeForeshadows([]ForeshadowHint{{ThreadID: "T055", Text: long}, {ThreadID: " T054 ", Text: " 上句\r\n下句\r尾句 "}}, threads)
	if err != nil || len(hints) != 2 || hints[0].ThreadID != "T054" || hints[0].Text != "上句 下句 尾句" || hints[0].SourceChapter != 50 || hints[1].Text != long {
		t.Fatalf("normalization lost content or source: %+v %v", hints, err)
	}
}

func chapter49Fixture(t *testing.T, s *Store) (Project, Run) {
	t.Helper()
	p := NewProject("伏笔格式验收", Structure{Chapters: 50, Threads: []Thread{{ID: "T054", Plant: 10, Payoff: 50, Reminds: []int{21, 37}}}})
	if err := ValidateStructure(&p.Structure); err != nil {
		t.Fatal(err)
	}
	adopt(&p, Change{Chapter: 50, Kind: "outline", Text: "## 事件要点\n\n后章揭晓。\n\n## 伏笔提示\n\n- T054（第10章 → 第50章；提醒第21、37章）：旧物留下未解疑问。"}, "fixture")
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Stage: "D", Node: "D4", NextNode: "D4", Chapter: 49, Status: "running", Epoch: 1, Data: freshData()}
	r.Data.Generated = "## 事件要点\n\n本章事件保持原样。\n\n## 伏笔提示\n\n- T054（第10章 → 第50章）：误抄的后章提示。"
	r.Data.Batch = []Change{{Chapter: 49, Kind: "outline", Text: r.Data.Generated}}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	return p, r
}

func TestForeshadowChapter49RejectsChapter50IDsAndRetriesOnlyD4(t *testing.T) {
	s, e, f, _ := setup(t)
	p, r := chapter49Fixture(t, s)
	original := r.Data.Generated
	calls := 0
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		calls++
		if req.Node != "D4" {
			t.Fatalf("format retry rewrote outline at %s", req.Node)
		}
		if !strings.Contains(req.User, "T054") || !strings.Contains(req.User, "本次允许输出的伏笔编号：[]") || !strings.Contains(req.User, "必须返回\"foreshadows\":[]") || !strings.Contains(req.User, "参考材料，不是本次输出清单") {
			t.Fatal("missing reference/whitelist distinction")
		}
		if strings.LastIndex(req.User, "本次允许输出的伏笔编号：[]") < strings.Index(req.User, "伏笔提示与两端状态") {
			t.Fatal("output contract must follow reference context")
		}
		if calls == 1 {
			return ModelResult{Text: `{"queries":[],"links":[],"issues":[],"foreshadows":[{"threadId":"T054","text":"错误照抄后章提示"}]}`}, nil, true
		}
		if !strings.Contains(req.User, "上次输出未通过格式校验") || !strings.Contains(req.User, "T054不在本次允许输出") {
			t.Fatal("retry did not include actionable correction")
		}
		return ModelResult{Text: `{"queries":["新线索"],"links":[],"issues":[],"foreshadows":[]}`}, nil, true
	}
	// 旧整理请求即使曾标为成功，也不能绕过新版的编号清单和格式校验。
	old := Attempt{ID: ID(), RunID: r.ID, Node: "D4", Chapter: 49, TaskKey: fmt.Sprintf("%d:49:D4:extract-foreshadows:%s", r.Cycle, fingerprint(original)), Status: "succeeded", Output: `{"foreshadows":[{"threadId":"T054","text":"旧缓存"}]}`}
	if err := s.SaveAttempt(old, r.Epoch); err != nil {
		t.Fatal(err)
	}
	next := "D5"
	if err := e.creationNode(context.Background(), &p, &r, 4, &next); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || r.Data.Generated != original || r.Data.Batch[0].Text != original || len(r.Data.Analysis.Foreshadows) != 0 || r.Data.Analysis.Queries[0] != "新线索" {
		t.Fatal("retry lost the candidate or analysis")
	}
	if err := e.creationNode(context.Background(), &p, &r, 5, &next); err != nil {
		t.Fatal(err)
	}
	if r.Data.Batch[0].Text != "## 事件要点\n\n本章事件保持原样。\n\n## 伏笔提示\n\n本章无预设伏笔。" {
		t.Fatal("formatting failed to remove other chapter's hints", r.Data.Batch[0].Text)
	}
	attempts, _ := s.Attempts(r.ID)
	if len(attempts) != 3 || attempts[1].Status != "failed" || attempts[2].Status != "succeeded" || attempts[1].ID == attempts[2].ID {
		t.Fatal("format failures must retain separate retry records", attempts)
	}
	saved, _ := s.Project(p.ID)
	if saved.Current(49, "outline") != nil || saved.Current(50, "outline").Text != p.Current(50, "outline").Text {
		t.Fatal("format correction changed adopted chapters")
	}
}

func TestForeshadowJSONRetryLimitPreservesCandidate(t *testing.T) {
	s, e, f, _ := setup(t)
	p, r := chapter49Fixture(t, s)
	original := r.Data.Generated
	answers := []string{`{"foreshadows":`, `{"queries":[],"links":[],"issues":[]}`, `{"queries":[],"links":[],"issues":[],"foreshadows":null}`, `{"queries":null,"links":[],"issues":[],"foreshadows":[]}`}
	calls := 0
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		answer := answers[calls]
		calls++
		return ModelResult{Text: answer}, nil, true
	}
	next := "D5"
	err := e.creationNode(context.Background(), &p, &r, 4, &next)
	if err == nil || !strings.Contains(err.Error(), "已自动重试 3 次") || calls != 4 || r.NextNode != "D4" || r.Data.Analysis != nil || r.Data.Generated != original || r.Data.Batch[0].Text != original {
		t.Fatalf("bad retry limit or candidate changed: calls=%d err=%v", calls, err)
	}
	attempts, _ := s.Attempts(r.ID)
	if len(attempts) != 4 {
		t.Fatal("wrong attempt count", len(attempts))
	}
	for _, a := range attempts {
		if a.Status != "failed" {
			t.Fatal("invalid JSON remained successful")
		}
	}
	events, _ := s.Events(r.ID, 0)
	retries := 0
	for _, event := range events {
		if event.Type == "retry" {
			retries++
		}
	}
	if retries != 3 {
		t.Fatal("missing retry progress", retries)
	}
}

func TestForeshadowBackfillCorrectsFormatWithoutRewriting(t *testing.T) {
	for _, stage := range []string{"D", "E"} {
		t.Run(stage, func(t *testing.T) {
			s, e, f, p := setup(t)
			p, err := s.UpdateProject(p.ID, 0, func(p *Project) error {
				adopt(p, Change{Chapter: 3, Kind: "outline", Text: "已有回收章大纲，尚无提示区。"}, "fixture")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			mode := "backward"
			if stage == "E" {
				mode = "forward"
			}
			r := Run{ID: ID(), ProjectID: p.ID, Stage: stage, Chapter: 1, Mode: mode, Status: "running", Epoch: 1, Data: freshData()}
			if err := s.CreateRun(r); err != nil {
				t.Fatal(err)
			}
			calls := 0
			f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
				calls++
				if req.Node != stage+"1" {
					t.Fatal("unexpected generation node", req.Node)
				}
				if calls == 1 {
					return ModelResult{Text: `{"foreshadows":[{"threadId":"T001","text":""}]}`}, nil, true
				}
				if !strings.Contains(req.User, "T001的text为空") {
					t.Fatal("missing precise validation feedback")
				}
				return ModelResult{Text: `{"foreshadows":[{"threadId":"T001","text":"旧物留下疑问。"}]}`}, nil, true
			}
			if err := e.prepareForeshadows(context.Background(), &p, &r); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(r.Data.Foreshadows) != 1 || r.Data.Foreshadows[0].SourceVersion != p.ActiveID(3, "outline") {
				t.Fatal("backfill lost source version")
			}
			if err := e.prepareForeshadows(context.Background(), &p, &r); err != nil || calls != 2 {
				t.Fatal("valid backfill was regenerated", err)
			}
			saved, _ := s.Project(p.ID)
			if saved.Current(3, "outline").Text != "已有回收章大纲，尚无提示区。" {
				t.Fatal("backfill rewrote the outline")
			}
		})
	}
}

func TestForeshadowRetryHonorsCancellation(t *testing.T) {
	s, e, f, _ := setup(t)
	p, r := chapter49Fixture(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		calls++
		cancel()
		return ModelResult{Text: `{"foreshadows":null}`}, nil, true
	}
	next := "D5"
	if err := e.creationNode(ctx, &p, &r, 4, &next); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("retry ignored cancellation: calls=%d err=%v", calls, err)
	}
}

func TestForeshadowContractsAlsoApplyToCustomPrompts(t *testing.T) {
	s, _, _, _ := setup(t)
	p, r := chapter49Fixture(t, s)
	p.Prompts["D2"] = []PromptVersion{{ID: "custom-D2", Text: "保留我的写作风格。"}}
	p.Prompts["D4"] = []PromptVersion{{ID: "custom-D4", Text: "旧的自定义整理指令。"}}
	refs := materials(p, 49, "backward", nil)
	system, user, _ := requestText(p, &r, "D2", refs, creationTask("backward"))
	if !strings.Contains(system, "保留我的写作风格") || !strings.Contains(user, "## 事件要点") || !strings.Contains(user, "第49章没有预设伏笔") {
		t.Fatal("outline contract missing with custom prompt")
	}
	system, user, _ = requestText(p, &r, "D4", refs, outlineAnalysisTask(p, r))
	if !strings.Contains(system, "旧的自定义整理指令") || !strings.Contains(user, "本次允许输出的伏笔编号：[]") || !strings.Contains(user, "foreshadows必须恰好0项") {
		t.Fatal("analysis contract missing with custom prompt")
	}
	r.Chapter = 50
	_, user, _ = requestText(p, &r, "D2", refs, creationTask("backward"))
	if !strings.Contains(user, "- T054（第10章 → 第50章；提醒第21、37章）：") {
		t.Fatal("actual chapter IDs and locations absent from outline contract")
	}
	_, user, _ = requestText(p, &r, "D4", refs, outlineAnalysisTask(p, r))
	if !strings.Contains(user, `本次允许输出的伏笔编号：["T054"]；foreshadows必须恰好1项`) {
		t.Fatal("payoff chapter did not get its own output whitelist")
	}
}

func TestForeshadowFailedD4ResumesAtSameStep(t *testing.T) {
	s, e, f, _ := setup(t)
	_, r := chapter49Fixture(t, s)
	original := r.Data.Generated
	if _, err := s.UpdateRun(r.ID, 0, func(saved *Run) error {
		saved.Status = "failed"
		saved.Error = "伏笔提示编号无效、重复或内容为空：T054"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "D4" {
			return ModelResult{Text: `{"queries":[],"links":[],"issues":[],"foreshadows":[]}`}, nil, true
		}
		return ModelResult{}, nil, false
	}
	if _, err := e.Control(r.ID, "step", 0); err != nil {
		t.Fatal(err)
	}
	saved := awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "paused" })
	if saved.NextNode != "D5" || saved.Data.Generated != original || saved.Data.Batch[0].Text != original || saved.Data.Analysis == nil {
		t.Fatal("resuming the old error did not preserve the candidate and cursor")
	}
	attempts, _ := s.Attempts(r.ID)
	if len(attempts) != 1 || attempts[0].Node != "D4" || attempts[0].Status != "succeeded" {
		t.Fatal("resuming D4 unexpectedly reran other generation steps")
	}
}
