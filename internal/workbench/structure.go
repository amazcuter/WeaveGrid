package workbench

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

//go:embed generator/*.py
var generatorFiles embed.FS

// GenerateStructure 在短时独立进程中运行原算法，不改变原脚本所在目录。
func GenerateStructure(ctx context.Context, chapters int, seed int64) (Structure, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var s Structure
	if chapters < 3 || chapters > 2000 {
		return s, fmt.Errorf("自动生成章节数必须在 3–2000 之间")
	}
	dir, err := os.MkdirTemp("", "novelflow-generator-")
	if err != nil {
		return s, err
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"original.py", "runner.py"} {
		b, _ := generatorFiles.ReadFile("generator/" + name)
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return s, err
		}
	}
	cmd := exec.CommandContext(ctx, "python3", "-I", "-B", filepath.Join(dir, "runner.py"), "--chapters", fmt.Sprint(chapters), "--seed", fmt.Sprint(seed))
	b, err := cmd.Output()
	if err != nil {
		return s, fmt.Errorf("生成失败，请检查 Python 3：%w", err)
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, ValidateStructure(&s)
}

func ImportStructure(raw json.RawMessage, chapters int) (Structure, error) {
	var s Structure
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return s, fmt.Errorf("请选择结构 JSON")
	}
	if raw[0] == '[' {
		s.Chapters = chapters
		s.GeneratorVersion = "import-lines"
		s.MinGap = 2
		var pairs [][]int
		if err := json.Unmarshal(raw, &pairs); err != nil {
			return s, err
		}
		for _, pair := range pairs {
			if len(pair) != 2 {
				return s, fmt.Errorf("每组章节关系必须恰好包含两个章号")
			}
			s.Lines = append(s.Lines, [2]int{pair[0], pair[1]})
		}
	} else {
		if err := json.Unmarshal(raw, &s); err != nil {
			return s, err
		}
		if s.Chapters == 0 {
			s.Chapters = chapters
		}
		if s.GeneratorVersion == "" {
			s.GeneratorVersion = "import-structure"
		}
	}
	// 对完整旧格式只解码已声明的结构字段，随机事件和线索字段会被丢弃。
	return s, ValidateStructure(&s)
}

func ValidateStructure(s *Structure) error {
	if s.Chapters < 1 || s.Chapters > 2000 {
		return fmt.Errorf("章节数必须在 1–2000 之间")
	}
	// 旧文件中的 min_gap 文案不作为算法约束；统一使用原算法实际跨度。
	s.MinGap = 2
	valid := func(a, b int) bool { return a >= 1 && b <= s.Chapters && b-a >= 2 }
	seen := map[[2]int]bool{}
	if len(s.Threads) > 0 {
		ids := map[string]bool{}
		for i := range s.Threads {
			t := &s.Threads[i]
			if !valid(t.Plant, t.Payoff) {
				return fmt.Errorf("伏笔 %s 起止章节无效", t.ID)
			}
			if t.ID == "" {
				t.ID = fmt.Sprintf("T%03d", i+1)
			}
			if ids[t.ID] {
				return fmt.Errorf("伏笔编号重复：%s", t.ID)
			}
			ids[t.ID] = true
			t.Span = t.Payoff - t.Plant
			for _, c := range t.Reminds {
				if c <= t.Plant || c >= t.Payoff {
					return fmt.Errorf("伏笔 %s 提醒位置必须在起止章节之间", t.ID)
				}
			}
			seen[[2]int{t.Plant, t.Payoff}] = true
		}
		// 若同时给出 lines，则要求它与完整记录一致，避免界面和执行器采用不同图。
		if len(s.Lines) > 0 {
			given := map[[2]int]bool{}
			for _, e := range s.Lines {
				if given[e] {
					return fmt.Errorf("重复连线：%v", e)
				}
				given[e] = true
			}
			if len(given) != len(seen) {
				return fmt.Errorf("lines 与 threads 的章节关系不一致")
			}
			for e := range given {
				if !seen[e] {
					return fmt.Errorf("lines 与 threads 的章节关系不一致")
				}
			}
		}
	} else {
		for i, e := range s.Lines {
			if !valid(e[0], e[1]) {
				return fmt.Errorf("第 %d 条连线越界或方向错误", i+1)
			}
			if seen[e] {
				return fmt.Errorf("重复连线：%v", e)
			}
			seen[e] = true
			s.Threads = append(s.Threads, Thread{ID: fmt.Sprintf("T%03d", i+1), Plant: e[0], Payoff: e[1], Span: e[1] - e[0], Reminds: []int{}, Tag: "导入"})
		}
	}
	s.Lines = [][2]int{}
	for e := range seen {
		s.Lines = append(s.Lines, e)
	}
	sort.Slice(s.Lines, func(i, j int) bool {
		if s.Lines[i][0] == s.Lines[j][0] {
			return s.Lines[i][1] < s.Lines[j][1]
		}
		return s.Lines[i][0] < s.Lines[j][0]
	})
	last := 0
	for _, a := range s.Arcs {
		if a[0] < 1 || a[1] < a[0] || a[1] > s.Chapters || a[0] <= last {
			return fmt.Errorf("剧情单元范围无效或重叠")
		}
		last = a[1]
	}
	for _, n := range s.Nodes {
		if n.Ch < 1 || n.Ch > s.Chapters {
			return fmt.Errorf("转折节点越界")
		}
	}
	if s.Arcs == nil {
		s.Arcs = [][2]int{}
	}
	if s.Nodes == nil {
		s.Nodes = []Turning{}
	}
	if s.Threads == nil {
		s.Threads = []Thread{}
	}
	if s.FixedReminders == nil {
		s.FixedReminders = []string{}
	}
	return nil
}
