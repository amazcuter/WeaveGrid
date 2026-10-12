// 浏览器验收专用服务。模拟模型只存在于这个独立入口，不进入正式启动流程。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"weavegrid/internal/workbench"
	"weavegrid/web"
)

type fixture struct {
	offered        atomic.Bool
	asked          atomic.Bool
	disconnected   atomic.Bool
	askInput       bool
	disconnectOnce bool
}

func (f *fixture) Generate(ctx context.Context, r workbench.ModelRequest, chunk func(string, string)) (workbench.ModelResult, error) {
	if r.Node == "B7" && f.disconnectOnce && f.disconnected.CompareAndSwap(false, true) {
		return workbench.ModelResult{}, io.EOF
	}
	text := `{"queries":[],"links":[],"issues":[]}`
	if r.Node == "B7" && f.askInput && f.asked.CompareAndSwap(false, true) {
		text = `{"queries":[],"inputs":["主角希望保留的名字是什么？"]}`
	}
	switch r.Node {
	case "O1":
		var input struct {
			Proposals []workbench.Proposal `json:"proposals"`
		}
		json.NewDecoder(strings.NewReader(r.User)).Decode(&input)
		decisions := []map[string]string{}
		for _, prop := range input.Proposals {
			decisions = append(decisions, map[string]string{"id": prop.ID, "decision": "accepted", "reason": "原文缺少因果衔接，按建议补足。"})
		}
		b, _ := json.Marshal(map[string]any{"reason": "由统筹按已有创意推进当前章", "memory": "主角主动面对误会，后续保留因果衔接。", "creativeDirection": "用主角的选择推动本章变化。", "answer": "创作选择：主角取名江砚。", "decisions": decisions})
		text = string(b)
	case "O2":
		text = `{"summary":"主角江砚主动面对误会；保留既有创意与人物选择，后章自然承接。"}`
	case "D1", "E1", "D4":
		// 仅维护模拟接口格式，不调用真实模型。
		threads := []workbench.Thread{}
		if _, raw, ok := strings.Cut(r.User, "需要提示的伏笔："); ok {
			json.NewDecoder(strings.NewReader(raw)).Decode(&threads)
		}
		hints := []map[string]string{}
		for _, thread := range threads {
			hints = append(hints, map[string]string{"threadId": thread.ID, "text": "一件旧物留下未解疑问。"})
		}
		b, _ := json.Marshal(map[string]any{"queries": []string{}, "links": []any{}, "issues": []any{}, "foreshadows": hints})
		text = string(b)
	case "D2", "E2":
		text = "主角留下了线索。\n\n雨停后，他把借来的书放回窗台。朋友在门外等他，两人终于说清了过去的误会。"
	case "F8":
		text = "主角留下了线索。\n\n他把来龙去脉告诉朋友，对方终于理解了当初的选择。这是采用人工意见后的新版本。"
	case "F9":
		text = `{"satisfied":true,"evidence":["主角留下了线索。"],"missing":[]}`
	case "G4":
		text = `{"summary":"主角留下线索，化解了与朋友之间的误会。","quote":"主角留下了线索。"}`
	}
	if r.Node == "F5" && strings.Contains(r.User, "当前章节：2；") && strings.Contains(r.User, "只核查目标第 3 章") && f.offered.CompareAndSwap(false, true) {
		start := strings.Index(r.User, "）：\n")
		end := strings.Index(r.User, "\n参考资料：")
		refs := []workbench.Citation{}
		if start >= 0 && end > start {
			json.Unmarshal([]byte(r.User[start+len("）：\n"):end]), &refs)
		}
		for _, c := range refs {
			if c.Chapter == 3 && c.Kind == "outline" && !strings.HasPrefix(c.Version, "candidate:") {
				b, _ := json.Marshal(map[string]any{"issues": []workbench.Proposal{{Chapter: 3, Kind: "outline", Problem: "是否让朋友提前知道真相？", Instruction: "明确主角主动告知朋友，而不是朋友无缘由猜到。", Critical: true, Evidence: []workbench.Citation{c}}}})
				text = string(b)
				break
			}
		}
	}
	runes := []rune(text)
	partial := ""
	for i := 0; i < len(runes); i += 12 {
		end := i + 12
		if end > len(runes) {
			end = len(runes)
		}
		piece := string(runes[i:end])
		partial += piece
		chunk(piece, "")
		select {
		case <-ctx.Done():
			return workbench.ModelResult{Text: partial}, ctx.Err()
		case <-time.After(35 * time.Millisecond):
		}
	}
	return workbench.ModelResult{Text: text, Usage: workbench.Usage{Input: 100, Output: len(runes), Total: 100 + len(runes)}}, nil
}
func main() {
	addr := flag.String("addr", "127.0.0.1:8741", "验收地址")
	data := flag.String("data", "artifacts/browser-smoke", "独立验收数据")
	askInput := flag.Bool("ask-input", false, "模拟缺少用户资料")
	disconnectOnce := flag.Bool("disconnect-once", false, "模拟一次 EOF")
	flag.Parse()
	os.MkdirAll(*data, 0700)
	store, e := workbench.OpenStore(filepath.Join(*data, "test.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer store.Close()
	engine := workbench.NewEngine(store, &fixture{askInput: *askInput, disconnectOnce: *disconnectOnce})
	defer engine.Close()
	api := workbench.NewServer(store, engine)
	mux := http.NewServeMux()
	mux.Handle("/api/", api.Handler())
	assets, _ := fs.Sub(web.Files, "dist")
	mux.Handle("/", http.FileServerFS(assets))
	fmt.Println("浏览器验收：模拟模型、独立数据库", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
