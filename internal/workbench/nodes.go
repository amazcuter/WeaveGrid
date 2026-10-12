package workbench

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// NextNode 是持久化的执行游标。一次 step 只执行一个小节点；单步与连续运行共用此路径。
// Node 表示最近执行的节点，NextNode 表示下一步，暂停、重启和检查点恢复均不跳过未完成工作。
func (e *Engine) step(ctx context.Context, p *Project, r *Run) error {
	if r.NextNode == "" {
		r.NextNode = r.Stage + "1"
	} // 旧数据从本阶段入口恢复，成功模型请求仍可复用。
	// 兼容旧检查点中的文风准备游标；移除的子流不再执行或请求模型。
	if strings.HasPrefix(r.NextNode, "I") {
		r.NextNode = "J1"
	}
	id := r.NextNode
	if len(id) < 2 || !validNode(id) {
		return fmt.Errorf("无效执行游标：%s", id)
	}
	r.Stage = id[:1]
	if err := e.node(r, id); err != nil {
		return err
	}
	n, _ := strconv.Atoi(id[1:])
	next := fmt.Sprintf("%s%d", r.Stage, n+1)
	var err error
	switch r.Stage {
	case "A":
		switch n {
		case 1:
			applied, er := e.applyPending(p, r)
			if er != nil {
				return er
			}
			if applied {
				next = "F1"
			}
		case 2, 3:
			err = ValidateStructure(&p.Structure)
		case 8:
			next = "J1"
		}
		if n == 3 {
			next = "A5"
		}
	case "B":
		err = e.materialNode(ctx, p, r, n, &next)
	case "C":
		// 即使有预存创意也要逐章确认。恢复旧版 C4/C6 时同样不能绕过人工交付。
		input := r.Data.CreativeInput
		if input == nil || input.Chapter != r.Chapter || input.Cycle != r.Cycle {
			input = &CreativeInput{ID: ID(), Chapter: r.Chapter, Cycle: r.Cycle, Ideas: chapterIdeas(*p, r.Chapter)}
			r.Data.CreativeInput = input
			r.Data.ContextIdeas = nil
		}
		if n >= 2 && input.SubmittedAt == "" {
			r.NextNode = "C2"
			return e.wait(r, fmt.Sprintf("请填写或确认第 %d 章的人工创意，提交后才会继续", r.Chapter))
		}
		if n == 4 {
			// 只交付用户此次确认的快照，不被运行中其他编辑静默替换。
			r.Data.ContextIdeas = activeIdeas(input.Ideas)
		}
		if n == 6 {
			next = "D1"
			if r.Mode == "forward" {
				next = "E1"
			}
		}
	case "D", "E":
		err = e.creationNode(ctx, p, r, n, &next)
	case "F":
		err = e.revisionNode(ctx, p, r, n, &next)
	case "G":
		if n == 4 {
			err = e.summarizeBatch(ctx, p, r, &next)
		}
		if n == 5 && p.Settings.EmbeddingURL != "" {
			provider, ok := e.Provider.(EmbeddingProvider)
			if !ok {
				return fmt.Errorf("当前提供方不支持向量服务")
			}
			for i := range r.Data.Batch {
				c := &r.Data.Batch[i]
				c.Embeddings, err = provider.Embed(ctx, p.Settings, paragraphs(c.Text))
				if err != nil {
					return err
				}
			}
		}
		if n == 6 {
			// 有效版本切换与游标、单步暂停同一事务提交；断电也不会重复采用。
			if err = e.Store.CommitBatch(r.ID, r.Epoch, r.Data.Batch, r.Data.NewLinks); err != nil {
				return err
			}
			return e.committed(r, id)
		}
		if n == 7 {
			next = "J1"
		}
	case "H":
		if n == 8 {
			if err = e.Store.CommitReview(r.ID, r.Epoch, *p); err != nil {
				return err
			}
			return e.committed(r, id)
		}
		err = e.reviewNode(ctx, p, r, n, &next)
	case "J":
		err = e.scheduleNode(p, r, n, &next)
	}
	if err != nil {
		return err
	}
	if r.Status == "waiting_input" || r.OrchestratorGate != "" {
		return nil
	}
	r.NextNode = next
	if next != "" {
		r.Stage = next[:1]
	}
	if r.SingleStep && r.Status == "running" {
		r.Status = "paused"
		r.Message = "已完成 " + id + "；下一步 " + next
	}
	if err = e.save(r); err != nil {
		return err
	}
	return e.Store.Event(r.ID, "node", map[string]any{"node": id, "chapter": r.Chapter, "status": "succeeded", "cycle": r.Cycle})
}

func (e *Engine) committed(r *Run, id string) error {
	saved, err := e.Store.Run(r.ID)
	if err != nil {
		return err
	}
	*r = saved
	e.Store.Event(r.ID, "node", map[string]any{"node": id, "chapter": r.Chapter, "status": "succeeded", "cycle": r.Cycle})
	e.Store.Event(r.ID, "run", r)
	return e.Store.Event(r.ID, "project", map[string]string{"projectId": r.ProjectID})
}
func (e *Engine) materialNode(ctx context.Context, p *Project, r *Run, n int, next *string) error {
	switch n {
	case 1:
		applied, err := e.applyPending(p, r)
		if err != nil {
			return err
		}
		if applied {
			*next = "F1"
		}
	case 6:
		r.Data.Materials = materials(*p, r.Chapter, r.Mode, nil)
	case 7:
		var answer struct {
			Queries []string `json:"queries"`
			Inputs  []string `json:"inputs"`
		}
		_, err := e.call(ctx, p, r, "B7", "materials", "check", r.Data.Materials, "检查缺失的资料；只能由用户决定的必要信息放入 inputs，允许作者自由构思的内容不要提问。", func(text string) error {
			answer.Queries, answer.Inputs = nil, nil
			if invalid := parseJSON(text, &answer); invalid != nil {
				return invalid
			}
			if answer.Queries == nil {
				return fmt.Errorf("检索结果缺少 queries 数组")
			}
			return nil
		})
		if err != nil {
			return err
		}
		r.Data.SearchTerms = capQueries(answer.Queries)
		if questions := capQueries(answer.Inputs); len(questions) > 0 {
			r.Data.Input = &InputRequest{ID: ID(), Questions: questions}
		}
	case 8:
		if r.Data.Input != nil && strings.TrimSpace(r.Data.Input.Answer) == "" {
			return e.wait(r, "需要你补充必要资料，请回答弹窗中的问题")
		}
	case 9:
		for _, q := range r.Data.SearchTerms {
			hits, err := e.search(ctx, *p, q, 12)
			if err != nil {
				return err
			}
			r.Data.Materials = mergeCitations(r.Data.Materials, hits)
		}
	case 10:
		*next = "C1"
	}
	return nil
}

// 实际请求和预览共用任务：倒序编纲问前因，正序写作问下一步，避免方向混淆。
func creationTask(mode string) string {
	if mode == "backward" {
		return "请先看已确认的后续章节，想一想：这之前应该发生什么？据此写本章大纲，让本章结尾自然造成下一章开头的处境。尚未生成的更早章节可继续向前安排，不必在本章兑现全书创意或重复后章情节。"
	}
	return "承接已写前文，根据本章大纲生成完整正文。结构只作为计划，不需要逐项解释检查结果。"
}

func (e *Engine) creationNode(ctx context.Context, p *Project, r *Run, n int, next *string) error {
	stage := r.Stage
	kind := kindFor(r.Mode)
	switch n {
	case 1:
		return e.prepareForeshadows(ctx, p, r)
	case 2:
		// 兼容已停在D2/E2的旧运行，生成前补齐回收章派生的提示。
		if err := e.prepareForeshadows(ctx, p, r); err != nil {
			return err
		}
		// 到生成安全点再次补齐相邻材料，旧运行已经越过B6也会采用新的两章窗口。
		valid := []Citation{}
		for _, c := range r.Data.Materials {
			if validateEvidence(*p, *r, c) {
				valid = append(valid, c)
			}
		}
		r.Data.Materials = mergeCitations(materials(*p, r.Chapter, r.Mode, nil), valid)
		task := creationTask(r.Mode)
		text, err := e.call(ctx, p, r, stage+"2", "generate-foreshadows:"+fingerprint([]any{r.Data.Materials, foreshadowMaterials(*p, *r, r.Data.Materials), task}), kind, r.Data.Materials, task)
		if err != nil {
			return err
		}
		r.Data.Generated = text
	case 3:
		r.Data.Batch = []Change{{Chapter: r.Chapter, Kind: kind, Base: p.ActiveID(r.Chapter, kind), Text: r.Data.Generated, Reason: "自由生成"}}
	case 4:
		key := "extract-foreshadows:" + fingerprint(r.Data.Generated)
		task := outlineAnalysisTask(*p, *r)
		refs := materials(*p, r.Chapter, r.Mode, r.Data.Batch)
		var a Analysis
		var err error
		if kind == "outline" {
			a, err = e.callForeshadowJSON(ctx, p, r, stage+"4", "check", refs, issueFormat+"\n"+task, payoffThreads(p.Structure, r.Chapter))
		} else {
			a, err = e.analysis(ctx, p, r, stage+"4", key, refs, task)
		}
		if err != nil {
			return err
		}
		r.Data.Analysis = &a
	case 5:
		if a := r.Data.Analysis; a != nil {
			if kind == "outline" {
				// 提示直接写进大纲候选，再进入F审核；G保存阶段不偷偷补写内容。
				for i := range r.Data.Batch {
					c := &r.Data.Batch[i]
					if c.Chapter == r.Chapter && c.Kind == "outline" {
						c.Foreshadows = a.Foreshadows
						c.Text = formatOutline(*p, *r, *c)
						c.Summary = ""
					}
				}
			}
			r.Data.SearchTerms = a.Queries
			r.Data.NewLinks = validLinks(a.Links, len(p.Chapters))
			r.Data.Proposals = e.prepareProposals(*p, *r, a.Issues, false)
		}
	case 6:
		r.Data.Generated = ""
		r.Data.Analysis = nil
		*next = "F1"
	}
	return nil
}
func (e *Engine) summarizeBatch(ctx context.Context, p *Project, r *Run, next *string) error {
	for i := range r.Data.Batch {
		c := &r.Data.Batch[i]
		// 索引从最终大纲的可见提示行提取，审核中改过提示时以大纲为准。
		if c.Kind == "outline" {
			c.Foreshadows = outlineForeshadows(*p, *c)
		}
		if c.Summary != "" {
			continue
		}
		key := fmt.Sprintf("summary:%d:%s:%s", c.Chapter, c.Kind, fingerprint(c.Text))
		var v struct {
			Summary string `json:"summary"`
		}
		_, err := e.call(ctx, p, r, "G4", key, "summary", nil, "原文：\n"+c.Text, func(text string) error {
			v.Summary = ""
			if invalid := parseJSON(text, &v); invalid != nil {
				return invalid
			}
			if strings.TrimSpace(v.Summary) == "" {
				return fmt.Errorf("摘要为空")
			}
			return nil
		})
		if err != nil {
			return err
		}
		c.Summary = v.Summary
		// 同一小节点涉及多章时，一次单步只生成一份摘要，进度留在 G4。
		for _, b := range r.Data.Batch {
			if b.Summary == "" {
				*next = "G4"
				break
			}
		}
		break
	}
	return nil
}
func (e *Engine) scheduleNode(p *Project, r *Run, n int, next *string) error {
	switch n {
	case 1:
		applied, err := e.applyPending(p, r)
		if err != nil {
			return err
		}
		if applied {
			*next = "F1"
			return nil
		}
		if r.Data.ReviewReturn {
			r.Data = freshData()
			r.Cycle++
		}
	case 2:
		scopes := [][]int{}
		if p.Settings.ArcReviews {
			for _, a := range p.Structure.Arcs {
				scope := []int{}
				for c := a[0]; c <= a[1]; c++ {
					scope = append(scope, c)
				}
				if completeScope(*p, scope, r.Mode) {
					scopes = append(scopes, scope)
				}
			}
		}
		all := []int{}
		for _, c := range p.Chapters {
			all = append(all, c.Number)
		}
		if completeScope(*p, all, r.Mode) {
			scopes = append(scopes, all)
		}
		*next = "J4"
		for _, scope := range scopes {
			if !reviewValid(*p, scope, r.Mode) {
				r.Data = freshData()
				r.Data.ReviewChapters = scope
				r.Cycle++
				*next = "J3"
				break
			}
		}
	case 3:
		*next = "H1"
	case 4:
		r.Data.NextChapter = 0
		for i := 1; i <= len(p.Chapters); i++ {
			ch := i
			if r.Mode == "backward" {
				ch = len(p.Chapters) - i + 1
			}
			if p.ActiveID(ch, kindFor(r.Mode)) == "" {
				r.Data.NextChapter = ch
				break
			}
		}
		if r.Data.NextChapter == 0 {
			*next = "J7"
		}
	case 5:
		r.Chapter = r.Data.NextChapter
		r.Cycle++
		r.Data = freshData()
	case 6:
		*next = "B1"
	case 8:
		r.Status = "completed"
		r.Message = "全部内容与最终审核已完成"
		*next = ""
	}
	return nil
}
