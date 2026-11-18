package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"nuan/internal/memory"
	"nuan/internal/store"
)

func (s *Server) listMemories(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ms, err := s.Store.ListMemories(r.Context(), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	n := 0
	for _, m := range ms {
		if m.Source == "auto" && store.Now()-m.CreatedAt < 3*24*3600*1000 {
			n++
		}
	}
	writeJSON(w, 200, map[string]any{"memories": ms, "total": len(ms), "new": n, "enabled": memory.Enabled(u)})
}
func validMemoryInput(category, content string) bool {
	return store.ValidMemoryCategory(category, true) && utf8.RuneCountInString(content) >= 1 && utf8.RuneCountInString(content) <= 200
}
func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	var in memory.ImportItem
	if !decode(w, r, &in) {
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if !validMemoryInput(in.Category, in.Content) {
		writeErr(w, 400, "bad_memory", "分类不正确，或内容不是 1–200 字")
		return
	}
	m := &store.Memory{UserID: currentUser(r).ID, Category: in.Category, Content: in.Content, Source: "manual", Pinned: in.Category != "episode"}
	if err := s.Store.CreateMemories(r.Context(), []*store.Memory{m}); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, m)
}
func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Category *string `json:"category"`
		Content  *string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Content != nil {
		v := strings.TrimSpace(*in.Content)
		in.Content = &v
		if utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 200 {
			writeErr(w, 400, "bad_memory", "内容需要 1–200 字")
			return
		}
	}
	if in.Category != nil && !store.ValidMemoryCategory(*in.Category, true) {
		writeErr(w, 400, "bad_memory", "分类不正确")
		return
	}
	m, err := s.Store.UpdateMemory(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID, in.Category, in.Content)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, m)
}
func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteMemory(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) clearMemories(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ClearMemories(r.Context(), currentUser(r).ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) previewMemories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	if utf8.RuneCountInString(in.Text) > 8000 {
		writeErr(w, 400, "bad_memory", "导入文本最多 8000 字")
		return
	}
	if s.Chat.Memory == nil {
		writeErr(w, 503, "memory_preview_failed", "暂时无法整理，请稍后再试")
		return
	}
	items, err := s.Chat.Memory.Preview(r.Context(), in.Text)
	if err != nil {
		s.Log.Warn("memory import preview", "err", err)
		writeErr(w, 502, "memory_preview_failed", "暂时无法整理，请稍后再试")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) importMemories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []memory.ImportItem `json:"items"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Items) > 200 {
		writeErr(w, 400, "bad_memory", "一次最多导入 200 条")
		return
	}
	ms := []*store.Memory{}
	for _, item := range in.Items {
		item.Content = strings.TrimSpace(item.Content)
		if !validMemoryInput(item.Category, item.Content) || item.Category == "episode" {
			writeErr(w, 400, "bad_memory", "分类不正确，或内容不是 1–200 字")
			return
		}
		ms = append(ms, &store.Memory{UserID: currentUser(r).ID, Category: item.Category, Content: item.Content, Source: "import", Pinned: true})
	}
	if err := s.Store.CreateMemories(r.Context(), ms); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"memories": ms})
}
