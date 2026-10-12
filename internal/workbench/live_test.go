package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveOpenCode 仅显式启用，使用独立临时项目及合成情节，不读取用户小说内容。
func TestLiveOpenCode(t *testing.T) {
	if os.Getenv("NOVELFLOW_LIVE") != "1" {
		t.Skip("set NOVELFLOW_LIVE=1 to run live model smoke")
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := &HTTPProvider{}
	engine := NewEngine(store, provider)
	defer engine.Close()
	p := NewProject("真实接口隔离冒烟", Structure{Chapters: 3, Lines: [][2]int{{1, 3}}})
	ValidateStructure(&p.Structure)
	for role, m := range p.Settings.Models {
		m.MaxTokens = 1024
		if strings.Contains(m.Model, "kimi-k3") {
			m.MaxTokens = 8192
			m.ReasoningEffort = "low"
		}
		m.TimeoutSeconds = 120
		p.Settings.Models[role] = m
	}
	p.Prompts["D2"] = []PromptVersion{{ID: "live-short", Text: "为第3章写80–120字的完整短大纲：主角归还一本借来的书，与朋友和解。只输出大纲。"}}
	p.Prompts["E2"] = []PromptVersion{{ID: "live-short", Text: "根据提供的大纲写120–180字短正文。只输出正文。"}}
	if err = store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	r := Run{ID: ID(), ProjectID: p.ID, Mode: "backward", Status: "running", Stage: "D", Chapter: 3, Epoch: 1, Revision: 1, Created: now(), Updated: now(), Data: freshData()}
	if err = store.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	outline, err := engine.call(ctx, &p, &r, "D2", "live-outline", "outline", nil, "这是独立接口验收，请完成短大纲。")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(outline)) < 20 {
		t.Fatal("outline too short")
	}
	r.Mode = "forward"
	draft, err := engine.call(ctx, &p, &r, "E2", "live-draft", "draft", nil, "已采用的短大纲：\n"+outline)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(draft)) < 30 {
		t.Fatal("draft too short")
	}
	refs := citations(Version{ID: "live-draft", Chapter: 3, Kind: "draft", Text: draft}, false)
	a, err := engine.analysis(ctx, &p, &r, "F5", "live-check", refs, "对照原文核查是否存在明确的内部矛盾。没有明确问题时返回空 issues 数组。")
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.call(ctx, &p, &r, "O1", "live-orchestrator", "orchestrator", nil, "本次是C2创意确认：短故事讲主角归还借来的书，与朋友和解。请给出一项简短创作方向，返回{\"reason\":\"判断理由\",\"memory\":\"后续需记住的决定\",\"creativeDirection\":\"创作方向\"}。", func(text string) error { _, err := validateOrchestratorReply(text, "C2", nil); return err })
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.call(ctx, &p, &r, "O2", "live-memory", "orchestrator", nil, "把这段记忆压缩成一句：主角借书后失约，决定亲自归还，主动说明失约缘由，朋友愿意倾听。返回{\"summary\":\"一句压缩记忆\"}。", func(text string) error {
		var v struct {
			Summary string `json:"summary"`
		}
		if err := parseJSON(text, &v); err != nil {
			return err
		}
		if v.Summary == "" {
			return fmt.Errorf("empty memory")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	as, err := store.Attempts(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, v := range as {
		total += v.Usage.Total
		if v.Status != "succeeded" {
			t.Fatal("unsuccessful attempt")
		}
		if strings.Contains(v.Prompt, "Bearer ") {
			t.Fatal("credential in attempt")
		}
	}
	t.Logf("live smoke passed: outline=%d chars, draft=%d chars, checkIssues=%d, attempts=%d, totalTokens=%d", len([]rune(outline)), len([]rune(draft)), len(a.Issues), len(as), total)
	// 临时数据库会在测试后清理；显式指定文件时保存原文和实际请求，便于人工评读。
	if outputPath := os.Getenv("NOVELFLOW_LIVE_OUTPUT"); outputPath != "" {
		output, err := json.MarshalIndent(map[string]any{
			"generatedAt": now(), "outline": outline, "draft": draft,
			"analysis": a, "attempts": as, "totalTokens": total,
		}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(outputPath, output, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved live sample: %s", outputPath)
	}
}
