package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const revisionVerificationFormat = `返回 JSON：{"satisfied":true或false,"evidence":[{"quote":"从修订后候选连续复制的原文","reason":"这段原文如何落实要求"}],"missing":["尚未落实或仍矛盾的具体位置与纠正要求"]}。quote只放连续原文，不加“第1点”、引号外的说明或省略号；解释放reason。通过时至少提供一条原文证据，不通过时必须说明具体缺项。`

type revisionVerification struct {
	Satisfied *bool             `json:"satisfied"`
	Evidence  []json.RawMessage `json:"evidence"`
	Missing   []string          `json:"missing"`
}

func parseRevisionVerification(answer, text string) (revisionVerification, error) {
	var result revisionVerification
	if err := parseJSON(answer, &result); err != nil || result.Satisfied == nil {
		return result, fmt.Errorf("核验结果缺少有效JSON或satisfied布尔值")
	}
	missing := []string{}
	for _, item := range result.Missing {
		if strings.TrimSpace(item) != "" {
			missing = append(missing, item)
		}
	}
	result.Missing = missing
	if !*result.Satisfied || len(result.Missing) > 0 {
		if len(result.Missing) == 0 {
			return result, fmt.Errorf("判定未落实时，missing必须写明具体缺项")
		}
		return result, nil
	}
	if len(result.Evidence) == 0 {
		return result, fmt.Errorf("判定通过时缺少原文引用，请在evidence中提供quote")
	}
	// 只容忍换行、缩进差异，不做近似语义匹配，防止把模型编写的说明当作原文。
	normalize := func(s string) string { return strings.Join(strings.Fields(s), "") }
	source := normalize(text)
	for i, raw := range result.Evidence {
		var quote string
		// 兼容旧的纯字符串证据；新格式把引用和解释分开，避免整段说明做子串匹配。
		if err := json.Unmarshal(raw, &quote); err != nil {
			var item struct {
				Quote string `json:"quote"`
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return result, fmt.Errorf("第%d条证据格式无效，请使用quote和reason对象", i+1)
			}
			quote = item.Quote
		}
		quote = normalize(quote)
		if quote == "" || !strings.Contains(source, quote) {
			return result, fmt.Errorf("第%d条quote不能在候选中找到；请重新连续摘录，不添加省略号、编号或解释，解释放reason", i+1)
		}
	}
	return result, nil
}

// 引用格式错误只重试核验，不回到F8重写，更不消耗正文修订次数。
func (e *Engine) checkRevisionEvidence(ctx context.Context, p *Project, r *Run, chapter int, kind, text string, selected []Proposal) (revisionVerification, error) {
	task := "目标章节：" + fmt.Sprint(chapter) + "；类型：" + kind + "\n本次执行的意见：" + asJSON(selected) + "\n修订后完整候选：\n" + text + "\n检查本次意见和相关的既有人工决定是否已落实，后提交的人工决定覆盖冲突的旧意见。不要提出新审美要求，不要要求用户重新选择，不把建议字数当作硬性验收条件。\n" + revisionVerificationFormat
	base := fingerprint([]any{text, selected, decisionContext(*r), r.Data.RevisionTries, r.Epoch})
	var result revisionVerification
	_, err := e.call(ctx, p, r, "F9", "verify-v3:"+base, "check", materials(*p, chapter, r.Mode, r.Data.Batch), task, func(answer string) error {
		var invalid error
		result, invalid = parseRevisionVerification(answer, text)
		return invalid
	})
	if err != nil {
		r.NextNode = "F9"
	}
	return result, err
}
