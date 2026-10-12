package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("状态已变化，请刷新后再操作")
var ErrStopped = errors.New("执行已暂停或终止")

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// OpenStore 的 WAL 保存已提交状态；异常退出后只恢复为待继续，不自动消费模型额度。
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	schema := `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS meta(version INTEGER NOT NULL);
 INSERT INTO meta SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM meta);
 CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY, doc TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY, project_id TEXT NOT NULL, status TEXT NOT NULL, doc TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS run_project ON runs(project_id);
 CREATE TABLE IF NOT EXISTS attempts(id TEXT PRIMARY KEY, run_id TEXT NOT NULL, task_key TEXT NOT NULL, status TEXT NOT NULL, doc TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS attempt_run ON attempts(run_id,task_key);
 CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL, kind TEXT NOT NULL, data TEXT NOT NULL, created TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS idempotency(key TEXT PRIMARY KEY, body_hash TEXT NOT NULL, status INTEGER NOT NULL, response BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS checkpoints(id TEXT PRIMARY KEY, project_id TEXT NOT NULL, created TEXT NOT NULL, doc TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS checkpoint_project ON checkpoints(project_id);
 CREATE TABLE IF NOT EXISTS canvas_layouts(id TEXT PRIMARY KEY, revision INTEGER NOT NULL, doc TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS vectors(project_id TEXT, version_id TEXT, paragraph INTEGER, model TEXT, vector TEXT, PRIMARY KEY(project_id,version_id,paragraph,model));
 CREATE VIRTUAL TABLE IF NOT EXISTS passages USING fts5(project_id UNINDEXED,version_id UNINDEXED,chapter UNINDEXED,kind UNINDEXED,paragraph UNINDEXED,text, tokenize='trigram');`
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	rs, err := s.Runs("")
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		// 旧版按整章必留标记升级的待办重新核查，历史决定不丢失，也不擅自当成用户采用。
		if r.Stage == "F" && (r.Status == "waiting_input" || r.Status == "paused") {
			changed := false
			r.HumanDecisions = humanDecisions(r)
			for i := range r.Data.Proposals {
				prop := &r.Data.Proposals[i]
				if prop.Decision == "pending" && prop.Reason == "涉及包含必留要求的章节" {
					prop.Decision, prop.Reason = "superseded", "旧规则待办已撤回，按最新人工决定重新核查"
					r.Data.Queue = append(r.Data.Queue, CheckItem{prop.Chapter, prop.Kind, "升级后重新核查"})
					changed = true
				}
			}
			if changed {
				r.NextNode, r.Status = "F5", "paused"
				r.Message = "旧规则产生的重复待办已收起；继续后按你的既有决定重新核查"
				r.Revision++
				r.Updated = now()
				b, _ := json.Marshal(r)
				if _, err = db.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", r.Status, b, r.ID); err != nil {
					db.Close()
					return nil, err
				}
			}
		}
		// 旧版本停在文风准备的运行改为待继续，历史模型输出保留但不再执行子流。
		if (r.Stage == "I" || strings.HasPrefix(r.NextNode, "I")) && r.Status != "completed" && r.Status != "stopped" {
			r.Stage, r.Node, r.NextNode = "J", "J1", "J1"
			r.Status = "running" // 复用下方中断恢复，明确等待用户继续。
		}
		if r.Status == "running" {
			r.Status = "paused"
			r.Epoch++
			r.Revision++
			r.Message = "服务重启，执行已中断；可继续"
			r.Updated = now()
			b, _ := json.Marshal(r)
			if _, err = db.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", r.Status, b, r.ID); err != nil {
				return nil, err
			}
			s.interruptAttempts(r.ID)
			s.Event(r.ID, "run", r)
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

type querier interface{ QueryRow(string, ...any) *sql.Row }

func readProject(q querier, id string) (Project, error) {
	var p Project
	var b []byte
	if err := q.QueryRow("SELECT doc FROM projects WHERE id=?", id).Scan(&b); err != nil {
		return p, err
	}
	err := json.Unmarshal(b, &p)
	if err == nil {
		normalizeSettings(&p.Settings)
	}
	return p, err
}
func readRun(q querier, id string) (Run, error) {
	var r Run
	var b []byte
	if err := q.QueryRow("SELECT doc FROM runs WHERE id=?", id).Scan(&b); err != nil {
		return r, err
	}
	err := json.Unmarshal(b, &r)
	return r, err
}
func (s *Store) Project(id string) (Project, error) { return readProject(s.db, id) }
func (s *Store) Run(id string) (Run, error)         { return readRun(s.db, id) }
func (s *Store) Projects() ([]Project, error) {
	rows, e := s.db.Query("SELECT doc FROM projects ORDER BY rowid DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var b []byte
		var p Project
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		normalizeSettings(&p.Settings)
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Runs(project string) ([]Run, error) {
	q := "SELECT doc FROM runs"
	args := []any{}
	if project != "" {
		q += " WHERE project_id=?"
		args = append(args, project)
	}
	q += " ORDER BY rowid DESC"
	rows, e := s.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var b []byte
		var r Run
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) CreateProject(p Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO projects VALUES(?,?)", p.ID, b)
	return e
}
func (s *Store) UpdateProject(id string, expected int, fn func(*Project) error) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return Project{}, e
	}
	defer tx.Rollback()
	p, e := readProject(tx, id)
	if e != nil {
		return p, e
	}
	if expected > 0 && p.Revision != expected {
		return p, ErrConflict
	}
	if e = fn(&p); e != nil {
		return p, e
	}
	p.Revision++
	if e = saveProject(tx, p); e != nil {
		return p, e
	}
	return p, tx.Commit()
}
func saveProject(tx *sql.Tx, p Project) error {
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE projects SET doc=? WHERE id=?", b, p.ID); e != nil {
		return e
	}
	return rebuildIndex(tx, p)
}

// 索引与有效版本在同一事务里切换，旧版本永远不会混入默认检索。
func rebuildIndex(tx *sql.Tx, p Project) error {
	if _, e := tx.Exec("DELETE FROM passages WHERE project_id=?", p.ID); e != nil {
		return e
	}
	for _, ch := range p.Chapters {
		for _, kind := range []string{"outline", "draft"} {
			v := p.Current(ch.Number, kind)
			if v == nil {
				continue
			}
			for i, t := range paragraphs(v.Text) {
				if _, e := tx.Exec("INSERT INTO passages(project_id,version_id,chapter,kind,paragraph,text) VALUES(?,?,?,?,?,?)", p.ID, v.ID, ch.Number, kind, i+1, t); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func paragraphs(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	out := []string{}
	for _, s := range raw {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
func (s *Store) CreateRun(r Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if e := s.db.QueryRow("SELECT count(*) FROM runs WHERE project_id=? AND status NOT IN ('completed','stopped')", r.ProjectID).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return fmt.Errorf("该项目已有未结束运行，请继续或终止后再开始")
	}
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO runs VALUES(?,?,?,?)", r.ID, r.ProjectID, r.Status, b)
	return e
}
func (s *Store) UpdateRun(id string, epoch int, fn func(*Run) error) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.Run(id)
	if e != nil {
		return r, e
	}
	if epoch > 0 && (r.Epoch != epoch || r.Status != "running") {
		return r, ErrStopped
	}
	if e = fn(&r); e != nil {
		return r, e
	}
	r.Revision++
	r.Updated = now()
	b, e := json.Marshal(r)
	if e != nil {
		return r, e
	}
	_, e = s.db.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", r.Status, b, r.ID)
	return r, e
}
func (s *Store) Event(run, kind string, data any) error {
	b, e := json.Marshal(data)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO events(run_id,kind,data,created) VALUES(?,?,?,?)", run, kind, b, now())
	return e
}
func (s *Store) Events(run string, after int64) ([]Event, error) {
	rows, e := s.db.Query("SELECT seq,kind,data,created FROM events WHERE run_id=? AND seq>? ORDER BY seq LIMIT 500", run, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		v := Event{RunID: run}
		var b []byte
		if e = rows.Scan(&v.Seq, &v.Type, &b, &v.Created); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v.Data); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Attempts(run string) ([]Attempt, error) {
	rows, e := s.db.Query("SELECT doc FROM attempts WHERE run_id=? ORDER BY rowid", run)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Attempt{}
	for rows.Next() {
		var a Attempt
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) SuccessfulAttempt(run, key string) (Attempt, bool) {
	var a Attempt
	var b []byte
	e := s.db.QueryRow("SELECT doc FROM attempts WHERE run_id=? AND task_key=? AND status='succeeded' ORDER BY rowid DESC LIMIT 1", run, key).Scan(&b)
	if e != nil {
		return a, false
	}
	return a, json.Unmarshal(b, &a) == nil
}
func (s *Store) SaveAttempt(a Attempt, epoch int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.Run(a.RunID)
	if e != nil {
		return e
	}
	if r.Epoch != epoch || r.Status != "running" {
		return ErrStopped
	}
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO attempts VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,doc=excluded.doc", a.ID, a.RunID, a.TaskKey, a.Status, b)
	return e
}
func (s *Store) interruptAttempts(run string) {
	as, e := s.Attempts(run)
	if e != nil {
		return
	}
	for _, a := range as {
		if a.Status == "running" {
			a.Status = "interrupted"
			a.Finished = now()
			b, _ := json.Marshal(a)
			s.db.Exec("UPDATE attempts SET status=?,doc=? WHERE id=?", a.Status, b, a.ID)
		}
	}
}

func (s *Store) CommitBatch(runID string, epoch int, batch []Change, links []Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := readRun(tx, runID)
	if e != nil {
		return e
	}
	if r.Epoch != epoch || r.Status != "running" {
		return ErrStopped
	}
	p, e := readProject(tx, r.ProjectID)
	if e != nil {
		return e
	}
	// 标记与状态推进也在同一事务，进程退出后不会重复提交这一批。
	if r.Stage != "G" || r.NextNode == "G7" {
		return ErrConflict
	}
	chapterCompleted := false
	for _, c := range batch {
		if p.ActiveID(c.Chapter, c.Kind) != c.Base {
			return fmt.Errorf("第 %d 章版本已变化，需要重新核查", c.Chapter)
		}
		if c.Chapter == r.Chapter && c.Kind == kindFor(r.Mode) && c.Base == "" {
			chapterCompleted = true
		}
	}
	for _, c := range batch {
		adopt(&p, c, "run:"+runID)
		for i, vector := range c.Embeddings {
			if _, e = tx.Exec("INSERT INTO vectors VALUES(?,?,?,?,?)", p.ID, p.ActiveID(c.Chapter, c.Kind), i+1, vectorModel(p.Settings), asJSON(vector)); e != nil {
				return e
			}
		}
	}
	commitForeshadows(&p, r, batch)
	for _, idea := range r.Data.FutureIdeas {
		found := false
		for _, old := range p.Ideas {
			found = found || old.ID == idea.ID
		}
		if !found {
			p.Ideas = append(p.Ideas, idea)
		}
	}
	remaining := []PendingEdit{}
	for _, edit := range p.PendingEdits {
		applied := false
		for _, id := range r.Data.PendingEditIDs {
			applied = applied || edit.ID == id
		}
		if !applied {
			remaining = append(remaining, edit)
		}
	}
	p.PendingEdits = remaining
	// 已确认意见随采用版本提交，供下一轮和前向写作继续遵守。
	p.HumanDecisions = humanDecisions(r)
	for _, l := range links {
		l.ID = ID()
		l.Valid = true
		l.SourceVersion = p.ActiveID(l.From, kindFor(r.Mode))
		l.TargetVersion = p.ActiveID(l.To, kindFor(r.Mode))
		p.Links = append(p.Links, l)
	}
	p.Revision++
	if e = saveProject(tx, p); e != nil {
		return e
	}
	r.Stage = "J"
	if r.Data.AfterCommit == "I" {
		r.Stage = "I"
	}
	r.Node = "G6"
	if r.NextNode != "" {
		r.Stage = "G"
		r.NextNode = "G7"
	}
	if r.SingleStep {
		r.Status = "paused"
		r.Message = "已完成 G6；下一步 G7"
	}
	r.Revision++
	r.Updated = now()
	r.Data.Batch = nil
	b, _ := json.Marshal(r)
	if _, e = tx.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", r.Status, b, r.ID); e != nil {
		return e
	}
	if chapterCompleted {
		if e = chapterCheckpoint(tx, p, r); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// 审核记录与状态推进同事务提交，并检查执行代次，避免暂停竞争和重启重复采用。
func (s *Store) CommitReview(runID string, epoch int, expected Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := readRun(tx, runID)
	if err != nil {
		return err
	}
	if r.Epoch != epoch || r.Status != "running" {
		return ErrStopped
	}
	if r.Stage != "H" || r.NextNode == "H9" || !r.Data.ReviewApproved {
		return ErrConflict
	}
	p, err := readProject(tx, r.ProjectID)
	if err != nil {
		return err
	}
	v := Review{ID: ID(), Mode: r.Mode, Chapters: r.Data.ReviewChapters, Versions: map[string]string{}, Valid: true, Created: now()}
	for _, ch := range v.Chapters {
		kind := kindFor(r.Mode)
		if expected.ActiveID(ch, kind) != p.ActiveID(ch, kind) {
			return ErrConflict
		}
		v.Versions[fmt.Sprintf("%d:%s", ch, kind)] = p.ActiveID(ch, kind)
	}
	p.Reviews = append(p.Reviews, v)
	r.HumanDecisions = humanDecisions(r)
	p.HumanDecisions = r.HumanDecisions
	p.Revision++
	if err = saveProject(tx, p); err != nil {
		return err
	}
	r.Data = freshData()
	r.Stage = "J"
	r.Node = "H8"
	if r.NextNode != "" {
		r.Stage = "H"
		r.NextNode = "H9"
	}
	if r.SingleStep {
		r.Status = "paused"
		r.Message = "已完成 H8；下一步 H9"
	}
	r.Revision++
	r.Updated = now()
	b, _ := json.Marshal(r)
	if _, err = tx.Exec("UPDATE runs SET status=?,doc=? WHERE id=?", r.Status, b, r.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func adopt(p *Project, c Change, source string) {
	old := p.ActiveID(c.Chapter, c.Kind)
	v := Version{ID: ID(), Chapter: c.Chapter, Kind: c.Kind, Text: c.Text, Summary: c.Summary, SummaryValid: c.Summary != "", Created: now(), Source: source, Parent: old}
	p.Versions = append(p.Versions, v)
	if c.Kind == "draft" {
		p.Chapters[c.Chapter-1].Draft = v.ID
	} else {
		p.Chapters[c.Chapter-1].Outline = v.ID
	}
	for i := range p.Reviews {
		if _, ok := p.Reviews[i].Versions[fmt.Sprintf("%d:%s", c.Chapter, c.Kind)]; ok {
			p.Reviews[i].Valid = false
		}
	}
	for i := range p.Links {
		if old != "" && (p.Links[i].SourceVersion == old || p.Links[i].TargetVersion == old) {
			p.Links[i].Valid = false
		}
	}
}
func kindFor(mode string) string {
	if mode == "forward" {
		return "draft"
	}
	return "outline"
}
func (s *Store) Search(project, query string, limit int) ([]Citation, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	args := []any{project}
	q := "SELECT chapter,kind,version_id,paragraph,text FROM passages WHERE project_id=?"
	if len([]rune(query)) >= 3 {
		q += " AND passages MATCH ?"
		args = append(args, `"`+strings.ReplaceAll(query, `"`, `""`)+`"`)
	} else {
		q += " AND instr(text,?)>0"
		args = append(args, query)
	}
	q += " LIMIT ?"
	args = append(args, limit)
	rows, e := s.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Citation{}
	for rows.Next() {
		var c Citation
		if e = rows.Scan(&c.Chapter, &c.Kind, &c.Version, &c.Paragraph, &c.Text); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
