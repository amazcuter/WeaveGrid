package workbench

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// 已处理的决定单独保留，切章清空工作队列时仍能约束后续生成；兼容旧运行的决定记录。
func humanDecisions(r Run) []Proposal {
	out := append([]Proposal{}, r.HumanDecisions...)
	for _, p := range r.Data.Proposals {
		if p.Reason != "人工确认继续修订" || p.Decision == "pending" {
			continue
		}
		found := false
		for _, old := range out {
			if old.ID == p.ID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, p)
		}
	}
	return out
}

func recordHumanDecision(r *Run, p Proposal) {
	kept := []Proposal{}
	for _, old := range humanDecisions(*r) {
		if old.ID != p.ID {
			kept = append(kept, old)
		}
	}
	r.HumanDecisions = append(kept, p)
}

// 撤回决定时同时撤掉审计记录，否则已撤销的选择仍会约束后续生成。
func forgetHumanDecision(r *Run, id string) {
	kept := []Proposal{}
	for _, old := range r.HumanDecisions {
		if old.ID != id {
			kept = append(kept, old)
		}
	}
	r.HumanDecisions = kept
}

// 请求只携带决定本身；原始问题及证据留在审计记录，避免旧问题的错误前提反复干扰生成。
func decisionContext(r Run) []map[string]any {
	out := []map[string]any{}
	for _, p := range humanDecisions(r) {
		item := map[string]any{"chapter": p.Chapter, "kind": p.Kind, "decision": p.Decision, "instruction": p.Instruction, "source": p.Source}
		if p.Decision == "rejected" {
			item["rejectedProblem"] = p.Problem
		}
		out = append(out, item)
	}
	return out
}

// 同一版本、同一引用位置的重复意见只保留一次，模型换种措辞也不会反复弹窗。
func sameIssue(a, b Proposal) bool {
	if a.Decision == "superseded" || b.Decision == "superseded" {
		return false
	}
	if a.Chapter != b.Chapter || a.Kind != b.Kind {
		return false
	}
	if strings.TrimSpace(a.Problem) == strings.TrimSpace(b.Problem) && (a.Decision == "rejected" || !a.Applied) {
		return true
	}
	if len(a.Evidence) == 0 || len(b.Evidence) == 0 {
		return false
	}
	keys := func(p Proposal) string {
		parts := []string{}
		for _, c := range p.Evidence {
			parts = append(parts, fmt.Sprintf("%d:%s:%s:%d:%s", c.Chapter, c.Kind, c.Version, c.Paragraph, strings.TrimSpace(c.Text)))
		}
		sort.Strings(parts)
		return strings.Join(parts, "|")
	}
	return keys(a) == keys(b)
}

func (e *Engine) enqueue(p Project, r *Run, ch int, kind, cause string) {
	text := ""
	if v := p.Current(ch, kind); v != nil {
		text = v.Text
	}
	for _, b := range r.Data.Batch {
		if b.Chapter == ch && b.Kind == kind {
			text = b.Text
		}
	}
	if text == "" {
		return
	}
	key := fmt.Sprintf("%d:%s:%s:%s", ch, kind, fingerprint(text), cause)
	if r.Data.Seen[key] {
		return
	}
	r.Data.Seen[key] = true
	r.Data.Queue = append(r.Data.Queue, CheckItem{ch, kind, cause})
}
func (e *Engine) relatedQueue(p Project, r *Run, ch int, cause string) {
	wanted := structuralChapters(p, ch)
	for _, ls := range [][]Link{p.Links, r.Data.NewLinks} {
		for _, l := range ls {
			if l.From == ch {
				wanted[l.To] = true
			}
			if l.To == ch {
				wanted[l.From] = true
			}
		}
	}
	order := []int{}
	for c := range wanted {
		order = append(order, c)
	}
	sort.Ints(order)
	for _, c := range order {
		kind := "outline"
		if r.Mode == "forward" && c <= r.Chapter {
			kind = "draft"
		}
		e.enqueue(p, r, c, kind, cause)
	}
}

// revise 保留同步入口供批次验证使用；生产调度每次只调用一个 revisionNode。
func (e *Engine) revise(ctx context.Context, p *Project, r *Run) error {
	for r.Stage == "F" && r.Status == "running" {
		if err := e.step(ctx, p, r); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) revisionNode(ctx context.Context, p *Project, r *Run, n int, next *string) error {
	switch n {
	case 1:
		if r.Data.Seen["initialized"] {
			*next = "F7"
		}
	case 3:
		a, err := e.analysis(ctx, p, r, "F3", "discover", materials(*p, r.Chapter, r.Mode, r.Data.Batch), "本批次候选变化：\n"+asJSON(r.Data.Batch))
		if err != nil {
			return err
		}
		r.Data.SearchTerms = append(r.Data.SearchTerms, a.Queries...)
	case 4:
		for _, q := range capQueries(r.Data.SearchTerms) {
			hits, err := e.search(ctx, *p, q, 10)
			if err != nil {
				return err
			}
			for _, hit := range hits {
				e.enqueue(*p, r, hit.Chapter, hit.Kind, "发现关联:"+q)
			}
		}
		for _, b := range r.Data.Batch {
			e.relatedQueue(*p, r, b.Chapter, fingerprint(b.Text))
		}
		r.Data.Seen["initialized"] = true
		*next = "F7"
	case 5:
		return e.checkQueue(ctx, p, r)
	case 6:
		*next = "F7"
	case 7:
		r.HumanDecisions = humanDecisions(*r)
		for _, prop := range r.Data.Proposals {
			if prop.Decision == "pending" {
				// 先收集本轮其余目标的问题，一次呈现必要取舍，避免每检查两项便弹窗。
				if len(r.Data.Queue) > 0 {
					*next = "F5"
					return nil
				}
				return e.wait(r, "存在需要人工选择的修订，请在审核弹窗处理")
			}
		}
		*next = "F10"
		for _, prop := range r.Data.Proposals {
			if prop.Decision == "accepted" && !prop.Applied {
				*next = "F8"
				break
			}
		}
	case 8:
		return e.reviseOne(ctx, p, r, next)
	case 9:
		return e.verifyRevision(ctx, p, r, next)
	case 10:
		if len(r.Data.Queue) > 0 {
			*next = "F5"
		}
	case 11:
		*next = "G1"
	}
	return nil
}
func (e *Engine) reviseOne(ctx context.Context, p *Project, r *Run, next *string) error {
	for i := range r.Data.Proposals {
		prop := &r.Data.Proposals[i]
		if prop.Applied || prop.Decision != "accepted" {
			continue
		}
		// 按目标累计自动修订次数，不允许模型换一种问题表述便重置上限。
		key := fmt.Sprintf("target:%d:%s", prop.Chapter, prop.Kind)
		hasHuman := false
		for _, item := range r.Data.Proposals {
			if item.Chapter == prop.Chapter && item.Kind == prop.Kind && item.Decision == "accepted" && !item.Applied && item.Reason == "人工确认继续修订" {
				hasHuman = true
			}
		}
		if r.Data.Rounds[key] >= 3 && !hasHuman && r.Data.RevisionTries == 0 {
			prop.Decision = "pending"
			prop.Critical = true
			prop.Reason = "同一问题已自动修订三轮，请选定方案"
			r.NextNode = "F7"
			return e.wait(r, "同一问题反复出现，需要人工取舍")
		}
		current := ""
		base := p.ActiveID(prop.Chapter, prop.Kind)
		if v := p.Current(prop.Chapter, prop.Kind); v != nil {
			current = v.Text
		}
		idx := -1
		for j, b := range r.Data.Batch {
			if b.Chapter == prop.Chapter && b.Kind == prop.Kind {
				current = b.Text
				base = b.Base
				idx = j
				break
			}
		}
		if current == "" {
			r.Data.FutureIdeas = append(r.Data.FutureIdeas, Idea{ID: prop.ID, Chapter: prop.Chapter, Text: prop.Instruction, Required: true, Active: true})
			prop.Applied = true
			prop.Reason = "目标尚未生成，要求保留供后续章节处理"
			return nil
		}
		// 同一目标的待执行意见一次合并修订，避免逐条重写互相覆盖。
		selected := []Proposal{}
		ids := []string{}
		for _, item := range r.Data.Proposals {
			if item.Chapter == prop.Chapter && item.Kind == prop.Kind && item.Decision == "accepted" && !item.Applied {
				selected = append(selected, item)
				ids = append(ids, item.ID)
			}
		}
		keyCall := "revision:" + fingerprint([]any{current, selected, humanDecisions(*r), r.Data.RevisionFeedback, r.Data.RevisionTries})
		task := "目标章节：" + fmt.Sprint(prop.Chapter) + "\n目标内容类型：" + prop.Kind + "\n待修订原文：\n" + current + "\n本次合并执行的要求：\n" + asJSON(selected) + "\n必须同时保留此前已确认的人工决定；新决定与旧创意冲突时，以新决定为准。\n落实检查的补充要求：" + r.Data.RevisionFeedback
		if prop.Kind == "outline" {
			task += "\n目标是章节大纲，保留事件、动机、因果、信息揭示与伏笔安排，概述对白意图，不展开逐句对白、动作和心理描写。"
		}
		text, err := e.call(ctx, p, r, "F8", keyCall, prop.Kind, materials(*p, prop.Chapter, r.Mode, r.Data.Batch), task)
		if err != nil {
			return err
		}
		if text != current {
			change := Change{Chapter: prop.Chapter, Kind: prop.Kind, Base: base, Text: text, Reason: prop.Problem}
			if idx >= 0 {
				r.Data.Batch[idx] = change
			} else {
				r.Data.Batch = append(r.Data.Batch, change)
			}
		}
		r.Data.RevisionIDs = ids
		r.Data.RevisionTries++
		*next = "F9"
		return nil
	}
	*next = "F10"
	return nil
}

// 模型返回文本不等于意见已落实。独立核验后才标记执行成功并向关联章节传播。
func (e *Engine) verifyRevision(ctx context.Context, p *Project, r *Run, next *string) error {
	*next = "F7"
	if len(r.Data.RevisionIDs) == 0 {
		return nil
	}
	selected := []Proposal{}
	for _, prop := range r.Data.Proposals {
		for _, id := range r.Data.RevisionIDs {
			if prop.ID == id {
				selected = append(selected, prop)
				break
			}
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("待核验的修订意见不存在")
	}
	ch, kind := selected[0].Chapter, selected[0].Kind
	text := ""
	if v := p.Current(ch, kind); v != nil {
		text = v.Text
	}
	for _, b := range r.Data.Batch {
		if b.Chapter == ch && b.Kind == kind {
			text = b.Text
		}
	}
	result, err := e.checkRevisionEvidence(ctx, p, r, ch, kind, text, selected)
	if err != nil {
		return err
	}
	if !*result.Satisfied || len(result.Missing) > 0 {
		r.Data.RevisionFeedback = strings.Join(result.Missing, "\n")
		if r.Data.RevisionFeedback == "" {
			r.Data.RevisionFeedback = "落实检查未通过，重新对照已提交要求修订，不可原样返回"
		}
		if r.Data.RevisionTries >= 3 {
			r.NextNode = "F8"
			r.Data.RevisionTries = 0
			return fmt.Errorf("修订经过三次尝试仍未落实，候选已保留且未标记已执行：%s", r.Data.RevisionFeedback)
		}
		*next = "F8"
		return nil
	}
	for i := range r.Data.Proposals {
		for _, id := range r.Data.RevisionIDs {
			if r.Data.Proposals[i].ID == id {
				r.Data.Proposals[i].Applied = true
			}
		}
	}
	r.Data.Rounds[fmt.Sprintf("target:%d:%s", ch, kind)]++
	e.relatedQueue(*p, r, ch, fingerprint(text))
	r.Data.RevisionIDs = nil
	r.Data.RevisionFeedback = ""
	r.Data.RevisionTries = 0
	return nil
}
func (e *Engine) checkQueue(ctx context.Context, p *Project, r *Run) error {
	// 不同补查词和多次修订可排入同一目标；核查时以最新材料快照去重，丢弃过时排队原因。
	queue := []CheckItem{}
	keys := []string{}
	seen := map[string]bool{}
	for _, item := range r.Data.Queue {
		key := fmt.Sprintf("checked:%d:%s:%s", item.Chapter, item.Kind, fingerprint([]any{materials(*p, item.Chapter, r.Mode, r.Data.Batch), humanDecisions(*r)}))
		if seen[key] || r.Data.Seen[key] {
			continue
		}
		seen[key] = true
		queue = append(queue, item)
		keys = append(keys, key)
	}
	r.Data.Queue = queue
	n := p.Settings.CheckConcurrency
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	if r.SingleStep {
		n = 1
	}
	if n > len(r.Data.Queue) {
		n = len(r.Data.Queue)
	}
	items := append([]CheckItem{}, r.Data.Queue[:n]...)
	answers := make([]Analysis, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	// 独立检查读取同一候选快照；单步模式强制一次只核查一个目标。
	for i, item := range items {
		wg.Add(1)
		go func(i int, item CheckItem) {
			defer wg.Done()
			local := *r
			refs := materials(*p, item.Chapter, r.Mode, r.Data.Batch)
			answers[i], errs[i] = e.analysis(ctx, p, &local, "F5", fmt.Sprintf("check-v3:%d:%s:%s", item.Chapter, item.Kind, fingerprint([]any{refs, decisionContext(local)})), refs, "只核查目标第 "+fmt.Sprint(item.Chapter)+" 章的 "+item.Kind+"。\n本批次变化："+asJSON(r.Data.Batch)+"\n本轮正在创作第"+fmt.Sprint(r.Chapter)+"章。新增候选重复已采用章节的关键事件时，优先修正新增候选的章节分工；不能为了迁就新生成的重复情节，要求改写已确认章节。若问题实际属于别章，不针对本目标提出迁就性修改，由该章的核查处理。")
		}(i, item)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	for i, a := range answers {
		issues := []Proposal{}
		for _, v := range a.Issues {
			if v.Chapter == items[i].Chapter && v.Kind == items[i].Kind {
				issues = append(issues, v)
			}
		}
		r.Data.Proposals = append(r.Data.Proposals, e.prepareProposals(*p, *r, issues, false)...)
		r.Data.NewLinks = append(r.Data.NewLinks, validLinks(a.Links, len(p.Chapters))...)
	}
	r.Data.Queue = r.Data.Queue[n:]
	for _, key := range keys[:n] {
		r.Data.Seen[key] = true
	}
	r.Data.CheckIndex += n
	return nil
}
func (e *Engine) reviewNode(ctx context.Context, p *Project, r *Run, n int, next *string) error {
	switch n {
	case 1:
		if r.Data.ReviewChecked {
			*next = "H5"
		}
	case 2:
		index := r.Data.CheckIndex
		if index < len(r.Data.ReviewChapters) {
			ch := r.Data.ReviewChapters[index]
			r.Chapter = ch
			refs := materials(*p, ch, r.Mode, nil)
			a, err := e.analysis(ctx, p, r, "H2", fmt.Sprintf("review:%d:%s", ch, fingerprint([]any{refs, decisionContext(*r)})), refs, "本轮按正序审核。当前重点是第 "+fmt.Sprint(ch)+" 章。审美建议也必须具体并引用原文。")
			if err != nil {
				return err
			}
			r.Data.Proposals = append(r.Data.Proposals, e.prepareProposals(*p, *r, a.Issues, true)...)
			r.Data.CheckIndex++
		}
		if r.Data.CheckIndex < len(r.Data.ReviewChapters) {
			*next = "H2"
		}
	case 3:
		r.Data.ReviewChecked = true
	case 5:
		if !r.Data.ReviewApproved {
			return e.wait(r, "请按正序通读，在审核弹窗处理意见并确认完成")
		}
	case 6:
		*next = "H8"
		for _, v := range r.Data.Proposals {
			if v.Decision == "accepted" && !v.Applied {
				*next = "H7"
				break
			}
		}
	case 7:
		r.Data.ReviewReturn = true
		r.Data.Seen = map[string]bool{}
		*next = "F1"
	case 9:
		*next = "J1"
	}
	return nil
}
