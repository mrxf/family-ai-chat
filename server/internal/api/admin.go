package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nuan/internal/auth"
	"nuan/internal/store"
)

// ---- users ----

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-\p{Han}]{2,32}$`)

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.Store.ListUsers(r.Context())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, us)
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Avatar      string `json:"avatar"`
		Role        string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !usernameRe.MatchString(in.Username) {
		writeErr(w, 400, "bad_username", "用户名需要 2–32 位，可以用字母、数字、汉字")
		return
	}
	if len(in.Password) < 6 {
		writeErr(w, 400, "weak_password", "密码至少 6 位")
		return
	}
	if in.Role != "admin" {
		in.Role = "member"
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = in.Username
	}
	h, err := auth.HashPassword(in.Password)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	u := &store.User{Username: in.Username, DisplayName: strings.TrimSpace(in.DisplayName), Avatar: in.Avatar, Role: in.Role, PasswordHash: h}
	if err := s.Store.CreateUser(r.Context(), u); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, u)
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in struct {
		DisplayName *string `json:"display_name"`
		Avatar      *string `json:"avatar"`
		Role        *string `json:"role"`
		Password    string  `json:"password"`
		Disabled    *bool   `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	me := currentUser(r)
	if id == me.ID && ((in.Role != nil && *in.Role != "admin") || (in.Disabled != nil && *in.Disabled)) {
		writeErr(w, 400, "self", "不能取消自己的管理员权限或停用自己")
		return
	}
	p := store.UserPatch{DisplayName: in.DisplayName, Avatar: in.Avatar, Disabled: in.Disabled}
	if in.Role != nil {
		if *in.Role != "admin" && *in.Role != "member" {
			writeErr(w, 400, "bad_role", "角色不正确")
			return
		}
		p.Role = in.Role
	}
	if in.Password != "" {
		if len(in.Password) < 6 {
			writeErr(w, 400, "weak_password", "密码至少 6 位")
			return
		}
		h, err := auth.HashPassword(in.Password)
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
		p.PasswordHash = &h
	}
	u, err := s.Store.UpdateUser(r.Context(), id, p)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if in.Password != "" || (in.Disabled != nil && *in.Disabled) {
		_ = s.Store.DeleteUserSessions(r.Context(), id)
	}
	writeJSON(w, 200, u)
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == currentUser(r).ID {
		writeErr(w, 400, "self", "不能删除自己")
		return
	}
	if err := s.Store.DeleteUser(r.Context(), id); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- settings & usage ----

var settingKeys = []string{"default_model_id", "fallback_model_id", "title_model_id"}

func (s *Server) adminGetSettings(w http.ResponseWriter, r *http.Request) {
	out := map[string]string{}
	for _, k := range settingKeys {
		v, _ := s.Store.GetSetting(r.Context(), k)
		out[k] = v
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminPutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	for _, k := range settingKeys {
		v, ok := in[k]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" && k == "default_model_id" {
			writeErr(w, 400, "bad_model", "全家默认模型不能为空")
			return
		}
		if v != "" {
			if _, err := s.Store.GetModel(r.Context(), v); err != nil {
				writeErr(w, 400, "bad_model", "模型不存在")
				return
			}
		}
		if err := s.Store.SetSetting(r.Context(), k, v); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	s.adminGetSettings(w, r)
}

func (s *Server) adminUsage(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 366 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).UnixMilli()
	rows, err := s.Store.UsageSince(r.Context(), since)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	models, err := s.Store.ModelUsageSince(r.Context(), since)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	total := store.UsageRow{}
	for _, u := range rows {
		total.Messages += u.Messages
		total.PromptTokens += u.PromptTokens
		total.CompletionTokens += u.CompletionTokens
		total.Cost += u.Cost
	}
	writeJSON(w, 200, map[string]any{"days": days, "items": rows, "models": models, "total": total})
}
