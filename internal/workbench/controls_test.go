package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func requestAPI(t *testing.T, s *Store, e *Engine, method, path string, body any, revision int) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, "/api"+path, strings.NewReader(asJSON(body)))
	req.Header.Set("Idempotency-Key", ID())
	if revision > 0 {
		req.Header.Set("If-Match", fmt.Sprint(revision))
	}
	w := httptest.NewRecorder()
	NewServer(s, e).Handler().ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}
func TestSingleStepRunsExactlyOneInternalNode(t *testing.T) {
	s, e, f, p := setup(t)
	r, err := e.StartMode(p.ID, "backward", true)
	if err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "paused" })
	if r.Node != "A1" || r.NextNode != "A2" {
		t.Fatalf("单步越过 A1：%+v", r)
	}
	f.mu.Lock()
	calls := len(f.calls)
	f.mu.Unlock()
	if calls != 0 {
		t.Fatal("程序节点调用了模型")
	}
	// 首次暂停也可能先于末尾事件落库，取事件边界前等待A1的worker退出。
	e.mu.Lock()
	initialWorker, initialActive := e.workers[r.ID]
	e.mu.Unlock()
	if initialActive {
		<-initialWorker.done
	}
	for steps := 0; steps < 260; steps++ {
		last := int64(0)
		if err := s.db.QueryRow("SELECT coalesce(max(seq),0) FROM events WHERE run_id=?", r.ID).Scan(&last); err != nil {
			t.Fatal(err)
		}
		expected := r.NextNode
		if _, err = e.Control(r.ID, "step", r.Revision); err != nil {
			t.Fatal(err)
		}
		r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "paused" || r.Status == "waiting_input" })
		if r.Node != expected {
			t.Fatalf("单步应执行 %s，实际 %s", expected, r.Node)
		}
		// 等 worker 完成事件写入，节点状态已经提交。
		e.mu.Lock()
		w, active := e.workers[r.ID]
		e.mu.Unlock()
		if active {
			<-w.done
		}
		events, _ := s.Events(r.ID, last)
		for _, ev := range events {
			if ev.Type == "node" {
				m := ev.Data.(map[string]any)
				if m["node"] != expected {
					t.Fatalf("期望 %s，上一次游标 %s，新增事件 %v", expected, r.NextNode, m)
				}
			}
		}
		if r.Status == "waiting_input" {
			if r.NextNode == "C2" {
				r = submitFixtureCreativity(t, s, e, r)
				continue
			}
			if r.NextNode != "H5" {
				t.Fatal(r.NextNode)
			}
			break
		}
		if steps == 259 {
			t.Fatal("未到达最终审核")
		}
	}
	p, _ = s.Project(p.ID)
	if len(p.Versions) != 3 {
		t.Fatal("提交次数不正确", len(p.Versions))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	generated := 0
	for _, req := range f.calls {
		if req.Node == "D2" {
			generated++
		}
	}
	if generated != 3 {
		t.Fatal("单步重复生成", generated)
	}
}
func TestCheckpointRestoresCursorCandidatesAndKeepsBackup(t *testing.T) {
	s, e, f, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Stage: "D", Node: "D2", NextNode: "D3", Status: "paused", Epoch: 1, Revision: 1, Chapter: 3, Data: freshData()}
	r.Data.Generated = "主角留下了线索。\n\n检查点中的候选。"
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCheckpoint(p.ID, "候选生成完毕", "继续保存候选", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.UpdateProject(p.ID, p.Revision, func(p *Project) error {
		adopt(p, Change{Chapter: 1, Kind: "outline", Text: "恢复前的新内容"}, "manual")
		p.Ideas = append(p.Ideas, Idea{ID: ID(), Text: "恢复前的新点子"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	restored, nr, err := s.RestoreCheckpoint(p.ID, c.ID, changed.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if nr == nil || nr.ID == r.ID || nr.NextNode != "D3" || nr.Status != "paused" {
		t.Fatalf("游标恢复错误：%+v", nr)
	}
	if restored.ActiveID(1, "outline") != "" || len(restored.Versions) != 1 {
		t.Fatal("未恢复有效版本或丢失历史")
	}
	old, _ := s.Run(r.ID)
	if old.Status != "stopped" {
		t.Fatal("旧运行未作废")
	}
	cs, _ := s.Checkpoints(p.ID)
	if len(cs) != 2 || cs[0].Name != "恢复前自动备份" {
		t.Fatal("缺少自动备份", cs)
	}
	if _, err = e.Control(nr.ID, "step", 0); err != nil {
		t.Fatal(err)
	}
	result := awaitRun(t, s, nr.ID, func(r Run) bool { return r.Status == "paused" })
	if result.Node != "D3" || result.NextNode != "D4" || len(result.Data.Batch) != 1 || result.Data.Batch[0].Text != r.Data.Generated {
		t.Fatal("没有继续保存原候选", result)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 {
		t.Fatal("恢复后重复调用了生成模型")
	}
	if _, _, err = s.RestoreCheckpoint(p.ID, c.ID, 1); err != ErrConflict {
		t.Fatal("旧版本恢复请求未拒绝", err)
	}
}
func TestCheckpointCannotRestoreDuringRunning(t *testing.T) {
	s, _, _, p := setup(t)
	c, err := s.CreateCheckpoint(p.ID, "开始前", "", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	r := Run{ID: ID(), ProjectID: p.ID, Status: "running", Epoch: 1, Data: freshData()}
	s.CreateRun(r)
	if _, _, err = s.RestoreCheckpoint(p.ID, c.ID, p.Revision); err == nil {
		t.Fatal("在途运行被覆盖")
	}
	cs, _ := s.Checkpoints(p.ID)
	if len(cs) != 1 {
		t.Fatal("拒绝恢复后留下了半成品备份")
	}
}
func TestMissingInputSavedAsReferenceAndResumes(t *testing.T) {
	s, e, f, p := setup(t)
	var asked atomic.Bool
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "B7" && asked.CompareAndSwap(false, true) {
			return ModelResult{Text: `{"queries":[],"inputs":["主角必须保留的名字是什么？"]}`}, nil, true
		}
		return ModelResult{}, nil, false
	}
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.NextNode != "B8" || r.Data.Input == nil {
		t.Fatal("必要问题没有中断等待", r)
	}
	status, body := requestAPI(t, s, e, "POST", "/runs/"+r.ID+"/input", map[string]string{"id": r.Data.Input.ID, "answer": "请使用名字：林晚"}, r.Revision)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	p, _ = s.Project(p.ID)
	if len(p.References) != 1 || !strings.Contains(p.References[0].Text, "林晚") {
		t.Fatal("回答没有保存为资料")
	}
	if _, err = e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
	r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Stage != "H" {
		t.Fatal("回答后重复卡在资料输入", r.Stage)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, c := range f.calls {
		if c.Node == "D2" && strings.Contains(c.User, "林晚") {
			found = true
		}
	}
	if !found {
		t.Fatal("回答没有进入创作请求")
	}
}
func TestHTTPUnexpectedDisconnectRetriesWithNewConnection(t *testing.T) {
	s, e, _, p := setup(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"恢复成功\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	e.Provider = &HTTPProvider{}
	p, _ = s.UpdateProject(p.ID, p.Revision, func(p *Project) error {
		m := p.Settings.Models["check"]
		m.BaseURL = server.URL
		m.Credential = "none"
		p.Settings.Models["check"] = m
		return nil
	})
	r := Run{ID: ID(), ProjectID: p.ID, Status: "running", Chapter: 1, Epoch: 1, Data: freshData()}
	s.CreateRun(r)
	text, err := e.call(context.Background(), &p, &r, "B7", "eof", "check", nil, "test")
	if err != nil || text != "恢复成功" {
		t.Fatal(text, err)
	}
	attempts, _ := s.Attempts(r.ID)
	if calls.Load() != 2 || len(attempts) != 2 || attempts[0].Status != "failed" || attempts[1].Status != "succeeded" {
		t.Fatal("重试记录错误", attempts, calls.Load())
	}
	events, _ := s.Events(r.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Type == "retry" {
			found = true
		}
	}
	if !found {
		t.Fatal("重试过程未推送")
	}
}
func TestRetryWaitCanBePaused(t *testing.T) {
	s, e, f, p := setup(t)
	var calls atomic.Int32
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		calls.Add(1)
		return ModelResult{}, &url.Error{Op: "Post", URL: "https://model.invalid", Err: io.EOF}, true
	}
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := s.Events(r.ID, 0)
		found := false
		for _, v := range events {
			found = found || v.Type == "retry"
		}
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	started := time.Now()
	if _, err = e.Control(r.ID, "pause", 0); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second || calls.Load() != 1 {
		t.Fatal("暂停没有取消退避等待")
	}
}
func TestNotesLibraryAndCheckpointAPI(t *testing.T) {
	s, e, _, p := setup(t)
	status, body := requestAPI(t, s, e, "PUT", "/projects/"+p.ID+"/notes", Note{Kind: "review", Text: "第一章节奏太快", Chapter: 1}, p.Revision)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	json.Unmarshal(body, &p)
	if len(p.Notes) != 1 {
		t.Fatal("笔记未保存")
	}
	status, body = requestAPI(t, s, e, "POST", "/projects/"+p.ID+"/copy", map[string]string{"name": "副本"}, p.Revision)
	if status != 201 {
		t.Fatal(status, string(body))
	}
	var copy Project
	json.Unmarshal(body, &copy)
	if copy.ID == p.ID || len(copy.Notes) != 1 {
		t.Fatal("复制失败")
	}
	status, body = requestAPI(t, s, e, "PUT", "/projects/"+p.ID, map[string]bool{"archived": true}, p.Revision)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	if _, err := e.Start(p.ID, "backward"); err == nil {
		t.Fatal("归档作品仍能运行")
	}
	status, body = requestAPI(t, s, e, "POST", "/projects/"+copy.ID+"/checkpoints", map[string]string{"name": "第一次"}, copy.Revision)
	if status != 201 {
		t.Fatal(status, string(body))
	}
	status, body = requestAPI(t, s, e, "GET", "/projects/"+copy.ID+"/checkpoints", nil, 0)
	if status != 200 || !strings.Contains(string(body), "第一次") {
		t.Fatal("检查点列表失败", status, string(body))
	}
}

func TestWrappedEOFRetryLimitAndIndependentAttempts(t *testing.T) {
	s, e, f, p := setup(t)
	var calls atomic.Int32
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		n := calls.Add(1)
		if req.FreshConnection != (n > 1) {
			t.Errorf("重试连接没有隔离")
		}
		chunk("未完成片段", "")
		return ModelResult{Text: "未完成片段"}, &url.Error{Op: "Post", URL: "https://model.invalid/chat/completions", Err: io.EOF}, true
	}
	r := Run{ID: ID(), ProjectID: p.ID, Status: "running", Epoch: 1, Chapter: 3, Data: freshData()}
	s.CreateRun(r)
	_, err := e.call(context.Background(), &p, &r, "D2", "eof-limit", "outline", nil, "测试")
	if err == nil || !strings.Contains(err.Error(), "已自动重试 3 次") || calls.Load() != 4 {
		t.Fatal(calls.Load(), err)
	}
	attempts, _ := s.Attempts(r.ID)
	if len(attempts) != 4 {
		t.Fatal("重试记录数量错误")
	}
	ids := map[string]bool{}
	for _, a := range attempts {
		if ids[a.ID] || a.Status != "failed" || a.Output != "未完成片段" {
			t.Fatal("失败候选丢失或混入成功", a)
		}
		ids[a.ID] = true
	}
}

func TestCheckpointSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/checkpoint.db"
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	p := NewProject("重启恢复", Structure{Chapters: 3})
	if err = s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "paused", Stage: "B", Node: "B7", NextNode: "B8", Revision: 1, Epoch: 1, Chapter: 3, Data: freshData()}
	r.Data.Input = &InputRequest{ID: ID(), Questions: []string{"保留谁？"}}
	if err = s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCheckpoint(p.ID, "待回答", "", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, run, err := reopened.RestoreCheckpoint(p.ID, c.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID != p.ID || run == nil || run.NextNode != "B8" || run.Data.Input.Questions[0] != "保留谁？" {
		t.Fatal("重启后丢失检查点")
	}
}

func TestCompletedCheckpointKeepsTerminalProgress(t *testing.T) {
	s, _, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "completed", Stage: "J", Node: "J8", Epoch: 1, Revision: 1, Chapter: 3, Data: freshData()}
	s.CreateRun(r)
	c, err := s.CreateCheckpoint(p.ID, "大纲完成", "", p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, restored, err := s.RestoreCheckpoint(p.ID, c.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored == nil || restored.Status != "completed" || restored.Node != "J8" || restored.ID == r.ID {
		t.Fatal("完成进度没有恢复", restored)
	}
}
