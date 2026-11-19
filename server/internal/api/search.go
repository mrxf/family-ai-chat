package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nuan/internal/store"
	"nuan/internal/websearch"
)

func (s *Server) searchStatus(ctx context.Context) (enabled bool, supportsFetch bool, kind string) {
	if s.Search == nil {
		return false, false, ""
	}
	return s.Search.ActiveStatus(ctx)
}

type searchProviderView struct {
	websearch.ProviderPreset
	HasKey                  bool   `json:"has_key"`
	APIKeyHint              string `json:"api_key_hint"`
	BaseURL                 string `json:"base_url"`
	Ready                   bool   `json:"ready"`
	ReusableProviderKeyHint string `json:"reusable_provider_key_hint,omitempty"`
}

type searchInput struct {
	Kind             string  `json:"kind"`
	APIKey           *string `json:"api_key"` // omitted or "" = keep existing stored key
	BaseURL          *string `json:"base_url"`
	ReuseProviderKey bool    `json:"reuse_provider_key"`
}

func (s *Server) reusableProviderKey(ctx context.Context, kind string) string {
	ps, err := s.Store.ListProviders(ctx)
	if err != nil {
		return ""
	}
	for _, p := range ps {
		if p.Kind == kind && strings.TrimSpace(p.APIKey) != "" {
			if p.Enabled {
				return strings.TrimSpace(p.APIKey)
			}
		}
	}
	for _, p := range ps {
		if p.Kind == kind && strings.TrimSpace(p.APIKey) != "" {
			return strings.TrimSpace(p.APIKey)
		}
	}
	return ""
}

func (s *Server) buildSearchConfig(ctx context.Context, in searchInput) (*store.SearchConfig, string, string) {
	kind := strings.TrimSpace(in.Kind)
	preset, ok := websearch.PresetFor(kind)
	if !ok {
		return nil, "bad_kind", "不支持的联网搜索类型"
	}
	if !preset.Available {
		return nil, "not_available", websearch.ErrNotImplemented.Error()
	}

	cfg, err := s.Store.GetSearchConfig(ctx, kind)
	isNew := err != nil
	if isNew {
		cfg = &store.SearchConfig{
			Kind:    kind,
			BaseURL: preset.DefaultURL,
		}
	}
	oldOrigin := origin(cfg.BaseURL)

	newKey := in.APIKey != nil && strings.TrimSpace(*in.APIKey) != ""
	if newKey {
		cfg.APIKey = strings.TrimSpace(*in.APIKey)
	} else if in.ReuseProviderKey {
		rk := s.reusableProviderKey(ctx, kind)
		if rk == "" {
			return nil, "no_reusable_key", "还没有配置同类型的模型供应商密钥，请手动粘贴 API Key"
		}
		cfg.APIKey = rk
		newKey = true
	}

	if in.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = preset.DefaultURL
	}
	if !validBaseURL(cfg.BaseURL) {
		return nil, "bad_url", "接口地址需要以 http:// 或 https:// 开头"
	}
	if !isNew && !newKey && oldOrigin != "" && origin(cfg.BaseURL) != oldOrigin {
		return nil, "key_required", "修改了接口地址，请重新填写 API Key"
	}
	return cfg, "", ""
}

func (s *Server) adminGetSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	active, _ := s.Store.GetSetting(ctx, websearch.SettingActiveProvider)
	saved, err := s.Store.ListSearchConfigs(ctx)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	enabled, supportsFetch, _ := s.searchStatus(ctx)

	views := make([]searchProviderView, 0, len(websearch.Presets))
	for _, pr := range websearch.Presets {
		v := searchProviderView{
			ProviderPreset: pr,
			BaseURL:        pr.DefaultURL,
		}
		if rk := s.reusableProviderKey(ctx, pr.Kind); rk != "" {
			v.ReusableProviderKeyHint = maskKey(rk)
		}
		if c, ok := saved[pr.Kind]; ok {
			v.HasKey = c.APIKey != ""
			if c.APIKey != "" {
				v.APIKeyHint = maskKey(c.APIKey)
			}
			if c.BaseURL != "" {
				v.BaseURL = c.BaseURL
			}
			v.Ready = websearch.IsReady(c) && pr.Available
		}
		views = append(views, v)
	}
	writeJSON(w, 200, map[string]any{
		"active":         active,
		"enabled":        enabled,
		"supports_fetch": supportsFetch,
		"providers":      views,
	})
}

func (s *Server) adminTestSearch(w http.ResponseWriter, r *http.Request) {
	var in searchInput
	if !decode(w, r, &in) {
		return
	}
	cfg, code, msg := s.buildSearchConfig(r.Context(), in)
	if code != "" {
		writeErr(w, 400, code, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
	defer cancel()

	ms, count, err := s.Search.Test(ctx, cfg)
	if err != nil {
		writeJSON(w, 200, map[string]any{
			"ok":           false,
			"latency_ms":   ms,
			"result_count": 0,
			"message":      websearch.FriendlyError(err, false),
		})
		return
	}
	summary := "连接成功，联网搜索服务正常"
	if count > 0 {
		summary = fmt.Sprintf("连接成功，实测返回 %d 条搜索结果", count)
	}
	writeJSON(w, 200, map[string]any{
		"ok":           true,
		"latency_ms":   ms,
		"result_count": count,
		"message":      summary,
	})
}

func (s *Server) adminPutSearch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active *string      `json:"active"`
		Config *searchInput `json:"config"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()

	if in.Config != nil {
		cfg, code, msg := s.buildSearchConfig(ctx, *in.Config)
		if code != "" {
			writeErr(w, 400, code, msg)
			return
		}
		if err := s.Search.Validate(cfg); err != nil {
			writeErr(w, 400, "bad_search_config", err.Error())
			return
		}
		if err := s.Store.UpsertSearchConfig(ctx, cfg); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}

	if in.Active != nil {
		act := strings.TrimSpace(*in.Active)
		if act != "" {
			preset, ok := websearch.PresetFor(act)
			if !ok {
				writeErr(w, 400, "bad_kind", "不支持的联网搜索类型")
				return
			}
			if !preset.Available {
				writeErr(w, 400, "not_available", websearch.ErrNotImplemented.Error())
				return
			}
			saved, err := s.Store.GetSearchConfig(ctx, act)
			if err != nil || !websearch.IsReady(saved) {
				if errors.Is(err, store.ErrNotFound) || saved == nil {
					writeErr(w, 400, "not_configured", "请先填写并保存搜索服务 API Key，再开启联网搜索")
					return
				}
				writeErr(w, 400, "not_configured", "搜索密钥配置还不完整，请补全后再开启")
				return
			}
		}
		if err := s.Store.SetSetting(ctx, websearch.SettingActiveProvider, act); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}

	s.adminGetSearch(w, r)
}
