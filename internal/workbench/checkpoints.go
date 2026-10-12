package workbench

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Checkpoint struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	Name            string `json:"name"`
	Note            string `json:"note"`
	Created         string `json:"created"`
	ProjectRevision int    `json:"projectRevision"`
	Mode            string `json:"mode"`
	Node            string `json:"node"`
	Chapter         int    `json:"chapter"`
	Automatic       bool   `json:"automatic,omitempty"`
}
type checkpointDoc struct {
	Meta     Checkpoint `json:"meta"`
	Project  Project    `json:"project"`
	Run      *Run       `json:"run"`
	Attempts []Attempt  `json:"attempts"`
}

func checkpointAt(tx *sql.Tx, p Project, name, note string) (checkpointDoc, error) {
	var raw string
	err := tx.QueryRow("SELECT doc FROM runs WHERE project_id=? ORDER BY rowid DESC LIMIT 1", p.ID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return checkpointDoc{}, err
	}
	if err == nil {
		var r Run
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return checkpointDoc{}, err
		}
		return checkpointForRun(tx, p, &r, name, note)
	}
	return checkpointForRun(tx, p, nil, name, note)
}

func checkpointForRun(tx *sql.Tx, p Project, r *Run, name, note string) (checkpointDoc, error) {
	c := checkpointDoc{Meta: Checkpoint{ID: ID(), ProjectID: p.ID, Name: name, Note: note, Created: now(), ProjectRevision: p.Revision}, Project: p, Run: r}
	if r != nil {
		if r.Status == "running" {
			return c, fmt.Errorf("请先暂停运行，再保存或恢复检查点")
		}
		c.Meta.Mode = r.Mode
		c.Meta.Node = r.NextNode
		if c.Meta.Node == "" {
			c.Meta.Node = r.Node
		}
		c.Meta.Chapter = r.Chapter
		rows, err := tx.Query("SELECT doc FROM attempts WHERE run_id=? ORDER BY rowid", r.ID)
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			var a Attempt
			if err = rows.Scan(&raw); err != nil {
				return c, err
			}
			if err = json.Unmarshal([]byte(raw), &a); err != nil {
				return c, err
			}
			c.Attempts = append(c.Attempts, a)
		}
		if err = rows.Err(); err != nil {
			return c, err
		}
	}
	return c, nil
}

// 只在章节首次采用的事务内调用。快照使用保存后的游标，不暂停正在连续运行的任务。
// 内容、执行位置和检查点要么一起提交，要么一起回滚，重试不会漏存或重复打点。
func chapterCheckpoint(tx *sql.Tx, p Project, r Run) error {
	kind := "大纲"
	if r.Mode == "forward" {
		kind = "正文"
	}
	r.Status = "paused"
	r.SingleStep = false
	r.Error = ""
	name := fmt.Sprintf("自动 · 第 %d 章%s完成", r.Chapter, kind)
	r.Message = name + "；可继续后续流程"
	c, err := checkpointForRun(tx, p, &r, name, "章节首次保存成功后自动创建；包含本轮跨章修订、统筹记忆和执行位置。回退后从保存后的步骤继续，不重写已保存章节；后续审核仍照常进行。")
	if err != nil {
		return err
	}
	c.Meta.Automatic = true
	if err = writeCheckpoint(tx, c); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO events(run_id,kind,data,created) VALUES(?,?,?,?)", r.ID, "checkpoint", asJSON(c.Meta), c.Meta.Created)
	return err
}
func writeCheckpoint(tx *sql.Tx, c checkpointDoc) error {
	_, err := tx.Exec("INSERT INTO checkpoints VALUES(?,?,?,?)", c.Meta.ID, c.Meta.ProjectID, c.Meta.Created, asJSON(c))
	return err
}
func (s *Store) CreateCheckpoint(project, name, note string, revision int) (Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Checkpoint{}, err
	}
	defer tx.Rollback()
	p, err := readProject(tx, project)
	if err != nil {
		return Checkpoint{}, err
	}
	if revision > 0 && revision != p.Revision {
		return Checkpoint{}, ErrConflict
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "第 " + fmt.Sprint(p.Revision) + " 版检查点"
	}
	c, err := checkpointAt(tx, p, name, note)
	if err != nil {
		return Checkpoint{}, err
	}
	if err = writeCheckpoint(tx, c); err != nil {
		return Checkpoint{}, err
	}
	return c.Meta, tx.Commit()
}
func (s *Store) Checkpoints(project string) ([]Checkpoint, error) {
	rows, err := s.db.Query("SELECT doc FROM checkpoints WHERE project_id=? ORDER BY rowid DESC", project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkpoint{}
	for rows.Next() {
		var raw string
		var c checkpointDoc
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		out = append(out, c.Meta)
	}
	return out, rows.Err()
}

// 恢复前在同一事务保存当前快照，切换内容索引并创建新的暂停运行；旧执行代次作废。
// 历史正文与请求不删除，防止回退使用户丢失恢复后的内容或让迟到请求写回。
func (s *Store) RestoreCheckpoint(project, id string, revision int) (Project, *Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Project{}, nil, err
	}
	defer tx.Rollback()
	current, err := readProject(tx, project)
	if err != nil {
		return current, nil, err
	}
	if revision > 0 && revision != current.Revision {
		return current, nil, ErrConflict
	}
	var raw string
	if err = tx.QueryRow("SELECT doc FROM checkpoints WHERE id=? AND project_id=?", id, project).Scan(&raw); err != nil {
		return current, nil, err
	}
	var c checkpointDoc
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return current, nil, err
	}
	backup, err := checkpointAt(tx, current, "恢复前自动备份", "恢复到："+c.Meta.Name)
	if err != nil {
		return current, nil, err
	}
	if err = writeCheckpoint(tx, backup); err != nil {
		return current, nil, err
	}
	if old := backup.Run; old != nil && old.Status != "completed" && old.Status != "stopped" {
		old.Status = "stopped"
		old.Epoch++
		old.Revision++
		old.Message = "已从检查点创建新运行"
		old.Updated = now()
		if _, err = tx.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", old.Status, asJSON(old), old.ID); err != nil {
			return current, nil, err
		}
	}
	p := c.Project
	p.Revision = current.Revision + 1
	p.Archived = false
	seen := map[string]bool{}
	for _, v := range p.Versions {
		seen[v.ID] = true
	}
	for _, v := range current.Versions {
		if !seen[v.ID] {
			p.Versions = append(p.Versions, v)
		}
	}
	// 提示词历史也保留；把检查点选定配置重新发布为最新版本。
	for node, history := range current.Prompts {
		restored, _ := promptText(p, node)
		p.Prompts[node] = history
		p.Prompts[node] = append(p.Prompts[node], PromptVersion{ID: ID(), Text: restored, Created: now()})
	}
	if err = saveProject(tx, p); err != nil {
		return current, nil, err
	}
	var restored *Run
	if c.Run != nil {
		r := *c.Run
		r.ID = ID()
		r.ProjectID = p.ID
		r.Status = "paused"
		if c.Run.Status == "completed" || c.Run.Status == "stopped" {
			r.Status = c.Run.Status
		}
		r.Epoch = 1
		r.Revision = 1
		r.Created = now()
		r.Updated = r.Created
		r.Error = ""
		r.SingleStep = false
		r.Message = "已恢复检查点：" + c.Meta.Name
		if _, err = tx.Exec("INSERT INTO runs VALUES(?,?,?,?)", r.ID, p.ID, r.Status, asJSON(r)); err != nil {
			return current, nil, err
		}
		for _, a := range c.Attempts {
			a.ID = ID()
			a.RunID = r.ID
			if a.Status == "running" {
				a.Status = "interrupted"
				a.Finished = now()
			}
			if _, err = tx.Exec("INSERT INTO attempts VALUES(?,?,?,?,?)", a.ID, r.ID, a.TaskKey, a.Status, asJSON(a)); err != nil {
				return current, nil, err
			}
		}
		restored = &r
	}
	return p, restored, tx.Commit()
}
func (s *Server) listCheckpoints(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Store.Checkpoints(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, cs)
}
func (s *Server) createCheckpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	// 短保存事务先结束，随后取消在途请求并保存暂停游标。
	runs, err := s.Store.Runs(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	for _, run := range runs {
		if run.Status == "running" {
			if _, err = s.Engine.Control(run.ID, "pause", 0); err != nil {
				fail(w, err)
				return
			}
		}
	}
	c, err := s.Store.CreateCheckpoint(r.PathValue("id"), req.Name, req.Note, expected(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, c)
}
func (s *Server) restoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	p, run, err := s.Store.RestoreCheckpoint(r.PathValue("id"), r.PathValue("checkpoint"), expected(r))
	if err != nil {
		fail(w, err)
		return
	}
	if run != nil {
		s.Store.Event(run.ID, "run", run)
	}
	writeJSON(w, 200, map[string]any{"project": p, "run": run})
}
