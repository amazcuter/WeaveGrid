package workbench

import (
	"context"
	"strings"
	"testing"
)

// reviewFixture 驱动运行到 F 阶段的等待人工输入，产生一条待处理意见。
func reviewFixture(t *testing.T) (*Store, *Engine, Run) {
	t.Helper()
	s, e, f, p := setup(t)
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "F5" && strings.Contains(req.User, "只核查目标第 3 章") {
			proposal := Proposal{Chapter: 3, Kind: "outline", Problem: "人物认知需要人工确认", Instruction: "让人物主动告知同伴", Critical: true}
			// 核查目标可能尚未生成正文，证据可缺省，撤回逻辑不依赖证据。
			if latest, err := s.Project(p.ID); err == nil {
				if v := latest.Current(3, "outline"); v != nil {
					if c := citations(*v, true); len(c) > 0 {
						proposal.Evidence = c[:1]
					}
				}
			}
			return ModelResult{Text: asJSON(Analysis{Issues: []Proposal{proposal}})}, nil, true
		}
		return ModelResult{}, nil, false
	}
	run, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r := awaitWithHumanInputs(t, s, e, run.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if len(r.Data.Proposals) == 0 {
		t.Fatal("没有产生待处理意见")
	}
	return s, e, r
}

// 已提交但未执行的决定可以撤回为待处理，并且不再作为既有决定约束生成。
func TestWithdrawPendingDecision(t *testing.T) {
	s, e, r := reviewFixture(t)
	id := r.Data.Proposals[0].ID

	code, body := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/decisions", map[string]any{
		"decisions": []map[string]any{{"id": id, "decision": "rejected", "instruction": "不要改"}},
	}, r.Revision)
	if code != 200 {
		t.Fatalf("提交拒绝失败 code=%d body=%s", code, body)
	}
	cur, _ := s.Run(r.ID)
	if cur.Data.Proposals[0].Decision != "rejected" {
		t.Fatalf("决定未记录: %s", cur.Data.Proposals[0].Decision)
	}
	if len(humanDecisions(cur)) == 0 {
		t.Fatal("已提交的决定应进入人工决定记录")
	}

	code, body = requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/decisions", map[string]any{
		"decisions": []map[string]any{{"id": id, "decision": "pending"}},
	}, cur.Revision)
	if code != 200 {
		t.Fatalf("撤回失败 code=%d body=%s", code, body)
	}
	after, _ := s.Run(r.ID)
	if after.Data.Proposals[0].Decision != "pending" {
		t.Fatalf("撤回后应回到待处理，实际 %s", after.Data.Proposals[0].Decision)
	}
	// 撤回必须同时清掉审计记录，否则已撤销的选择仍会约束后续生成。
	for _, d := range humanDecisions(after) {
		if d.ID == id {
			t.Fatal("撤回后仍在人工决定记录中")
		}
	}
	for _, d := range decisionContext(after) {
		if d["decision"] == "rejected" {
			t.Fatal("撤回后拒绝决定仍传入生成上下文")
		}
	}
}

// 已经执行的修订维持原有约束：不能改决定，也不能撤回。
func TestWithdrawRejectedAfterApplied(t *testing.T) {
	s, e, r := reviewFixture(t)
	id := r.Data.Proposals[0].ID
	if _, err := s.UpdateRun(r.ID, 0, func(run *Run) error {
		run.Data.Proposals[0].Decision = "accepted"
		run.Data.Proposals[0].Applied = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Run(r.ID)
	for _, decision := range []string{"pending", "rejected"} {
		code, body := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/decisions", map[string]any{
			"decisions": []map[string]any{{"id": id, "decision": decision}},
		}, cur.Revision)
		if code == 200 {
			t.Fatalf("已执行的修订不应允许改为 %s", decision)
		}
		if !strings.Contains(string(body), "已经执行的修订不能更改决定") {
			t.Fatalf("错误信息不符: %s", body)
		}
	}
}

// 非法决定值仍然被拒绝。
func TestDecisionRejectsUnknownValue(t *testing.T) {
	s, e, r := reviewFixture(t)
	code, _ := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/decisions", map[string]any{
		"decisions": []map[string]any{{"id": r.Data.Proposals[0].ID, "decision": "maybe"}},
	}, r.Revision)
	if code == 200 {
		t.Fatal("未知决定值不应通过")
	}
}

// 撤回后的意见不能通过 Reason 扫描重新回到人工决定记录。
func TestWithdrawnNotReDerived(t *testing.T) {
	r := Run{}
	p := Proposal{ID: "x", Chapter: 1, Kind: "outline", Decision: "rejected", Reason: "人工确认继续修订"}
	r.Data.Proposals = []Proposal{p}
	recordHumanDecision(&r, p)
	if len(humanDecisions(r)) != 1 {
		t.Fatal("前置条件：应有一条人工决定")
	}
	r.Data.Proposals[0].Decision = "pending"
	r.Data.Proposals[0].Reason = "人工撤回决定，退回待处理"
	forgetHumanDecision(&r, "x")
	if got := humanDecisions(r); len(got) != 0 {
		t.Fatalf("撤回后仍被重新推导出 %d 条决定: %+v", len(got), got)
	}
	if ctx := decisionContext(r); len(ctx) != 0 {
		t.Fatalf("撤回后仍传入生成上下文: %+v", ctx)
	}
}
