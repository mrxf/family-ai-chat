// Package api wires HTTP routes to the store and chat service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nuan/internal/auth"
	"nuan/internal/chat"
	"nuan/internal/store"
)

type Config struct {
	DataDir    string
	ASR        *ASRConfig // nil = voice input disabled
	AppVersion string
}

type ASRConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type Server struct {
	Cfg      Config
	Store    *store.Store
	Chat     *chat.Service
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Log      *slog.Logger

	setup setupState
}

func New(cfg Config, st *store.Store, svc *chat.Service, log *slog.Logger) *Server {
	return &Server{
		Cfg:      cfg,
		Store:    st,
		Chat:     svc,
		Sessions: &auth.Sessions{Store: st},
		Limiter:  auth.NewLimiter(8, 15*time.Minute),
		Log:      log,
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recoverer, s.logger)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": s.Cfg.AppVersion})
	})
	r.Get("/api/app/version", s.appVersion)
	r.Get("/app/download/{name}", s.appDownload)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Get("/setup", s.setupStatus)
		r.Post("/setup", s.runSetup)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.logout)
			r.Get("/bootstrap", s.bootstrap)
			r.Get("/me", s.me)
			r.Patch("/me", s.updateMe)

			r.Get("/models", s.listModels)
			r.Get("/assistants", s.listAssistants)
			r.Post("/assistants", s.createAssistant)
			r.Patch("/assistants/{id}", s.updateAssistant)
			r.Delete("/assistants/{id}", s.deleteAssistant)

			r.Get("/conversations", s.listConversations)
			r.Post("/conversations", s.createConversation)
			r.Get("/conversations/{id}", s.getConversation)
			r.Patch("/conversations/{id}", s.updateConversation)
			r.Delete("/conversations/{id}", s.deleteConversation)
			r.Get("/conversations/{id}/messages", s.listMessages)
			r.Post("/chat", s.send)
			r.Post("/conversations/{id}/regenerate", s.regenerate)
			r.Get("/messages/{id}/stream", s.stream)
			r.Post("/messages/{id}/cancel", s.cancel)

			r.Post("/files", s.uploadFile)
			r.Get("/files/{id}", s.getFile)
			r.Post("/audio/transcriptions", s.transcribe)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", s.adminListUsers)
				r.Post("/users", s.adminCreateUser)
				r.Patch("/users/{id}", s.adminUpdateUser)
				r.Delete("/users/{id}", s.adminDeleteUser)
				r.Get("/provider-presets", s.adminProviderPresets)
				r.Get("/providers", s.adminListProviders)
				r.Post("/providers", s.adminSaveProvider)
				r.Post("/providers/test", s.adminTestProvider)
				r.Patch("/providers/{id}", s.adminSaveProvider)
				r.Delete("/providers/{id}", s.adminDeleteProvider)
				r.Get("/providers/{id}/upstream-models", s.adminUpstreamModels)
				r.Get("/models", s.adminListModels)
				r.Post("/models", s.adminSaveModel)
				r.Post("/models/batch", s.adminBatchModels)
				r.Put("/models/order", s.adminReorderModels)
				r.Patch("/models/{id}", s.adminSaveModel)
				r.Delete("/models/{id}", s.adminDeleteModel)
				r.Post("/models/{id}/test", s.adminTestModel)
				r.Get("/settings", s.adminGetSettings)
				r.Put("/settings", s.adminPutSettings)
				r.Get("/usage", s.adminUsage)
			})
		})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, "not_found", "接口不存在") })
	return r
}

// ---- helpers ----

type ctxKey int

const userKey ctxKey = 1

func currentUser(r *http.Request) *store.User { return r.Context().Value(userKey).(*store.User) }

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.URL.Query().Get("access_token")
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Sessions.Validate(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, 401, "unauthorized", "登录已过期，请重新登录")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin() {
			writeErr(w, 403, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// writeStoreErr maps common errors to responses.
func (s *Server) writeStoreErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "not_found", "内容不存在或已被删除")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, 409, "conflict", "已存在同名项目")
	case errors.Is(err, chat.ErrBusy):
		writeErr(w, 429, "busy", "同时进行的对话太多了，请等当前回答完成")
	case errors.Is(err, chat.ErrConvBusy):
		writeErr(w, 409, "conversation_busy", "上一条回答还没结束，请稍等或先停止")
	case errors.Is(err, chat.ErrNoModel):
		writeErr(w, 503, "no_model", "还没有可用的模型，请联系管理员配置")
	case errors.Is(err, chat.ErrEmptyMessage):
		writeErr(w, 400, "empty", "消息不能为空")
	case errors.Is(err, chat.ErrNothingToRedo):
		writeErr(w, 400, "nothing", "没有可以重新生成的回答")
	default:
		s.Log.Error("request failed", "path", r.URL.Path, "err", err)
		writeErr(w, 500, "internal", "服务器出了点问题，请稍后再试")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "bad_request", "请求格式不正确")
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int)        { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Log.Error("panic", "path", r.URL.Path, "err", v, "stack", string(debug.Stack()))
				writeErr(w, 500, "internal", "服务器出了点问题，请稍后再试")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
