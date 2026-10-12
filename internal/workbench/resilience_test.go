package workbench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMalformedAnalysisRetryDoesNotReuseRejectedOutput(t *testing.T) {
	s, e, f, p := setup(t)
	var count atomic.Int32
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if r.Node == "B7" && count.Add(1) == 1 {
			return ModelResult{Text: `{"wrong":true}`}, nil, true
		}
		return ModelResult{}, nil, false
	}
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	attempts, _ := s.Attempts(r.ID)
	failed, succeeded := 0, 0
	for _, a := range attempts {
		if a.Node == "B7" && a.Chapter == 3 {
			if a.Status == "failed" {
				failed++
			}
			if a.Status == "succeeded" {
				succeeded++
			}
		}
	}
	if failed != 1 || succeeded != 1 {
		t.Fatal("格式错误未生成独立重试记录", failed, succeeded)
	}
}

func TestTransientFailureRetriesAndPermanentFailureStops(t *testing.T) {
	for _, mode := range []string{"limited", "network", "auth"} {
		t.Run(mode, func(t *testing.T) {
			s, e, f, p := setup(t)
			var calls atomic.Int32
			f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
				if req.Node != "B7" {
					return ModelResult{}, nil, false
				}
				n := calls.Add(1)
				if mode == "auth" {
					return ModelResult{}, &ModelError{Status: 401, Message: "认证失败"}, true
				}
				if n == 1 {
					if mode == "network" {
						return ModelResult{}, &net.OpError{Op: "read", Net: "tcp", Err: fmt.Errorf("connection reset")}, true
					}
					return ModelResult{}, &ModelError{Status: 429, Message: "稍后重试"}, true
				}
				return ModelResult{}, nil, false
			}
			r, err := e.Start(p.ID, "backward")
			if err != nil {
				t.Fatal(err)
			}
			r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "failed" || r.Status == "waiting_input" })
			if mode == "auth" {
				if calls.Load() != 4 || r.Status != "failed" {
					t.Fatal("认证错误应重试三次后保留现场")
				}
			} else if calls.Load() != 4 || r.Status != "waiting_input" {
				t.Fatal("临时错误没有恢复", calls.Load(), r.Status)
			}
		})
	}
}

func TestFutureHumanRequirementCommittedWithBatch(t *testing.T) {
	s, e, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "running", Stage: "F", Chapter: 3, Epoch: 1, Data: freshData()}
	r.Data.Seen["initialized"] = true
	r.Data.Proposals = []Proposal{{ID: "future", Chapter: 1, Kind: "outline", Problem: "后续要求", Instruction: "第一章保留约定", Decision: "accepted"}}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	if err := e.revise(context.Background(), &p, &r); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Project(p.ID)
	if len(before.Ideas) != 0 {
		t.Fatal("未提交要求提前生效")
	}
	if err := s.CommitBatch(r.ID, r.Epoch, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Project(p.ID)
	if len(after.Ideas) != 1 || after.Ideas[0].Text != "第一章保留约定" || !after.Ideas[0].Required {
		t.Fatal("未来章节要求丢失")
	}
}

func TestSSEReconnectReplaysOnlyNewEvents(t *testing.T) {
	s, e, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Status: "paused", Epoch: 1, Revision: 1, Data: freshData()}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	s.Event(r.ID, "run", r)
	old, _ := s.Events(r.ID, 0)
	s.Event(r.ID, "node", map[string]string{"node": "D2", "status": "running"})
	server := httptest.NewServer(NewServer(s, e).Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/runs/"+r.ID+"/events", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprint(old[0].Seq))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		if !strings.HasPrefix(scan.Text(), "data: ") {
			continue
		}
		var event Event
		if err = json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "node" || event.Seq <= old[0].Seq {
			t.Fatal("旧事件被重复回放", event)
		}
		return
	}
	t.Fatal("未收到续传事件", scan.Err())
}

func TestArcReviewsWaitForWholeUnitInBothDirections(t *testing.T) {
	for _, mode := range []string{"backward", "forward"} {
		t.Run(mode, func(t *testing.T) {
			s, e, _, p := setup(t)
			p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
				p.Structure.Arcs = [][2]int{{1, 1}, {2, 3}}
				p.Settings.ArcReviews = true
				p.Settings.StyleVerified = true
				p.Settings.StyleModelFingerprint = fingerprint(p.Settings.Models["draft"])
				if mode == "forward" {
					for ch := 1; ch <= 3; ch++ {
						adopt(p, Change{Chapter: ch, Kind: "outline", Text: "已有大纲"}, "fixture")
					}
				}
				return nil
			})
			r, err := e.Start(p.ID, mode)
			if err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 3; round++ {
				r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
				p, _ = s.Project(p.ID)
				if !completeScope(p, r.Data.ReviewChapters, mode) {
					t.Fatal("剧情单元未完成就触发审核")
				}
				if round == 0 {
					want := "[1]"
					if mode == "backward" {
						want = "[2,3]"
					}
					if asJSON(r.Data.ReviewChapters) != want {
						t.Fatal("单元方向调度错误", r.Data.ReviewChapters)
					}
				}
				approveReview(t, s, e, r)
			}
			awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "completed" })
		})
	}
}

func TestReviewCommitRejectsPausedEpoch(t *testing.T) {
	s, _, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "running", Stage: "H", Epoch: 1, Data: freshData()}
	r.Data.ReviewApproved = true
	r.Data.ReviewChapters = []int{1}
	s.CreateRun(r)
	s.UpdateRun(r.ID, 0, func(r *Run) error { r.Status = "paused"; r.Epoch++; return nil })
	if err := s.CommitReview(r.ID, 1, p); err != ErrStopped {
		t.Fatal("暂停后审核仍然提交", err)
	}
	p, _ = s.Project(p.ID)
	if len(p.Reviews) != 0 {
		t.Fatal("审核记录越过取消屏障")
	}
}

func TestPromptPreviewUsesCompleteInputs(t *testing.T) {
	s, e, _, p := setup(t)
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.References = []Reference{{ID: "ref", Title: "背景", Text: "参考资料标记"}}
		p.Ideas = []Idea{{ID: "idea", Text: "人工创意标记", Active: true}}
		adopt(p, Change{Chapter: 3, Kind: "outline", Text: "有效原文标记"}, "fixture")
		return nil
	})
	req := httptest.NewRequest("POST", "/api/projects/"+p.ID+"/prompts/D2/preview", strings.NewReader(`{"text":"第 {{chapter}} 章的测试提示词","chapter":3,"mode":"backward"}`))
	req.Header.Set("Idempotency-Key", ID())
	w := httptest.NewRecorder()
	NewServer(s, e).Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, marker := range []string{"messages", "modelConfig", "参考资料标记", "人工创意标记", "有效原文标记", "第 3 章的测试提示词"} {
		if !strings.Contains(w.Body.String(), marker) {
			t.Fatal("请求预览漏材料", marker)
		}
	}
	if ValidatePrompt("{{unknown_variable}}") == nil {
		t.Fatal("未知变量未被拒绝")
	}
}
