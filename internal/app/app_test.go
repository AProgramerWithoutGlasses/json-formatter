package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStatelessAPI(t *testing.T) {
	handler := New(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("test")}})
	tests := []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/health", "", 200},
		{"POST", "/api/format", `{"input":"{\"id\":9007199254740993}"}`, 200},
		{"POST", "/api/format", `{"input":"{}","save":true}`, 400},
		{"GET", "/api/history", "", 404},
		{"GET", "/api/history/1", "", 404},
		{"DELETE", "/api/history/1", "", 404},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d, %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatal("stateless API must not create a history session")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("API responses must not be cached")
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"saved", "database", "warning"} {
			if _, ok := result[key]; ok {
				t.Fatalf("obsolete persistence field %q", key)
			}
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/format", strings.NewReader(`{"input":"{}"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin protection regressed")
	}
}
