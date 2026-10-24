package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrMissingDSN = errors.New("MYSQL_DSN is required; see README.md or run scripts/start-local.ps1")

type App struct{ db *sql.DB }

func New(db *sql.DB, assets fs.FS) http.Handler {
	a := &App{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("POST /api/format", a.format)
	mux.HandleFunc("GET /api/history", a.history)
	mux.HandleFunc("GET /api/history/{id}", a.detail)
	mux.HandleFunc("DELETE /api/history/{id}", a.delete)
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

func session(w http.ResponseWriter, r *http.Request) (string, error) {
	cookie, err := r.Cookie("json_studio_session")
	if err == nil && len(cookie.Value) == 64 {
		if _, err := hex.DecodeString(cookie.Value); err == nil {
			return hash(cookie.Value), nil
		}
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	value := hex.EncodeToString(token[:])
	http.SetCookie(w, &http.Cookie{Name: "json_studio_session", Value: value, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600})
	return hash(value), nil
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		writeJSON(w, 503, APIError{Error: "MySQL 暂不可用"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "database": "mysql"})
}

func (a *App) format(w http.ResponseWriter, r *http.Request) {
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
	if req.Save {
		// Store valid repaired content so history remains usable with repair disabled.
		if result.Repair != nil {
			req.Input = result.Output
		}
		s, err := session(w, r)
		if err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			err = a.save(ctx, s, req)
		}
		if err != nil {
			log.Printf("history save: %v", err)
			result.Warning = "整理完成，但历史记录保存失败，请稍后重试"
		} else {
			result.Saved = true
		}
	}
	writeJSON(w, 200, result)
}

func (a *App) history(w http.ResponseWriter, r *http.Request) {
	s, err := session(w, r)
	if err != nil {
		a.failure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := a.list(ctx, s)
	if err != nil {
		a.failure(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 63)
	if err != nil {
		writeJSON(w, 400, APIError{Error: "记录编号无效"})
		return
	}
	s, err := session(w, r)
	if err != nil {
		a.failure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var h History
	err = a.db.QueryRowContext(ctx, `SELECT id,title,input,indent_style,mode,input_bytes,updated_at FROM conversion_history WHERE id=? AND session_hash=?`, id, s).Scan(&h.ID, &h.Title, &h.Input, &h.Indent, &h.Mode, &h.Bytes, &h.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, APIError{Error: "记录不存在"})
		return
	}
	if err != nil {
		a.failure(w, err)
		return
	}
	writeJSON(w, 200, h)
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 63)
	if err != nil {
		writeJSON(w, 400, APIError{Error: "记录编号无效"})
		return
	}
	s, err := session(w, r)
	if err != nil {
		a.failure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := a.db.ExecContext(ctx, `DELETE FROM conversion_history WHERE id=? AND session_hash=?`, id, s)
	if err != nil {
		a.failure(w, err)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		a.failure(w, err)
		return
	}
	if n == 0 {
		writeJSON(w, 404, APIError{Error: "记录不存在"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) failure(w http.ResponseWriter, err error) {
	log.Printf("database: %v", err)
	writeJSON(w, 503, APIError{Error: "历史记录暂不可用，请稍后重试"})
}
