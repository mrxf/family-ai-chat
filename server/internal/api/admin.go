package api

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
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

// ---- providers ----

type providerView struct {
	*store.Provider
	APIKeyHint string `json:"api_key_hint"`
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:3] + "****" + k[len(k)-4:]
}

func (s *Server) adminListProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProviders(r.Context())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	out := make([]providerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, providerView{p, maskKey(p.APIKey)})
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminSaveProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    *string `json:"name"`
		BaseURL *string `json:"base_url"`
		APIKey  *string `json:"api_key"` // omitted or "" = keep existing
		Enabled *bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := &store.Provider{Enabled: true}
	if id := chi.URLParam(r, "id"); id != "" {
		var err error
		if p, err = s.Store.GetProvider(r.Context(), id); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.BaseURL != nil {
		p.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if in.APIKey != nil && *in.APIKey != "" {
		p.APIKey = strings.TrimSpace(*in.APIKey)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if p.Name == "" || !strings.HasPrefix(p.BaseURL, "http") || p.APIKey == "" {
		writeErr(w, 400, "bad_provider", "请填写名称、以 http 开头的地址和 API Key")
		return
	}
	if err := s.Store.UpsertProvider(r.Context(), p); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, providerView{p, maskKey(p.APIKey)})
}

func (s *Server) adminDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProvider(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// adminUpstreamModels lists the models the provider exposes, to help pick upstream ids.
func (s *Server) adminUpstreamModels(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetProvider(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	ids, err := fetchUpstreamModels(r.Context(), p)
	if err != nil {
		writeErr(w, 502, "upstream", "获取模型列表失败："+err.Error())
		return
	}
	writeJSON(w, 200, ids)
}

func fetchUpstreamModels(ctx context.Context, p *store.Provider) ([]string, error) {
	url := strings.TrimRight(p.BaseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/models", nil)
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// ---- models ----

func (s *Server) adminListModels(w http.ResponseWriter, r *http.Request) {
	ms, err := s.Store.ListModels(r.Context(), false)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, ms)
}

func (s *Server) adminSaveModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID    *string `json:"provider_id"`
		UpstreamModel *string `json:"upstream_model"`
		DisplayName   *string `json:"display_name"`
		Description   *string `json:"description"`
		Vision        *bool   `json:"vision"`
		DefaultEffort *string `json:"default_effort"`
		ThinkEffort   *string `json:"think_effort"`
		ContextWindow *int    `json:"context_window"`
		MaxOutput     *int    `json:"max_output"`
		Enabled       *bool   `json:"enabled"`
		Sort          *int    `json:"sort"`
	}
	if !decode(w, r, &in) {
		return
	}
	m := &store.Model{Enabled: true, ContextWindow: 128000}
	if id := chi.URLParam(r, "id"); id != "" {
		var err error
		if m, err = s.Store.GetModel(r.Context(), id); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&m.ProviderID, in.ProviderID)
	set(&m.UpstreamModel, in.UpstreamModel)
	set(&m.DisplayName, in.DisplayName)
	set(&m.Description, in.Description)
	set(&m.DefaultEffort, in.DefaultEffort)
	set(&m.ThinkEffort, in.ThinkEffort)
	for _, b := range []struct {
		dst *bool
		v   *bool
	}{{&m.Vision, in.Vision}, {&m.Enabled, in.Enabled}} {
		if b.v != nil {
			*b.dst = *b.v
		}
	}
	for _, i := range []struct {
		dst *int
		v   *int
	}{{&m.ContextWindow, in.ContextWindow}, {&m.MaxOutput, in.MaxOutput}, {&m.Sort, in.Sort}} {
		if i.v != nil {
			*i.dst = *i.v
		}
	}
	if m.DisplayName == "" {
		m.DisplayName = m.UpstreamModel
	}
	if m.UpstreamModel == "" {
		writeErr(w, 400, "bad_model", "请填写上游模型名")
		return
	}
	if _, err := s.Store.GetProvider(r.Context(), m.ProviderID); err != nil {
		writeErr(w, 400, "bad_provider", "供应商不存在")
		return
	}
	if err := s.Store.UpsertModel(r.Context(), m); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) adminDeleteModel(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteModel(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- settings & usage ----

var settingKeys = []string{"default_model_id", "title_model_id"}

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
		if v, ok := in[k]; ok {
			if err := s.Store.SetSetting(r.Context(), k, v); err != nil {
				s.writeStoreErr(w, r, err)
				return
			}
		}
	}
	s.adminGetSettings(w, r)
}

func (s *Server) adminUsage(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 366 {
		days = 30
	}
	rows, err := s.Store.UsageSince(r.Context(), time.Now().AddDate(0, 0, -days).UnixMilli())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"days": days, "items": rows})
}
