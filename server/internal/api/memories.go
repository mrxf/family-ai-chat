package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
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

// startMemoryImport begins organizing pasted text in the background; the App
// polls getMemoryImport. Each user has at most one job.
func (s *Server) startMemoryImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	if utf8.RuneCountInString(in.Text) > memory.MaxImportRunes {
		writeErr(w, 400, "bad_memory", fmt.Sprintf("导入文本最多 %d 字", memory.MaxImportRunes))
		return
	}
	if s.Chat.Memory == nil {
		writeErr(w, 503, "memory_preview_failed", "暂时无法整理，请稍后再试")
		return
	}
	job, err := s.Chat.Memory.StartImport(currentUser(r), in.Text)
	if err != nil {
		writeErr(w, 400, "bad_memory", "没有可以整理的内容")
		return
	}
	writeJSON(w, 202, job)
}
func (s *Server) getMemoryImport(w http.ResponseWriter, r *http.Request) {
	if s.Chat.Memory == nil {
		writeErr(w, 404, "not_found", "没有正在整理的内容")
		return
	}
	job, ok := s.Chat.Memory.ImportStatus(currentUser(r).ID)
	if !ok {
		writeErr(w, 404, "not_found", "没有正在整理的内容")
		return
	}
	writeJSON(w, 200, job)
}
func (s *Server) cancelMemoryImport(w http.ResponseWriter, r *http.Request) {
	if s.Chat.Memory != nil {
		s.Chat.Memory.CancelImport(currentUser(r).ID)
	}
	w.WriteHeader(204)
}
func (s *Server) importMemories(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []memory.ImportItem `json:"items"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Items) > memory.MaxImportItems {
		writeErr(w, 400, "bad_memory", fmt.Sprintf("一次最多导入 %d 条", memory.MaxImportItems))
		return
	}
	u := currentUser(r)
	items := []store.MemoryImport{}
	for _, item := range in.Items {
		item.Content = strings.TrimSpace(item.Content)
		if !validMemoryInput(item.Category, item.Content) || item.Category == "episode" {
			writeErr(w, 400, "bad_memory", "分类不正确，或内容不是 1–200 字")
			return
		}
		m := &store.Memory{Category: item.Category, Content: item.Content, Keywords: strings.TrimSpace(item.Keywords), Source: "import", Pinned: true}
		if item.ValidUntil != "" && item.Category == "ongoing" {
			d, err := time.ParseInLocation("2006-01-02", item.ValidUntil, cst)
			if err != nil {
				writeErr(w, 400, "bad_memory", "日期格式不正确")
				return
			}
			v := d.AddDate(0, 0, 1).UnixMilli() - 1
			m.ValidUntil = &v
		}
		items = append(items, store.MemoryImport{Memory: m, ReplacesID: item.ReplacesID})
	}
	saved, skipped, err := s.Store.ImportMemories(r.Context(), u.ID, items)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if s.Chat.Memory != nil {
		s.Chat.Memory.CancelImport(u.ID)
		if len(saved) > 0 {
			go s.Chat.Memory.DrainEmbeddings(context.WithoutCancel(r.Context()))
		}
	}
	if saved == nil {
		saved = []*store.Memory{}
	}
	writeJSON(w, 200, map[string]any{"memories": saved, "skipped": skipped})
}

// Memory dates are calendar days in China.
var cst = time.FixedZone("CST", 8*3600)
