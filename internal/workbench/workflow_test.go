package workbench

import (
	"strings"
	"testing"
)

func TestRenderPromptStructuredChildOverridesParentOutputMode(t *testing.T) {
	p := NewProject("提示词继承测试", Structure{Chapters: 1})
	p.Prompts["E"] = []PromptVersion{{ID: "parent", Text: "你是正文作者，只输出完整正文。"}}
	p.Prompts["E4"] = []PromptVersion{{ID: "child", Text: "只输出合法 JSON 对象，不输出正文。"}}

	system, _ := RenderPrompt(p, "E4", nil)
	if !strings.Contains(system, "公共节点提示词（仅作为角色背景") {
		t.Fatal("公共提示词没有被标记为背景")
	}
	if !strings.Contains(system, "当前子步骤提示词（优先执行") {
		t.Fatal("子步骤提示词没有被标记为优先")
	}
	if strings.Index(system, "当前子步骤提示词") < strings.Index(system, "公共节点提示词") {
		t.Fatal("子步骤优先级说明应放在公共提示词之后")
	}
}
