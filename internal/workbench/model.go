package workbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 模型调用使用自己的连接池，避免与本机其他 HTTP 请求相互干扰。
var modelHTTPClient = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 60 * time.Second
	t.IdleConnTimeout = 30 * time.Second
	return &http.Client{Transport: t}
}()

type ModelRequest struct {
	Node            string
	Config          ModelConfig
	System          string
	User            string
	Session         string
	FreshConnection bool
}
type ModelResult struct {
	Text      string
	Reasoning string
	Usage     Usage
}
type ModelProvider interface {
	Generate(context.Context, ModelRequest, func(string, string)) (ModelResult, error)
}
type HTTPProvider struct {
	Client         *http.Client
	CredentialFile string
}
type ModelError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *ModelError) Error() string { return e.Message }

// Credentials 仅解析后端配置的引用，不向浏览器返回密钥值。
func (h *HTTPProvider) Credentials(ref string) (string, error) {
	if ref == "" || ref == "none" {
		return "", nil
	}
	if strings.HasPrefix(ref, "env:") {
		key := os.Getenv(strings.TrimPrefix(ref, "env:"))
		if key == "" {
			return "", fmt.Errorf("凭据环境变量未设置")
		}
		return key, nil
	}
	if ref != "opencode-go" {
		return "", fmt.Errorf("凭据使用 opencode-go、none 或 env:变量名")
	}
	if key := os.Getenv("OPENCODE_API_KEY"); key != "" {
		return key, nil
	}
	path := h.CredentialFile
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".local/share/opencode/auth.json")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return "", fmt.Errorf("未找到 OpenCode 凭据，请配置 OPENCODE_API_KEY")
	}
	var data map[string]struct {
		Key string `json:"key"`
	}
	if e = json.Unmarshal(b, &data); e != nil {
		return "", fmt.Errorf("OpenCode 凭据格式无效")
	}
	if data[ref].Key == "" {
		return "", fmt.Errorf("OpenCode Go 凭据为空")
	}
	return data[ref].Key, nil
}
func ValidateModel(m ModelConfig) error {
	u, e := url.Parse(m.BaseURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("模型接口地址无效")
	}
	if m.Model == "" || m.MaxTokens < 64 || m.MaxTokens > 384000 || m.ContextChars < 1000 || m.TimeoutSeconds < 1 || m.TimeoutSeconds > 3600 {
		return fmt.Errorf("模型参数无效：检查名称、输出上限、上下文或超时")
	}
	if m.Temperature < 0 || m.Temperature > 2 {
		return fmt.Errorf("温度应在 0–2 之间")
	}
	if m.ReasoningEffort != "" && m.ReasoningEffort != "low" && m.ReasoningEffort != "high" && m.ReasoningEffort != "max" {
		return fmt.Errorf("思考强度必须为low、high或max")
	}
	return nil
}
func endpoint(base, suffix string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/chat/completions") {
		b = strings.TrimSuffix(b, "/chat/completions")
	}
	return b + suffix
}
func (h *HTTPProvider) Generate(ctx context.Context, r ModelRequest, chunk func(string, string)) (ModelResult, error) {
	var result ModelResult
	if e := ValidateModel(r.Config); e != nil {
		return result, e
	}
	if len([]rune(r.System+r.User)) > r.Config.ContextChars {
		return result, fmt.Errorf("材料超过当前模型的上下文字数上限，请提高上限或缩小审核范围")
	}
	key, e := h.Credentials(r.Config.Credential)
	if e != nil {
		return result, e
	}
	timeout := time.Duration(r.Config.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload := map[string]any{"model": r.Config.Model, "stream": true, "messages": []map[string]string{{"role": "system", "content": r.System}, {"role": "user", "content": r.User}}, "max_tokens": r.Config.MaxTokens, "temperature": r.Config.Temperature}
	if strings.Contains(strings.ToLower(r.Config.Model), "kimi-k3") {
		effort := r.Config.ReasoningEffort
		if effort == "" {
			effort = "high"
		}
		payload["reasoning_effort"] = effort
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	// 文本模型只需要通用参数。DeepSeek 的 thinking 扩展仅对该模型家族发送。
	if strings.Contains(strings.ToLower(r.Config.Model), "deepseek") {
		mode := "disabled"
		if r.Config.Thinking {
			mode = "enabled"
		}
		payload["thinking"] = map[string]string{"type": mode}
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	b, _ := json.Marshal(payload)
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint(r.Config.BaseURL, "/chat/completions"), bytes.NewReader(b))
	if e != nil {
		return result, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "WeaveGrid/0.1")
	req.Header.Set("x-opencode-session", r.Session)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := h.Client
	if client == nil {
		client = modelHTTPClient
	}
	// EOF 后用独立连接重试，避开已经被网关关闭的连接池；仍沿用代理和 TLS 验证。
	if r.FreshConnection {
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if base, ok := transport.(*http.Transport); ok {
			fresh := base.Clone()
			fresh.DisableKeepAlives = true
			defer fresh.CloseIdleConnections()
			copy := *client
			copy.Transport = fresh
			client = &copy
		}
	}
	resp, e := client.Do(req)
	if e != nil {
		return result, e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return result, &ModelError{resp.StatusCode, fmt.Sprintf("模型接口返回 %d：%s", resp.StatusCode, string(msg)), time.Duration(wait) * time.Second}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 8192), 8<<20)
	finished := false
	for scanner.Scan() {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var v struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					Reasoning string `json:"reasoning_content"`
				} `json:"delta"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Total      int `json:"total_tokens"`
			} `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if e = json.Unmarshal([]byte(data), &v); e != nil {
			return result, fmt.Errorf("无法解析模型事件：%w", e)
		}
		if len(v.Error) > 0 && string(v.Error) != "null" {
			return result, fmt.Errorf("模型流错误：%s", v.Error)
		}
		if v.Usage.Total > 0 {
			result.Usage = Usage{v.Usage.Prompt, v.Usage.Completion, v.Usage.Total}
		}
		for _, c := range v.Choices {
			if c.Delta.Content != "" || c.Delta.Reasoning != "" {
				result.Text += c.Delta.Content
				result.Reasoning += c.Delta.Reasoning
				chunk(c.Delta.Content, c.Delta.Reasoning)
			}
			if c.Finish != nil {
				if *c.Finish != "stop" {
					return result, fmt.Errorf("模型输出未完整结束（%s），候选已保留", *c.Finish)
				}
				finished = true
			}
		}
	}
	if e = scanner.Err(); e != nil {
		return result, e
	}
	if !finished {
		// 连接提前结束按临时传输故障重试；已有部分输出仍留在失败尝试中。
		return result, &ModelError{Status: 503, Message: "模型连接提前结束，未收到完整结束标记"}
	}
	if strings.TrimSpace(result.Text) == "" {
		return result, fmt.Errorf("模型返回空内容")
	}
	return result, nil
}

func parseJSON(text string, out any) error {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		i := strings.Index(t, "\n")
		j := strings.LastIndex(t, "```")
		if i >= 0 && j > i {
			t = t[i+1 : j]
		}
	}
	d := json.NewDecoder(strings.NewReader(t))
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("模型结构化结果不符合要求：%w", e)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("结构化结果后有额外内容")
	}
	return nil
}
