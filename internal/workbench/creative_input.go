package workbench

import (
	"fmt"
	"net/http"
	"strings"
)

func chapterIdeas(p Project, chapter int) []Idea {
	ideas := []Idea{}
	for _, idea := range p.Ideas {
		if idea.Chapter == 0 || idea.Chapter == chapter {
			ideas = append(ideas, idea)
		}
	}
	return ideas
}
func activeIdeas(ideas []Idea) []Idea {
	out := []Idea{}
	for _, idea := range ideas {
		if idea.Active {
			out = append(out, idea)
		}
	}
	return out
}

func (s *Server) submitCreativeInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID              string `json:"id"`
		Ideas           []Idea `json:"ideas"`
		EmptyConfirmed  bool   `json:"emptyConfirmed"`
		ProjectRevision int    `json:"projectRevision"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	// 创意笔记、运行交付和不可变提交记录同事务保存，过期窗口不能覆盖新内容。
	s.Store.mu.Lock()
	defer s.Store.mu.Unlock()
	tx, err := s.Store.db.Begin()
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	run, err := readRun(tx, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	input := run.Data.CreativeInput
	if expected(r) != run.Revision || input == nil || input.ID != req.ID || input.SubmittedAt != "" || input.Chapter != run.Chapter || input.Cycle != run.Cycle {
		fail(w, ErrConflict)
		return
	}
	if run.Stage != "C" || (run.Status != "waiting_input" && run.Status != "paused") {
		fail(w, fmt.Errorf("当前不等待人工创意"))
		return
	}
	p, err := readProject(tx, run.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	if req.ProjectRevision != p.Revision {
		fail(w, ErrConflict)
		return
	}
	retained := []Idea{}
	ids := map[string]bool{}
	for _, idea := range p.Ideas {
		if idea.Chapter != 0 && idea.Chapter != run.Chapter {
			retained = append(retained, idea)
			ids[idea.ID] = true
		}
	}
	for i := range req.Ideas {
		idea := &req.Ideas[i]
		if strings.TrimSpace(idea.Text) == "" || (idea.Chapter != 0 && idea.Chapter != run.Chapter) {
			fail(w, fmt.Errorf("请填写创意内容；范围只能为全书或本章"))
			return
		}
		if idea.ID == "" {
			idea.ID = ID()
		}
		if ids[idea.ID] {
			fail(w, fmt.Errorf("创意编号重复或属于其他章节"))
			return
		}
		ids[idea.ID] = true
	}
	active := activeIdeas(req.Ideas)
	if len(active) == 0 && !req.EmptyConfirmed {
		fail(w, fmt.Errorf("请填写本章创意，或明确确认本章不提供创意"))
		return
	}
	p.Ideas = append(retained, req.Ideas...)
	p.Revision++
	if err = saveProject(tx, p); err != nil {
		fail(w, err)
		return
	}
	input.Ideas = req.Ideas
	input.EmptyConfirmed = len(active) == 0 && req.EmptyConfirmed
	input.SubmittedAt = now()
	run.CreativeInputs = append(run.CreativeInputs, *input)
	run.Data.ContextIdeas = active
	run.Status = "paused"
	run.Revision++
	run.Updated = now()
	run.Message = "人工创意已原样保存，可继续交付创作"
	if _, err = tx.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", run.Status, asJSON(run), run.ID); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.Store.Event(run.ID, "creative_input", *input)
	s.Store.Event(run.ID, "run", run)
	s.Store.Event(run.ID, "project", map[string]string{"projectId": p.ID})
	writeJSON(w, 200, map[string]any{"project": p, "run": run})
}
