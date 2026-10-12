package workbench

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) copyProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	p, err := s.Store.Project(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if expected(r) > 0 && expected(r) != p.Revision {
		fail(w, ErrConflict)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = p.Name + "（副本）"
	}
	p.ID = ID()
	p.Name = req.Name
	p.Created = now()
	p.Revision = 1
	p.Archived = false
	p.PendingEdits = []PendingEdit{}
	if err = s.Store.CreateProject(p); err != nil {
		fail(w, err)
		return
	}
	// 新作品有独立索引，不继承原作品运行与候选修改。
	p, err = s.Store.UpdateProject(p.ID, 1, func(*Project) error { return nil })
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, p)
}
func (s *Server) saveNote(w http.ResponseWriter, r *http.Request) {
	var note Note
	if err := decode(r, &note); err != nil {
		fail(w, err)
		return
	}
	p, err := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		if note.Kind != "review" && note.Kind != "ideas" && note.Kind != "references" {
			return fmt.Errorf("笔记类型无效")
		}
		if note.Chapter < 0 || note.Chapter > len(p.Chapters) || strings.TrimSpace(note.Text) == "" {
			return fmt.Errorf("请填写笔记内容并选择有效章节")
		}
		note.Updated = now()
		if note.ID == "" {
			note.ID = ID()
			note.Created = note.Updated
			p.Notes = append(p.Notes, note)
			return nil
		}
		for i, old := range p.Notes {
			if old.ID == note.ID {
				note.Created = old.Created
				p.Notes[i] = note
				return nil
			}
		}
		return fmt.Errorf("笔记不存在")
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		for i, n := range p.Notes {
			if n.ID == r.PathValue("note") {
				p.Notes = append(p.Notes[:i], p.Notes[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("笔记不存在")
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) answerInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	if strings.TrimSpace(req.Answer) == "" {
		fail(w, fmt.Errorf("请填写回答，也可以明确写明交由作者自由决定"))
		return
	}
	// 回答及参考笔记同事务保存，刷新后不会再次询问已回答的问题。
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
	if (expected(r) > 0 && expected(r) != run.Revision) || run.Data.Input == nil || run.Data.Input.ID != req.ID {
		fail(w, ErrConflict)
		return
	}
	if run.Status != "waiting_input" && run.Status != "paused" {
		fail(w, fmt.Errorf("当前不等待补充输入"))
		return
	}
	p, err := readProject(tx, run.ProjectID)
	if err != nil {
		fail(w, err)
		return
	}
	text := strings.Join(run.Data.Input.Questions, "\n") + "\n\n用户回答：\n" + req.Answer
	found := false
	for i, ref := range p.References {
		if ref.ID == req.ID {
			p.References[i].Text = text
			found = true
		}
	}
	if !found {
		p.References = append(p.References, Reference{ID: req.ID, Title: fmt.Sprintf("第 %d 章 · 补充回答", run.Chapter), Text: text})
	}
	p.Revision++
	if err = saveProject(tx, p); err != nil {
		fail(w, err)
		return
	}
	run.Data.Input.Answer = req.Answer
	run.Status = "paused"
	run.Revision++
	run.Updated = now()
	run.Message = "回答已保存为参考资料，可继续或前进一个小节点"
	if _, err = tx.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", run.Status, asJSON(run), run.ID); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	s.Store.Event(run.ID, "run", run)
	s.Store.Event(run.ID, "project", map[string]string{"projectId": p.ID})
	writeJSON(w, 200, map[string]any{"project": p, "run": run})
}
