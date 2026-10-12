package workbench

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Store     *Store
	Engine    *Engine
	mutations sync.Mutex
}

func NewServer(s *Store, e *Engine) *Server { return &Server{Store: s, Engine: e} }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	status := 400
	if errors.Is(err, ErrConflict) {
		status = 409
	}
	if errors.Is(err, sql.ErrNoRows) {
		status = 404
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 12<<20))
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("请求内容无效：%w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("请求含多余内容")
	}
	return nil
}
func expected(r *http.Request) int { n, _ := strconv.Atoi(r.Header.Get("If-Match")); return n }

type responseCapture struct {
	header http.Header
	status int
	body   []byte
}

func (c *responseCapture) Header() http.Header { return c.header }
func (c *responseCapture) WriteHeader(s int)   { c.status = s }
func (c *responseCapture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = 200
	}
	c.body = append(c.body, b...)
	return len(b), nil
}

// Handler 将浏览器变更串行化并保存幂等响应；只接受同源页面操作本机服务。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": "0.1.0"})
	})
	mux.HandleFunc("GET /api/workflow", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"nodes": Workflow, "defaults": promptDefaults, "variables": variables, "actions": RunActions, "edges": FlowEdges})
	})
	mux.HandleFunc("POST /api/structure/preview", s.preview)
	mux.HandleFunc("GET /api/layouts/{id}", s.getLayout)
	mux.HandleFunc("PUT /api/layouts/{id}", s.putLayout)
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		ps, e := s.Store.Projects()
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, ps)
	})
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("GET /api/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, e := s.Store.Project(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, p)
	})
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("POST /api/projects/{id}/copy", s.copyProject)
	mux.HandleFunc("GET /api/projects/{id}/checkpoints", s.listCheckpoints)
	mux.HandleFunc("POST /api/projects/{id}/checkpoints", s.createCheckpoint)
	mux.HandleFunc("POST /api/projects/{id}/checkpoints/{checkpoint}/restore", s.restoreCheckpoint)
	mux.HandleFunc("PUT /api/projects/{id}/notes", s.saveNote)
	mux.HandleFunc("DELETE /api/projects/{id}/notes/{note}", s.deleteNote)
	mux.HandleFunc("POST /api/runs/{id}/input", s.answerInput)
	mux.HandleFunc("POST /api/runs/{id}/creative-input", s.submitCreativeInput)

	mux.HandleFunc("PUT /api/projects/{id}/structure", s.adoptStructure)
	mux.HandleFunc("POST /api/projects/{id}/edits", s.edit)
	mux.HandleFunc("GET /api/projects/{id}/search", s.search)
	mux.HandleFunc("GET /api/projects/{id}/export", s.export)
	mux.HandleFunc("GET /api/projects/{id}/prompts/{node}", s.getPrompt)
	mux.HandleFunc("PUT /api/projects/{id}/prompts/{node}", s.putPrompt)
	mux.HandleFunc("POST /api/projects/{id}/prompts/{node}/preview", s.previewPrompt)
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		runs, e := s.Store.Runs(r.URL.Query().Get("project"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, runs)
	})
	mux.HandleFunc("POST /api/runs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectID  string `json:"projectId"`
			Mode       string `json:"mode"`
			SingleStep bool   `json:"singleStep"`
		}
		if e := decode(r, &req); e != nil {
			fail(w, e)
			return
		}
		run, e := s.Engine.StartMode(req.ProjectID, req.Mode, req.SingleStep)
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 201, run)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Run(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/runs/{id}/control", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		if e := decode(r, &req); e != nil {
			fail(w, e)
			return
		}
		v, e := s.Engine.Control(r.PathValue("id"), req.Action, expected(r))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("GET /api/runs/{id}/attempts", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Attempts(r.PathValue("id"))
		if e != nil {
			fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/runs/{id}/decisions", s.decide)
	mux.HandleFunc("GET /api/runs/{id}/events", s.events)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == "GET" {
			mux.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
			writeJSON(w, 403, map[string]string{"error": "不允许跨站修改本机工作台"})
			return
		}
		// 预览不改变项目，也不要求幂等标识。
		if strings.HasSuffix(r.URL.Path, "/preview") {
			mux.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			writeJSON(w, 400, map[string]string{"error": "变更请求缺少幂等标识"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 12<<20))
		if err != nil {
			fail(w, err)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		sum := sha256.Sum256(append([]byte(r.Method+":"+r.URL.Path+":"+r.Header.Get("If-Match")), body...))
		hash := hex.EncodeToString(sum[:])
		s.mutations.Lock()
		defer s.mutations.Unlock()
		var oldHash string
		var status int
		var response []byte
		err = s.Store.db.QueryRow("SELECT body_hash,status,response FROM idempotency WHERE key=?", key).Scan(&oldHash, &status, &response)
		if err == nil {
			if oldHash != hash {
				fail(w, ErrConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write(response)
			return
		}
		capture := &responseCapture{header: make(http.Header)}
		mux.ServeHTTP(capture, r)
		if capture.status == 0 {
			capture.status = 200
		}
		_, err = s.Store.db.Exec("INSERT INTO idempotency VALUES(?,?,?,?)", key, hash, capture.status, capture.body)
		if err != nil {
			fail(w, err)
			return
		}
		for k, vs := range capture.header {
			w.Header()[k] = vs
		}
		w.WriteHeader(capture.status)
		w.Write(capture.body)
	})
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chapters int             `json:"chapters"`
		Seed     int64           `json:"seed"`
		Raw      json.RawMessage `json:"raw"`
	}
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	var st Structure
	var e error
	if len(req.Raw) > 0 && string(req.Raw) != "null" {
		st, e = ImportStructure(req.Raw, req.Chapters)
	} else {
		st, e = GenerateStructure(r.Context(), req.Chapters, req.Seed)
	}
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, st)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string    `json:"name"`
		Structure Structure `json:"structure"`
	}
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "未命名小说"
	}
	if e := ValidateStructure(&req.Structure); e != nil {
		fail(w, e)
		return
	}
	p := NewProject(req.Name, req.Structure)
	if e := s.Store.CreateProject(p); e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 201, p)
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       *string      `json:"name"`
		Archived   *bool        `json:"archived"`
		Ideas      *[]Idea      `json:"ideas"`
		References *[]Reference `json:"references"`
		Settings   *Settings    `json:"settings"`
	}
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	archiveActive := false
	if req.Archived != nil && *req.Archived {
		runs, err := s.Store.Runs(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		for _, run := range runs {
			if run.Status != "completed" && run.Status != "stopped" {
				archiveActive = true
			}
		}
	}
	p, e := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		if req.Name != nil {
			if strings.TrimSpace(*req.Name) == "" {
				return fmt.Errorf("作品名称不能为空")
			}
			p.Name = strings.TrimSpace(*req.Name)
		}
		if req.Archived != nil {
			if *req.Archived {
				if archiveActive {
					return fmt.Errorf("请先终止或完成运行，再归档作品")
				}
			}
			p.Archived = *req.Archived
		}
		if req.Ideas != nil {
			for i := range *req.Ideas {
				v := &(*req.Ideas)[i]
				if v.Chapter < 0 || v.Chapter > len(p.Chapters) {
					return fmt.Errorf("点子章节越界")
				}
				if v.ID == "" {
					v.ID = ID()
				}
			}
			p.Ideas = *req.Ideas
		}
		if req.References != nil {
			for i := range *req.References {
				if (*req.References)[i].ID == "" {
					(*req.References)[i].ID = ID()
				}
			}
			p.References = *req.References
		}
		if req.Settings != nil {
			normalizeSettings(req.Settings)
			for _, role := range []string{"outline", "draft", "check", "summary", "orchestrator"} {
				if err := ValidateModel(req.Settings.Models[role]); err != nil {
					return fmt.Errorf("%s：%w", role, err)
				}
			}
			if req.Settings.CheckConcurrency < 1 || req.Settings.CheckConcurrency > 4 {
				return fmt.Errorf("核查并发必须在 1–4 之间")
			}
			if req.Settings.Models["orchestrator"].ContextChars < 8000 {
				return fmt.Errorf("统筹模型上下文至少需要 8000 字")
			}
			if len([]rune(req.Settings.OrchestratorGuidance)) > 2000 {
				return fmt.Errorf("统筹要求最多 2000 字")
			}
			p.Settings = *req.Settings
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if req.Settings != nil && p.Settings.OrchestratorEnabled {
		if runs, err := s.Store.Runs(p.ID); err == nil {
			for _, run := range runs {
				if run.Status == "waiting_input" && !run.SingleStep {
					_, _ = s.Engine.Control(run.ID, "resume", run.Revision)
				}
			}
		}
	}
	writeJSON(w, 200, p)
}
func (s *Server) adoptStructure(w http.ResponseWriter, r *http.Request) {
	var st Structure
	if e := decode(r, &st); e != nil {
		fail(w, e)
		return
	}
	if e := ValidateStructure(&st); e != nil {
		fail(w, e)
		return
	}
	runs, e := s.Store.Runs(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	if len(runs) > 0 {
		fail(w, fmt.Errorf("已有运行记录，请将新结构保存为新项目"))
		return
	}
	p, e := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		if len(p.Versions) > 0 {
			return fmt.Errorf("已有创作内容，请将新结构保存为新项目")
		}
		if len(p.StructureVersions) == 0 {
			p.StructureVersions = []StructureVersion{{ID: ID(), Created: p.Created, Structure: p.Structure}}
		}
		p.Structure = st
		p.StructureVersions = append(p.StructureVersions, StructureVersion{ID: ID(), Created: now(), Structure: st})
		p.Chapters = nil
		for i := 1; i <= st.Chapters; i++ {
			p.Chapters = append(p.Chapters, Chapter{Number: i})
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var edit PendingEdit
	if e := decode(r, &edit); e != nil {
		fail(w, e)
		return
	}
	runs, e := s.Store.Runs(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	active := false
	for _, run := range runs {
		if run.Status != "completed" && run.Status != "stopped" {
			active = true
		}
	}
	p, e := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		if edit.Chapter < 1 || edit.Chapter > len(p.Chapters) || (edit.Kind != "outline" && edit.Kind != "draft") {
			return fmt.Errorf("章节或内容类型无效")
		}
		if strings.TrimSpace(edit.Text) == "" {
			return fmt.Errorf("内容不能为空")
		}
		if p.ActiveID(edit.Chapter, edit.Kind) != edit.Base {
			return ErrConflict
		}
		edit.ID = ID()
		if active {
			found := false
			for i := range p.PendingEdits {
				if p.PendingEdits[i].Chapter == edit.Chapter && p.PendingEdits[i].Kind == edit.Kind {
					p.PendingEdits[i] = edit
					found = true
				}
			}
			if !found {
				p.PendingEdits = append(p.PendingEdits, edit)
			}
		} else {
			adopt(p, Change{Chapter: edit.Chapter, Kind: edit.Kind, Base: edit.Base, Text: edit.Text}, "manual")
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeJSON(w, 200, []Citation{})
		return
	}
	p, e := s.Store.Project(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	cs, e := s.Engine.search(r.Context(), p, q, 40)
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, cs)
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	p, e := s.Store.Project(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "structure" {
		w.Header().Set("Content-Disposition", "attachment; filename=structure.json")
		writeJSON(w, 200, p.Structure)
		return
	}
	if kind != "outline" && kind != "draft" {
		fail(w, fmt.Errorf("导出类型无效"))
		return
	}
	ext := "md"
	if r.URL.Query().Get("format") == "txt" {
		ext = "txt"
	}
	w.Header().Set("Content-Disposition", "attachment; filename=novel-"+kind+"."+ext)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, c := range p.Chapters {
		if v := p.Current(c.Number, kind); v != nil {
			prefix := ""
			if ext == "md" {
				prefix = "# "
			}
			fmt.Fprintf(w, "%s第 %d 章\n\n%s\n\n", prefix, c.Number, v.Text)
		}
	}
}
func validNode(key string) bool {
	for _, n := range Workflow {
		if n.ID == key {
			return true
		}
		for _, st := range n.Steps {
			if st.ID == key {
				return true
			}
		}
	}
	return false
}
func (s *Server) getPrompt(w http.ResponseWriter, r *http.Request) {
	p, e := s.Store.Project(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	key := r.PathValue("node")
	text, version := promptText(p, key)
	writeJSON(w, 200, map[string]any{"text": text, "version": version, "history": p.Prompts[key], "default": promptDefaults[key]})
}
func (s *Server) putPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	key := r.PathValue("node")
	if !validNode(key) {
		fail(w, fmt.Errorf("节点不存在"))
		return
	}
	if e := ValidatePrompt(req.Text); e != nil {
		fail(w, e)
		return
	}
	p, e := s.Store.UpdateProject(r.PathValue("id"), expected(r), func(p *Project) error {
		p.Prompts[key] = append(p.Prompts[key], PromptVersion{ID(), req.Text, now()})
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) previewPrompt(w http.ResponseWriter, r *http.Request) {
	p, e := s.Store.Project(r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	var req struct {
		Text    string `json:"text"`
		Chapter int    `json:"chapter"`
		Mode    string `json:"mode"`
		RunID   string `json:"runId"`
	}
	if e = decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	if req.Chapter < 1 || req.Chapter > len(p.Chapters) {
		req.Chapter = 1
	}
	if e = ValidatePrompt(req.Text); e != nil {
		fail(w, e)
		return
	}
	key := r.PathValue("node")
	p.Prompts[key] = append(p.Prompts[key], PromptVersion{ID: "preview", Text: req.Text})
	if strings.HasPrefix(key, "D") {
		req.Mode = "backward"
	}
	if strings.HasPrefix(key, "E") {
		req.Mode = "forward"
	}
	refs := materials(p, req.Chapter, req.Mode, nil)
	ideas := []Idea{}
	for _, i := range p.Ideas {
		if i.Active && (i.Chapter == 0 || i.Chapter == req.Chapter) {
			ideas = append(ideas, i)
		}
	}
	state := Run{Chapter: req.Chapter, Mode: req.Mode, Data: freshData()}
	state.HumanDecisions = p.HumanDecisions
	if state.Mode != "forward" {
		state.Mode = "backward"
	}
	if req.RunID != "" {
		if running, er := s.Store.Run(req.RunID); er == nil && running.ProjectID == p.ID && running.Mode == state.Mode {
			state.Data = running.Data
			state.HumanDecisions = running.HumanDecisions
			refs = materials(p, req.Chapter, state.Mode, state.Data.Batch)
		}
	}
	state.Data.ContextIdeas = ideas
	node := key
	if len(key) == 1 {
		for _, n := range Workflow {
			if n.ID == key {
				for _, st := range n.Steps {
					if st.AI {
						node = st.ID
						break
					}
				}
			}
		}
		if key == "D" || key == "E" {
			node = key + "2"
		}
	}
	task := previewTask(node, state)
	if node == "D1" || node == "E1" {
		task = foreshadowTask(missingForeshadows(p, state))
		refs = materials(p, state.Chapter, "backward", nil)
	}
	if node == "D4" {
		task = issueFormat + "\n" + outlineAnalysisTask(p, state)
	}
	system, user, version := requestText(p, &state, node, refs, task)
	role := "check"
	switch node {
	case "D2":
		role = "outline"
	case "E2":
		role = "draft"
	case "F8":
		role = kindFor(state.Mode)
	case "D1", "E1", "G4":
		role = "summary"
	}
	writeJSON(w, 200, map[string]any{"node": node, "modelConfig": p.Settings.Models[role], "promptVersion": version, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}, "note": "完整请求预览使用当前章节快照；动态检索与跨章目标以实际执行记录为准。"})
}
func previewTask(node string, r Run) string {
	switch node {
	case "B7":
		return "检查缺失的资料"
	case "D2", "E2":
		return creationTask(r.Mode)
	case "D4", "E4":
		return issueFormat + "\n候选内容：\n" + asJSON(r.Data.Batch)
	case "F3":
		return issueFormat + "\n本批次候选变化：\n" + asJSON(r.Data.Batch)
	case "F5":
		return issueFormat + fmt.Sprintf("\n只核查目标第 %d 章。\n本批次变化：", r.Chapter) + asJSON(r.Data.Batch)
	case "F8":
		return "已采用的修订要求：\n" + asJSON(r.Data.Proposals)
	case "F9":
		return "核验已采用要求是否落实。" + revisionVerificationFormat + "\n候选：\n" + asJSON(r.Data.Batch)
	case "G4":
		return "原文：\n" + asJSON(r.Data.Batch)
	case "H2":
		return issueFormat + "\n正序审核本章及关联材料"
	default:
		return "此节点由程序执行，不发出模型请求。"
	}
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decisions []struct {
			ID          string `json:"id"`
			Decision    string `json:"decision"`
			Instruction string `json:"instruction"`
		} `json:"decisions"`
		CompleteReview bool       `json:"completeReview"`
		Manual         []Proposal `json:"manual"`
	}
	if e := decode(r, &req); e != nil {
		fail(w, e)
		return
	}
	existing, loadErr := s.Store.Run(r.PathValue("id"))
	if loadErr != nil {
		fail(w, loadErr)
		return
	}
	project, loadErr := s.Store.Project(existing.ProjectID)
	if loadErr != nil {
		fail(w, loadErr)
		return
	}
	run, e := s.Store.UpdateRun(r.PathValue("id"), 0, func(run *Run) error {
		if run.Status != "waiting_input" && run.Status != "paused" {
			return fmt.Errorf("请在等待人工输入或暂停时提交决定")
		}
		if expected(r) > 0 && expected(r) != run.Revision {
			return ErrConflict
		}
		for _, d := range req.Decisions {
			// 撤回把已提交的决定退回待定，是唯一允许回到 pending 的入口。
			withdraw := d.Decision == "pending"
			if !withdraw && d.Decision != "accepted" && d.Decision != "rejected" {
				return fmt.Errorf("决定必须为采用、拒绝或撤回")
			}
			found := false
			for i := range run.Data.Proposals {
				p := &run.Data.Proposals[i]
				if p.ID != d.ID {
					continue
				}
				if p.Applied {
					return fmt.Errorf("已经执行的修订不能更改决定")
				}
				if withdraw {
					// 撤回后该意见回到待办，不能再作为既有决定约束后续生成。
					p.Decision = "pending"
					p.Reason = "人工撤回决定，退回待处理"
					forgetHumanDecision(run, p.ID)
					found = true
					continue
				}
				p.Decision = d.Decision
				p.Source = ""
				if d.Instruction != "" {
					p.Instruction = d.Instruction
				}
				p.Reason = "人工确认继续修订"
				recordHumanDecision(run, *p)
				found = true
			}
			if !found {
				return fmt.Errorf("修订提议不存在")
			}
		}
		for _, m := range req.Manual {
			if run.Stage != "F" && run.Stage != "H" {
				return fmt.Errorf("请在复查或共同审核时提交修订，其他阶段可在章节编辑器保存修改")
			}
			if m.Chapter < 1 || m.Chapter > len(project.Chapters) || m.Instruction == "" || (m.Kind != "outline" && m.Kind != "draft") {
				return fmt.Errorf("人工修改要求不完整")
			}
			m.ID = ID()
			m.Decision = "accepted"
			m.Critical = true
			m.Problem = "人工修改"
			m.Reason = "人工确认继续修订"
			run.Data.Proposals = append(run.Data.Proposals, m)
			recordHumanDecision(run, m)
		}
		if req.CompleteReview {
			if run.Stage != "H" {
				return fmt.Errorf("当前不是共同审核阶段")
			}
			for _, p := range run.Data.Proposals {
				if p.Decision == "pending" {
					return fmt.Errorf("请先处理全部待定意见")
				}
			}
			run.Data.ReviewApproved = true
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	s.Store.Event(run.ID, "run", run)
	writeJSON(w, 200, run)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, e := s.Store.Run(id); e != nil {
		fail(w, e)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, fmt.Errorf("不支持实时事件"))
		return
	}
	after, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if after == 0 {
		after, _ = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	if after == 0 && r.URL.Query().Get("tail") == "1" {
		if err := s.Store.db.QueryRow("SELECT coalesce(max(seq),0) FROM events WHERE run_id=?", id).Scan(&after); err != nil {
			return
		}
		current, err := s.Store.Run(id)
		if err != nil {
			return
		}
		b, _ := json.Marshal(Event{Seq: after, RunID: id, Type: "run", Data: current, Created: now()})
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", after, b)
		flusher.Flush()
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, e := s.Store.Events(id, after)
		if e != nil {
			return
		}
		for _, v := range events {
			b, _ := json.Marshal(v)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", v.Seq, b)
			after = v.Seq
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
