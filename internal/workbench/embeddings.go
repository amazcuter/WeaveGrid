package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// EmbeddingProvider 可选；仅配置语义检索后调用，不影响纯文本 RWKV 接口。
type EmbeddingProvider interface {
	Embed(context.Context, Settings, []string) ([][]float64, error)
}

func (h *HTTPProvider) Embed(ctx context.Context, s Settings, texts []string) ([][]float64, error) {
	if s.EmbeddingURL == "" || s.EmbeddingModel == "" {
		return nil, fmt.Errorf("请配置向量接口与模型")
	}
	key, err := h.Credentials(s.EmbeddingCredential)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	b, _ := json.Marshal(map[string]any{"model": s.EmbeddingModel, "input": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint(s.EmbeddingURL, "/embeddings"), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "WeaveGrid/0.1")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("向量接口返回 %d", resp.StatusCode)
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("向量接口返回数量与段落数量不一致")
	}
	out := make([][]float64, len(texts))
	dimension := 0
	for _, d := range result.Data {
		if d.Index < 0 || d.Index >= len(out) || len(d.Embedding) == 0 || out[d.Index] != nil {
			return nil, fmt.Errorf("向量序号或维度无效")
		}
		if dimension == 0 {
			dimension = len(d.Embedding)
		}
		if len(d.Embedding) != dimension {
			return nil, fmt.Errorf("向量维度不一致")
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
func vectorModel(s Settings) string { return fingerprint([]string{s.EmbeddingURL, s.EmbeddingModel}) }
func (e *Engine) indexActive(ctx context.Context, p Project) error {
	if p.Settings.EmbeddingURL == "" {
		return nil
	}
	provider, ok := e.Provider.(EmbeddingProvider)
	if !ok {
		return fmt.Errorf("当前提供方不支持向量接口")
	}
	for _, ch := range p.Chapters {
		for _, kind := range []string{"outline", "draft"} {
			v := p.Current(ch.Number, kind)
			if v == nil {
				continue
			}
			var n int
			if err := e.Store.db.QueryRow("SELECT count(*) FROM vectors WHERE project_id=? AND version_id=? AND model=?", p.ID, v.ID, vectorModel(p.Settings)).Scan(&n); err != nil {
				return err
			}
			ps := paragraphs(v.Text)
			if n == len(ps) {
				continue
			}
			vectors, err := provider.Embed(ctx, p.Settings, ps)
			if err != nil {
				return err
			}
			e.Store.mu.Lock()
			tx, err := e.Store.db.Begin()
			if err == nil {
				for i, vect := range vectors {
					if _, err = tx.Exec("INSERT OR REPLACE INTO vectors VALUES(?,?,?,?,?)", p.ID, v.ID, i+1, vectorModel(p.Settings), asJSON(vect)); err != nil {
						break
					}
				}
				if err == nil {
					err = tx.Commit()
				} else {
					tx.Rollback()
				}
			}
			e.Store.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func cosine(a, b []float64) float64 {
	if len(a) != len(b) {
		return -1
	}
	dot, aa, bb := 0., 0., 0.
	for i := range a {
		dot += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 {
		return -1
	}
	return dot / math.Sqrt(aa*bb)
}
func (e *Engine) search(ctx context.Context, p Project, query string, limit int) ([]Citation, error) {
	hits, err := e.Store.Search(p.ID, query, limit)
	if err != nil || p.Settings.EmbeddingURL == "" {
		return hits, err
	}
	if strings.TrimSpace(query) == "" {
		return hits, nil
	}
	if err = e.indexActive(ctx, p); err != nil {
		return nil, err
	}
	provider := e.Provider.(EmbeddingProvider)
	q, err := provider.Embed(ctx, p.Settings, []string{query})
	if err != nil {
		return nil, err
	}
	rows, err := e.Store.db.Query("SELECT v.version_id,v.paragraph,v.vector,p.chapter,p.kind,p.text FROM vectors v JOIN passages p ON p.project_id=v.project_id AND p.version_id=v.version_id AND p.paragraph=v.paragraph WHERE v.project_id=? AND v.model=?", p.ID, vectorModel(p.Settings))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ranked struct {
		c     Citation
		score float64
	}
	rankings := []ranked{}
	for rows.Next() {
		var c Citation
		var b string
		if err = rows.Scan(&c.Version, &c.Paragraph, &b, &c.Chapter, &c.Kind, &c.Text); err != nil {
			return nil, err
		}
		var v []float64
		if err = json.Unmarshal([]byte(b), &v); err != nil {
			return nil, err
		}
		c.Plan = c.Kind == "outline"
		score := cosine(q[0], v)
		if score >= 0 {
			rankings = append(rankings, ranked{c, score})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(rankings, func(i, j int) bool { return rankings[i].score > rankings[j].score })
	for i, v := range rankings {
		if i >= limit {
			break
		}
		hits = mergeCitations(hits, []Citation{v.c})
	}
	return hits, nil
}
