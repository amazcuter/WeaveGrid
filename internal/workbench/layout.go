package workbench

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

type CanvasLayout struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	Graph     string `json:"graph"`
	Direction string `json:"direction"`
	Positions map[string]struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"positions"`
}
type LayoutRecord struct {
	Revision int           `json:"revision"`
	Layout   *CanvasLayout `json:"layout"`
}

// 画布位置独立于小说版本和运行检查点；移动节点不会改变执行顺序或使审核失效。
func (s *Store) CanvasLayout(id string) (LayoutRecord, error) {
	var record LayoutRecord
	var raw string
	err := s.db.QueryRow("SELECT revision,doc FROM canvas_layouts WHERE id=?", id).Scan(&record.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	err = json.Unmarshal([]byte(raw), &record.Layout)
	return record, err
}
func (s *Store) SaveCanvasLayout(id string, layout CanvasLayout, revision int) (LayoutRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.CanvasLayout(id)
	if err != nil {
		return old, err
	}
	if old.Revision != revision {
		return old, ErrConflict
	}
	raw, err := json.Marshal(layout)
	if err != nil {
		return old, err
	}
	next := LayoutRecord{Revision: old.Revision + 1, Layout: &layout}
	_, err = s.db.Exec("INSERT INTO canvas_layouts VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,doc=excluded.doc", id, next.Revision, string(raw))
	return next, err
}
func validLayoutID(id string) bool {
	return id == "workflow:all" || strings.HasPrefix(id, "relations:") && len(id) > 10 && len(id) < 200
}
func (s *Server) getLayout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validLayoutID(id) {
		fail(w, fmt.Errorf("未知画布"))
		return
	}
	value, err := s.Store.CanvasLayout(id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, value)
}
func (s *Server) putLayout(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var layout CanvasLayout
	if err := decode(r, &layout); err != nil {
		fail(w, err)
		return
	}
	validDirection := layout.Direction == "compact" || layout.Direction == "RIGHT" || layout.Direction == "DOWN"
	validGraph := id == "workflow:all" && layout.Graph == id || strings.HasPrefix(id, "relations:") && layout.Graph == "relations"
	if !validLayoutID(id) || !validDirection || !validGraph || layout.Format != "novelflow-layout" || layout.Version != 1 || len(layout.Positions) == 0 || len(layout.Positions) > 2500 {
		fail(w, fmt.Errorf("布局格式无效"))
		return
	}
	for node, p := range layout.Positions {
		if len(node) > 100 || math.IsNaN(p.X) || math.IsNaN(p.Y) || math.Abs(p.X) > 100000 || math.Abs(p.Y) > 100000 {
			fail(w, fmt.Errorf("节点位置超出范围"))
			return
		}
	}
	value, err := s.Store.SaveCanvasLayout(id, layout, expected(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, value)
}
