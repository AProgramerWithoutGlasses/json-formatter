package app

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

func New(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", health)
	mux.HandleFunc("POST /api/format", format)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 404, APIError{Error: "接口不存在"}) })
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Embedded assets keep stable URLs across builds; revalidate on refresh.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		// CodeMirror injects its scoped styles at runtime. Scripts remain same-origin.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					writeJSON(w, 403, APIError{Error: "不允许跨站请求"})
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeJSON(w, 403, APIError{Error: "不允许跨站请求"})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok", "history": "browser"})
}

func format(w http.ResponseWriter, r *http.Request) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeJSON(w, 415, APIError{Error: "请使用 application/json"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6*MaxInput+4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req FormatRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, 400, APIError{Error: "请求内容无效或过大"})
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, 400, APIError{Error: "请求只能包含一个对象"})
		return
	}
	if req.Indent == "" {
		req.Indent = "2"
	}
	if req.Mode == "" {
		req.Mode = "format"
	}
	result, apiErr := Format(req)
	if apiErr != nil {
		writeJSON(w, 422, apiErr)
		return
	}
	writeJSON(w, 200, result)
}
