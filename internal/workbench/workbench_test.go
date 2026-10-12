package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureModel struct {
	mu    sync.Mutex
	calls []ModelRequest
	hook  func(context.Context, ModelRequest, func(string, string)) (ModelResult, error, bool)
}

func (f *fixtureModel) Generate(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, r)
	f.mu.Unlock()
	if f.hook != nil {
		if result, e, handled := f.hook(ctx, r, chunk); handled {
			return result, e
		}
	}
	text := `{"queries":[],"issues":[],"links":[]}`
	switch r.Node {
	case "D1", "E1", "D4":
		// 随接口新增字段更新原有夹具，不调用真实模型。
		threads := []Thread{}
		if _, raw, ok := strings.Cut(r.User, "需要提示的伏笔："); ok {
			json.NewDecoder(strings.NewReader(raw)).Decode(&threads)
		}
		hints := []ForeshadowHint{}
		for _, thread := range threads {
			hints = append(hints, ForeshadowHint{ThreadID: thread.ID, Text: "一件旧物留下未解疑问。"})
		}
		text = asJSON(map[string]any{"queries": []string{}, "issues": []Proposal{}, "links": []Link{}, "foreshadows": hints})
	case "D2", "E2", "F8", "I4":
		text = "主角留下了线索。\n\n他在雨夜作出了不可逆的选择。"
	case "F9":
		text = `{"satisfied":true,"evidence":[{"quote":"主角留下了线索。","reason":"候选保留了需要核验的线索。"}],"missing":[]}`
	case "G4":
		text = `{"summary":"主角留下线索并做出选择。"}`
	}
	chunk(text, "")
	return ModelResult{Text: text, Usage: Usage{100, 50, 150}}, nil
}
func setup(t *testing.T) (*Store, *Engine, *fixtureModel, Project) {
	t.Helper()
	s, e := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	f := &fixtureModel{}
	eng := NewEngine(s, f)
	p := NewProject("测试小说", Structure{Chapters: 3, Lines: [][2]int{{1, 3}}, Arcs: [][2]int{{1, 3}}})
	if e = ValidateStructure(&p.Structure); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateProject(p); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { eng.Close(); s.Close() })
	return s, eng, f, p
}
func awaitRun(t *testing.T, s *Store, id string, pred func(Run) bool) Run {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r, e := s.Run(id)
		if e != nil {
			t.Fatal(e)
		}
		if pred(r) {
			return r
		}
		if r.Status == "failed" {
			t.Fatalf("run failed: stage=%s node=%s error=%s", r.Stage, r.Node, r.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, _ := s.Run(id)
	t.Fatalf("timeout waiting for run: %+v", r)
	return r
}
func approveReview(t *testing.T, s *Store, e *Engine, r Run) {
	t.Helper()
	_, err := s.UpdateRun(r.ID, 0, func(r *Run) error {
		r.Data.ReviewApproved = true
		for i := range r.Data.Proposals {
			r.Data.Proposals[i].Decision = "rejected"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratorReproducesAndSanitizes(t *testing.T) {
	s, e := GenerateStructure(context.Background(), 50, 7)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile("testdata/lines50.json")
	var expected [][2]int
	json.Unmarshal(b, &expected)
	if !reflect.DeepEqual(s.Lines, expected) || len(s.Threads) != 58 || len(s.Nodes) != 11 || len(s.Arcs) != 5 {
		t.Fatalf("unexpected structure: %+v", s)
	}
	if want := [][2]int{{1, 10}, {11, 17}, {18, 30}, {31, 43}, {44, 50}}; !reflect.DeepEqual(s.Arcs, want) {
		t.Fatalf("arcs must end at chapters with more than four incoming threads: got %v, want %v", s.Arcs, want)
	}
	again, e := GenerateStructure(context.Background(), 50, 7)
	if e != nil || !reflect.DeepEqual(s, again) {
		t.Fatal("seed is not repeatable", e)
	}
	encoded := asJSON(s)
	for _, forbidden := range []string{"clue", "event", "身份", "死亡"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("random content escaped: %s", forbidden)
		}
	}
	for _, n := range []int{3, 5, 6, 7, 27, 100, 2000} {
		for _, seed := range []int64{1, 6, 7, 19} {
			st, err := GenerateStructure(context.Background(), n, seed)
			if err != nil {
				t.Fatal(err)
			}
			assertGeneratedArcBoundaries(t, st)
			for _, v := range st.Threads {
				if v.Span < 2 || v.Span > 25 && len(v.Reminds) == 0 {
					t.Fatalf("invalid thread %+v", v)
				}
			}
		}
	}
	if _, e = GenerateStructure(context.Background(), 2, 7); e == nil {
		t.Fatal("small chapter count accepted")
	}
}
func TestImportAndValidation(t *testing.T) {
	for _, raw := range []string{`[[1,3],[1,3]]`, `[[3,1]]`, `[[0,3]]`, `[[1,4]]`, `[[1,2]]`, `[[1,3,4]]`, `[[1]]`} {
		if _, e := ImportStructure([]byte(raw), 3); e == nil {
			t.Fatalf("invalid input accepted: %s", raw)
		}
	}
	p, e := ImportStructure([]byte(`{"chapters":3,"nodes":[{"ch":3,"arc":1,"kind":"结账","event":"随机死亡"}],"threads":[{"id":"T1","plant":1,"payoff":3,"clue":"随机钥匙","type":"身份"}]}`), 3)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(asJSON(p), "随机") {
		t.Fatal("random narrative imported")
	}
}
func TestBothDirectionsFinalReviewAndInvalidation(t *testing.T) {
	s, e, f, p := setup(t)
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Stage != "H" {
		t.Fatalf("expected final review: %+v", r)
	}
	p, _ = s.Project(p.ID)
	if !completeScope(p, []int{1, 2, 3}, "backward") {
		t.Fatal("isolated chapter skipped")
	}
	f.mu.Lock()
	order := []string{}
	for _, c := range f.calls {
		if c.Node == "D2" {
			order = append(order, strings.Split(c.User, "；")[0])
		}
	}
	f.mu.Unlock()
	if !reflect.DeepEqual(order, []string{"当前章节：3", "当前章节：2", "当前章节：1"}) {
		t.Fatal(order)
	}
	approveReview(t, s, e, r)
	awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "completed" })
	p, err = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Settings.StyleVerified = true
		p.Settings.StyleModelFingerprint = fingerprint(p.Settings.Models["draft"])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err = e.Start(p.ID, "forward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Stage != "H" {
		t.Fatal(r.Stage)
	}
	approveReview(t, s, e, r)
	awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "completed" })
	p, _ = s.Project(p.ID)
	if !reviewValid(p, []int{1, 2, 3}, "forward") {
		t.Fatal("final review missing")
	}
	_, err = s.UpdateProject(p.ID, 0, func(p *Project) error {
		adopt(p, Change{Chapter: 2, Kind: "draft", Text: "新的原文"}, "manual")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.Project(p.ID)
	if reviewValid(p, []int{1, 2, 3}, "forward") {
		t.Fatal("old review remained valid")
	}
	hits, err := s.Search(p.ID, "新的", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("short Chinese search failed: %v %v", hits, err)
	}
}
func TestPauseResumeKeepsCompletedStepsAndPromptVersions(t *testing.T) {
	s, e, f, p := setup(t)
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	f.hook = func(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if r.Node == "D2" && calls.Add(1) == 1 {
			chunk("部分草稿", "")
			entered <- struct{}{}
			<-ctx.Done()
			return ModelResult{Text: "迟到的完整回复"}, nil, true
		}
		return ModelResult{}, nil, false
	}
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r = awaitRun(t, s, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	r = submitFixtureCreativity(t, s, e, r)
	if _, err = e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no generation")
	}
	if _, err = e.Control(r.ID, "pause", 0); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Project(p.ID)
	if len(p.Versions) > 0 {
		t.Fatal("late result adopted")
	}
	as, _ := s.Attempts(r.ID)
	partial := false
	for _, a := range as {
		if a.Node == "D2" && a.Status == "interrupted" && a.Output == "部分草稿" {
			partial = true
		}
	}
	if !partial {
		t.Fatal("partial output lost")
	}
	_, err = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Prompts["D2"] = []PromptVersion{{ID: "updated", Text: "新提示词标记", Created: now()}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(r.ID, "resume", 0); err != nil {
		t.Fatal(err)
	}
	r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	as, _ = s.Attempts(r.ID)
	b7 := 0
	newPrompt := false
	for _, a := range as {
		if a.Node == "B7" && a.Chapter == 3 {
			b7++
		}
		if a.Node == "D2" && a.Status == "succeeded" && strings.Contains(a.Prompt, "新提示词标记") {
			newPrompt = true
		}
	}
	if b7 != 1 || !newPrompt {
		t.Fatalf("resume did not preserve steps/config: b7=%d new=%v", b7, newPrompt)
	}
	if _, err = e.Control(r.ID, "stop", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Control(r.ID, "resume", 0); err == nil {
		t.Fatal("terminated run resumed")
	}
}
func TestRestartRecoversPaused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	r := Run{ID: ID(), ProjectID: ID(), Status: "running", Epoch: 1, Data: freshData()}
	if err = s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err = s.Run(r.ID)
	if err != nil || r.Status != "paused" || r.Epoch != 2 {
		t.Fatal(r, err)
	}
}
func TestAtomicBatchConflictAndFTS(t *testing.T) {
	s, _, _, p := setup(t)
	r := Run{ID: ID(), ProjectID: p.ID, Status: "running", Epoch: 1, Stage: "G", Data: freshData()}
	if err := s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	batch := []Change{{Chapter: 1, Kind: "outline", Text: "旧仓库的秘密", Summary: "秘密"}, {Chapter: 2, Kind: "outline", Text: "第二章", Base: "nonexistent"}}
	if err := s.CommitBatch(r.ID, 1, batch, nil); err == nil {
		t.Fatal("version conflict accepted")
	}
	p, _ = s.Project(p.ID)
	if len(p.Versions) != 0 {
		t.Fatal("partial batch adopted")
	}
	batch = batch[:1]
	if err := s.CommitBatch(r.ID, 1, batch, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(p.ID, "旧仓库", 10)
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
	if err = s.CommitBatch(r.ID, 1, batch, nil); err == nil {
		t.Fatal("batch committed twice")
	}
}
func TestHTTPProviderTextCompatibilityAndTruncation(t *testing.T) {
	for _, finish := range []string{"stop", "length", ""} {
		t.Run("finish_"+finish, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-opencode-session") != "test-session" {
					t.Error("missing stable session")
				}
				var payload map[string]any
				json.NewDecoder(r.Body).Decode(&payload)
				if _, ok := payload["tools"]; ok {
					t.Error("text provider received tools")
				}
				if _, ok := payload["thinking"]; ok {
					t.Error("RWKV received DeepSeek parameter")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"本地正文"}}]}`+"\n\n")
				if finish != "" {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\n", finish)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			m := defaults().Models["draft"]
			m.BaseURL = server.URL
			m.Model = "rwkv-local"
			m.Credential = "none"
			result, e := (&HTTPProvider{}).Generate(context.Background(), ModelRequest{Config: m, Session: "test-session"}, func(string, string) {})
			if result.Text != "本地正文" {
				t.Fatal(result)
			}
			if (e == nil) != (finish == "stop") {
				t.Fatalf("finish handling: %v", e)
			}
		})
	}
}
func TestIdempotentCreateAndOptimisticEdits(t *testing.T) {
	s, e, _, p := setup(t)
	handler := NewServer(s, e).Handler()
	payload := asJSON(map[string]any{"name": "幂等", "structure": p.Structure})
	request := func(body string, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	a := request(payload, "same")
	b := request(payload, "same")
	if a.Code != 201 || a.Body.String() != b.Body.String() {
		t.Fatal(a.Code, a.Body, b.Body)
	}
	if c := request(strings.ReplaceAll(payload, "幂等", "不同"), "same"); c.Code != 409 {
		t.Fatal(c.Code)
	}
	_, err := s.UpdateProject(p.ID, p.Revision+1, func(*Project) error { return nil })
	if err != ErrConflict {
		t.Fatal(err)
	}
}
