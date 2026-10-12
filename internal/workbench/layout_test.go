package workbench

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanvasLayoutPersistenceAndConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(store, nil).Handler()
	payload := `{"format":"novelflow-layout","version":1,"graph":"workflow:all","direction":"compact","positions":{"A":{"x":120,"y":24},"A1":{"x":80,"y":112}}}`
	request := func(body, key, revision string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/api/layouts/workflow:all", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	first := request(payload, "layout-1", "0")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body)
	}
	duplicate := request(payload, "layout-1", "0")
	if duplicate.Body.String() != first.Body.String() {
		t.Fatal("幂等保存改变了布局")
	}
	if stale := request(payload, "layout-stale", "0"); stale.Code != 409 {
		t.Fatal(stale.Code)
	}
	invalid := strings.Replace(payload, `"x":120`, `"x":100001`, 1)
	if result := request(invalid, "layout-invalid", "1"); result.Code != 400 {
		t.Fatal(result.Code)
	}
	moved := strings.Replace(payload, `"x":120`, `"x":240`, 1)
	if result := request(moved, "layout-2", "1"); result.Code != 200 {
		t.Fatal(result.Code, result.Body)
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record, err := store.CanvasLayout("workflow:all")
	if err != nil {
		t.Fatal(err)
	}
	if record.Revision != 2 || record.Layout.Positions["A"].X != 240 || record.Layout.Positions["A1"].X != 80 {
		t.Fatal(record)
	}
	req := httptest.NewRequest("GET", "/api/layouts/workflow:all", nil)
	w := httptest.NewRecorder()
	NewServer(store, nil).Handler().ServeHTTP(w, req)
	var loaded LayoutRecord
	if err = json.Unmarshal(w.Body.Bytes(), &loaded); err != nil || loaded.Revision != 2 {
		t.Fatal(err, w.Body)
	}
}
