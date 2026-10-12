package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCrossChapterHumanDecisionAndPropagation(t *testing.T) {
	s, e, f, p := setup(t)
	var offered atomic.Bool
	f.hook = func(ctx context.Context, req ModelRequest, chunk func(string, string)) (ModelResult, error, bool) {
		if req.Node == "F8" {
			text := "主角留下了线索。\n\n他决定主动告知同伴，完成了跨章修订。"
			chunk(text, "")
			return ModelResult{Text: text}, nil, true
		}
		if req.Node == "F5" && strings.Contains(req.User, "当前章节：2；") && strings.Contains(req.User, "只核查目标第 3 章") && offered.CompareAndSwap(false, true) {
			latest, _ := s.Project(p.ID)
			v := latest.Current(3, "outline")
			proposal := Proposal{Chapter: 3, Kind: "outline", Problem: "人物认知需要人工确认", Instruction: "让人物主动告知同伴", Critical: true, Evidence: citations(*v, true)[:1]}
			text := asJSON(Analysis{Issues: []Proposal{proposal}})
			return ModelResult{Text: text}, nil, true
		}
		return ModelResult{}, nil, false
	}
	run, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	r := awaitWithHumanInputs(t, s, e, run.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Stage != "F" || len(r.Data.Proposals) != 1 {
		t.Fatalf("missing creative gate: %+v", r)
	}
	before, _ := s.Project(p.ID)
	if len(before.Versions) != 1 {
		t.Fatal("unapproved batch published")
	}
	_, err = s.UpdateRun(r.ID, 0, func(r *Run) error { r.Data.Proposals[0].Decision = "accepted"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	e.Control(r.ID, "resume", 0)
	r = awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	if r.Stage != "H" {
		t.Fatal(r.Stage)
	}
	after, _ := s.Project(p.ID)
	if !strings.Contains(after.Current(3, "outline").Text, "跨章修订") {
		t.Fatal("revision did not propagate")
	}
	if len(after.Versions) != 4 {
		t.Fatalf("unexpected versions %d", len(after.Versions))
	}
	if r.Chapter > 3 || r.Chapter < 1 {
		t.Fatal("revision changed scheduler cursor")
	}
}
func TestPendingEditAppliedAtSafePoint(t *testing.T) {
	s, e, f, p := setup(t)
	gate := make(chan struct{})
	var once atomic.Bool
	f.hook = func(ctx context.Context, r ModelRequest, c func(string, string)) (ModelResult, error, bool) {
		if r.Node == "D2" && once.CompareAndSwap(false, true) {
			close(gate)
			<-ctx.Done()
			return ModelResult{}, ctx.Err(), true
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
	<-gate
	e.Control(r.ID, "pause", 0)
	_, err = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.PendingEdits = []PendingEdit{{ID: ID(), Chapter: 1, Kind: "outline", Text: "主角留下了线索。人工提前写出的第一章。"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e.Control(r.ID, "resume", 0)
	awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "waiting_input" })
	p, _ = s.Project(p.ID)
	if p.Current(1, "outline") == nil || !strings.Contains(p.Current(1, "outline").Text, "人工提前") {
		t.Fatal("pending edit lost")
	}
	if len(p.PendingEdits) != 0 {
		t.Fatal("pending edits not drained")
	}
}
func TestVectorIndexOnlyReturnsActiveVersions(t *testing.T) {
	s, e, _, p := setup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		data := []map[string]any{}
		for i, text := range request.Input {
			vector := []float64{1, 0}
			if strings.Contains(text, "旧") {
				vector = []float64{0, 1}
			}
			data = append(data, map[string]any{"index": i, "embedding": vector})
		}
		writeJSON(w, 200, map[string]any{"data": data})
	}))
	defer server.Close()
	e.Provider = &HTTPProvider{}
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Settings.EmbeddingURL = server.URL
		p.Settings.EmbeddingModel = "test"
		p.Settings.EmbeddingCredential = "none"
		adopt(p, Change{Chapter: 1, Kind: "outline", Text: "旧版资料"}, "manual")
		return nil
	})
	if err := e.indexActive(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	old := p.Current(1, "outline").ID
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		adopt(p, Change{Chapter: 1, Kind: "outline", Text: "新的有效资料"}, "manual")
		return nil
	})
	hits, err := e.search(context.Background(), p, "不匹配关键词的语义查询", 10)
	if err != nil || len(hits) != 1 || hits[0].Version == old {
		t.Fatal(hits, err)
	}
}
func TestFiftyChapterProgressIncludesIsolatedChapters(t *testing.T) {
	s, e, _, p := setup(t)
	st, err := GenerateStructure(context.Background(), 50, 7)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.UpdateProject(p.ID, 0, func(p *Project) error {
		p.Structure = st
		p.Chapters = nil
		for c := 1; c <= 50; c++ {
			p.Chapters = append(p.Chapters, Chapter{Number: c})
		}
		return nil
	})
	r, err := e.Start(p.ID, "backward")
	if err != nil {
		t.Fatal(err)
	}
	// 50章会保存完整请求和逐章伏笔材料，竞态检测下为数据库写入留出余量。
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		r, _ = s.Run(r.ID)
		if r.Status == "waiting_input" && r.Stage == "C" {
			r = submitFixtureCreativity(t, s, e, r)
			if _, err = e.Control(r.ID, "resume", 0); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if r.Status == "waiting_input" || r.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r.Status != "waiting_input" || r.Stage != "H" {
		t.Fatalf("50 chapter run: %s %s %s", r.Status, r.Stage, r.Error)
	}
	p, _ = s.Project(p.ID)
	for _, c := range p.Chapters {
		if c.Outline == "" {
			t.Fatalf("chapter %d skipped", c.Number)
		}
	}
	approveReview(t, s, e, r)
	awaitWithHumanInputs(t, s, e, r.ID, func(r Run) bool { return r.Status == "completed" })
}
