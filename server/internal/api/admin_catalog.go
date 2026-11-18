package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"nuan/internal/llm"
	"nuan/internal/store"
)

// ---- providers ----

type providerView struct {
	*store.Provider
	APIKeyHint        string `json:"api_key_hint"`
	Endpoint          string `json:"endpoint"` // the chat completions URL actually called
	KeyURL            string `json:"key_url"`
	ModelCount        int    `json:"model_count"`
	EnabledModelCount int    `json:"enabled_model_count"`
}

func maskKey(k string) string {
	if len(k) < 16 {
		return "****"
	}
	return k[:3] + "****" + k[len(k)-4:]
}

func (s *Server) providerView(p *store.Provider, counts map[string][2]int) providerView {
	c := counts[p.ID]
	keyURL := ""
	if p.Kind != llm.KindCustom {
		keyURL = llm.PresetFor(p.Kind).KeyURL
	}
	return providerView{p, maskKey(p.APIKey), llm.Endpoint(p.BaseURL, "/chat/completions"), keyURL, c[0], c[1]}
}

func (s *Server) adminProviderPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, llm.Presets)
}

func (s *Server) adminListProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProviders(r.Context())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	counts, err := s.Store.ModelCounts(r.Context())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	out := make([]providerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, s.providerView(p, counts))
	}
	writeJSON(w, 200, out)
}

type providerInput struct {
	Kind    *string `json:"kind"`
	Name    *string `json:"name"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"` // omitted or "" = keep existing
	Enabled *bool   `json:"enabled"`
	Sort    *int    `json:"sort"`
}

// origin is scheme://host[:port], the part that decides where the key is sent.
func origin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func validBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// apply merges the input into p. It refuses to send a stored key to a new host:
// changing the address requires entering the key again, so a stolen admin session
// cannot exfiltrate keys by pointing a provider at its own server.
func (in *providerInput) apply(p *store.Provider, isNew bool) (code, msg string) {
	if in.Kind != nil {
		k := strings.TrimSpace(*in.Kind)
		if !llm.ValidKind(k) {
			return "bad_kind", "供应商类型不正确"
		}
		p.Kind = k
	}
	if p.Kind == "" {
		p.Kind = llm.KindCustom
	}
	preset := llm.PresetFor(p.Kind)
	oldOrigin := origin(p.BaseURL)
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.BaseURL != nil {
		p.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if isNew && p.Name == "" && p.Kind != llm.KindCustom {
		p.Name = preset.Name
	}
	if isNew && p.BaseURL == "" {
		p.BaseURL = preset.BaseURL
	}
	newKey := in.APIKey != nil && strings.TrimSpace(*in.APIKey) != ""
	if newKey {
		p.APIKey = strings.TrimSpace(*in.APIKey)
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.Sort != nil {
		p.Sort = *in.Sort
	}
	switch {
	case p.Name == "" || utf8.RuneCountInString(p.Name) > 30:
		return "bad_name", "请填写名称（30 个字以内）"
	case !validBaseURL(p.BaseURL):
		return "bad_url", "接口地址需要以 http:// 或 https:// 开头"
	case p.APIKey == "":
		return "key_required", "请填写 API Key"
	case !isNew && !newKey && origin(p.BaseURL) != oldOrigin:
		return "key_required", "修改了接口地址，请重新填写 API Key"
	}
	return "", ""
}

func (s *Server) adminSaveProvider(w http.ResponseWriter, r *http.Request) {
	var in providerInput
	if !decode(w, r, &in) {
		return
	}
	p := &store.Provider{Enabled: true}
	id := chi.URLParam(r, "id")
	if id != "" {
		var err error
		if p, err = s.Store.GetProvider(r.Context(), id); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	} else if ps, err := s.Store.ListProviders(r.Context()); err == nil {
		p.Sort = len(ps)
	}
	if code, msg := in.apply(p, id == ""); code != "" {
		writeErr(w, 400, code, msg)
		return
	}
	if err := s.Store.UpsertProvider(r.Context(), p); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	s.repairSettings(r)
	counts, _ := s.Store.ModelCounts(r.Context())
	writeJSON(w, 200, s.providerView(p, counts))
}

func (s *Server) adminDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteProvider(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	s.repairSettings(r)
	s.adminGetSettings(w, r)
}

func (s *Server) repairSettings(r *http.Request) {
	if err := s.Chat.RepairSettings(r.Context()); err != nil {
		s.Log.Error("repair model settings", "err", err)
	}
}

// adminTestProvider checks an address + key before (or after) saving.
// Failures are reported as {ok:false, message} with status 200 so the app can show them inline.
func (s *Server) adminTestProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID string `json:"id"`
		providerInput
	}
	if !decode(w, r, &in) {
		return
	}
	p := &store.Provider{Enabled: true}
	if in.ID != "" {
		var err error
		if p, err = s.Store.GetProvider(r.Context(), in.ID); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	if code, msg := in.apply(p, in.ID == ""); code != "" {
		writeErr(w, 400, code, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c := llm.NewClient(p.Kind, p.BaseURL, p.APIKey)
	start := time.Now()
	out := map[string]any{"ok": false, "endpoint": llm.Endpoint(p.BaseURL, "/chat/completions")}
	key, err := c.CheckKey(ctx)
	var ms []llm.UpstreamModel
	if err == nil {
		ms, err = c.ListModels(ctx)
	}
	out["latency_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		out["message"] = providerError(err)
		writeJSON(w, 200, out)
		return
	}
	chat := 0
	for _, m := range ms {
		if llm.Suggest(p.Kind, m).Chat {
			chat++
		}
	}
	out["ok"], out["model_count"], out["chat_model_count"] = true, len(ms), chat
	if key != nil {
		out["key_usage"], out["key_limit"] = key.Usage, key.Limit
	}
	writeJSON(w, 200, out)
}

// providerError explains connection problems in plain words.
func providerError(err error) string {
	var ae *llm.APIError
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &ae):
		switch {
		case ae.Status == 401 || ae.Status == 403:
			return "API Key 不正确，或者没有权限"
		case ae.Status == 402:
			return "账户余额不足"
		case ae.Status == 404:
			return "接口地址不对（返回 404），请检查地址"
		case ae.Status == 429:
			return "请求太频繁，或者额度用完了"
		case ae.Status >= 500:
			return fmt.Sprintf("对方服务暂时不可用（%d），稍后再试", ae.Status)
		}
		return fmt.Sprintf("对方返回错误 %d：%s", ae.Status, truncate(ae.Message, 120))
	case errors.Is(err, context.DeadlineExceeded):
		return "连接超时：请检查地址，或者服务器能不能访问这个服务"
	case errors.As(err, &dnsErr):
		return "找不到这个域名，请检查地址"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return "连接失败：" + truncate(ue.Err.Error(), 120)
	}
	return truncate(err.Error(), 160)
}

type upstreamItem struct {
	llm.Suggestion
	ExistingModelID string `json:"existing_model_id"`
}

// adminUpstreamModels lists the provider's models with suggested settings.
func (s *Server) adminUpstreamModels(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetProvider(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ms, err := s.Chat.Client(p).ListModels(ctx)
	if err != nil {
		writeErr(w, 502, "upstream", "获取模型列表失败："+providerError(err))
		return
	}
	existing := map[string]string{}
	if all, err := s.Store.ListModels(r.Context(), false); err == nil {
		for _, m := range all {
			if m.ProviderID == p.ID {
				existing[m.UpstreamModel] = m.ID
			}
		}
	}
	items := make([]upstreamItem, 0, len(ms))
	for _, m := range ms {
		sg := llm.Suggest(p.Kind, m)
		items = append(items, upstreamItem{sg, existing[sg.UpstreamModel]})
	}
	rank := func(sg llm.Suggestion) int {
		switch {
		case sg.Chat:
			return 0
		case sg.Image:
			return 1
		}
		return 2
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if rank(a.Suggestion) != rank(b.Suggestion) {
			return rank(a.Suggestion) < rank(b.Suggestion)
		}
		if a.Recommended != b.Recommended {
			return a.Recommended
		}
		if a.Created != b.Created {
			return a.Created > b.Created
		}
		return a.UpstreamModel < b.UpstreamModel
	})
	writeJSON(w, 200, map[string]any{"items": items})
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

type modelInput struct {
	ProviderID    *string  `json:"provider_id"`
	Kind          *string  `json:"kind"`
	Tools         *bool    `json:"tools"`
	UpstreamModel *string  `json:"upstream_model"`
	DisplayName   *string  `json:"display_name"`
	Description   *string  `json:"description"`
	Vision        *bool    `json:"vision"`
	DefaultEffort *string  `json:"default_effort"`
	ThinkEffort   *string  `json:"think_effort"`
	ContextWindow *int     `json:"context_window"`
	MaxOutput     *int     `json:"max_output"`
	InputPrice    *float64 `json:"input_price"`
	OutputPrice   *float64 `json:"output_price"`
	Enabled       *bool    `json:"enabled"`
	Sort          *int     `json:"sort"`
}

// newModel is a fresh model of the requested kind.
func newModel(kind *string) *store.Model {
	m := &store.Model{Kind: store.KindChat, Enabled: true, Tools: true, ContextWindow: llm.MaxSuggestedContext}
	if kind != nil {
		m.Kind, m.Tools = *kind, *kind == store.KindChat
	}
	return m
}

func (in *modelInput) apply(m *store.Model) (code, msg string) {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&m.Kind, in.Kind)
	set(&m.ProviderID, in.ProviderID)
	set(&m.UpstreamModel, in.UpstreamModel)
	set(&m.DisplayName, in.DisplayName)
	set(&m.Description, in.Description)
	set(&m.DefaultEffort, in.DefaultEffort)
	set(&m.ThinkEffort, in.ThinkEffort)
	for _, b := range []struct {
		dst *bool
		v   *bool
	}{{&m.Vision, in.Vision}, {&m.Enabled, in.Enabled}, {&m.Tools, in.Tools}} {
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
	for _, f := range []struct {
		dst *float64
		v   *float64
	}{{&m.InputPrice, in.InputPrice}, {&m.OutputPrice, in.OutputPrice}} {
		if f.v != nil {
			*f.dst = *f.v
		}
	}
	if m.DisplayName == "" {
		m.DisplayName = llm.PrettyName(m.UpstreamModel)
	}
	if m.Kind != store.KindChat && m.Kind != store.KindImage && m.Kind != store.KindEmbedding {
		return "bad_model", "模型类型不正确"
	}
	switch {
	case m.UpstreamModel == "":
		return "bad_model", "请填写上游模型名"
	case utf8.RuneCountInString(m.DisplayName) > 40 || utf8.RuneCountInString(m.Description) > 60:
		return "bad_model", "名称最多 40 个字，简介最多 60 个字"
	case m.Kind != store.KindEmbedding && m.ContextWindow < 1024:
		return "bad_model", "上下文长度至少 1024"
	case m.MaxOutput < 0 || m.InputPrice < 0 || m.OutputPrice < 0:
		return "bad_model", "数值不能是负数"
	case len(m.DefaultEffort) > 16 || len(m.ThinkEffort) > 16:
		return "bad_model", "思考档位不正确"
	}
	return "", ""
}

func (s *Server) adminSaveModel(w http.ResponseWriter, r *http.Request) {
	var in modelInput
	if !decode(w, r, &in) {
		return
	}
	m := newModel(in.Kind)
	id := chi.URLParam(r, "id")
	if id != "" {
		var err error
		if m, err = s.Store.GetModel(r.Context(), id); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}
	if code, msg := in.apply(m); code != "" {
		writeErr(w, 400, code, msg)
		return
	}
	if _, err := s.Store.GetProvider(r.Context(), m.ProviderID); err != nil {
		writeErr(w, 400, "bad_provider", "供应商不存在")
		return
	}
	var err error
	if id == "" {
		err = s.Store.CreateModels(r.Context(), []*store.Model{m})
	} else {
		err = s.Store.UpsertModel(r.Context(), m)
	}
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if id == "" {
		s.adoptImageModel(r, []*store.Model{m})
	}
	s.repairSettings(r)
	writeJSON(w, 200, m)
}

// adminBatchModels adds several upstream models at once (from the picker).
func (s *Server) adminBatchModels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProviderID string       `json:"provider_id"`
		Models     []modelInput `json:"models"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, err := s.Store.GetProvider(r.Context(), in.ProviderID); err != nil {
		writeErr(w, 400, "bad_provider", "供应商不存在")
		return
	}
	if len(in.Models) == 0 || len(in.Models) > 100 {
		writeErr(w, 400, "bad_model", "请选择 1–100 个模型")
		return
	}
	existing := map[string]bool{}
	if all, err := s.Store.ListModels(r.Context(), false); err == nil {
		for _, m := range all {
			if m.ProviderID == in.ProviderID {
				existing[m.UpstreamModel] = true
			}
		}
	}
	var add []*store.Model
	for _, mi := range in.Models {
		m := newModel(mi.Kind)
		mi.ProviderID, mi.Sort = &in.ProviderID, nil
		if code, msg := mi.apply(m); code != "" {
			writeErr(w, 400, code, msg)
			return
		}
		if existing[m.UpstreamModel] {
			continue
		}
		existing[m.UpstreamModel] = true
		add = append(add, m)
	}
	if len(add) > 0 {
		if err := s.Store.CreateModels(r.Context(), add); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
		s.adoptImageModel(r, add)
	}
	s.repairSettings(r)
	def, _ := s.Store.GetSetting(r.Context(), "default_model_id")
	if add == nil {
		add = []*store.Model{}
	}
	writeJSON(w, 200, map[string]any{"items": add, "default_model_id": def})
}

func (s *Server) adminDeleteModel(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteModel(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	s.repairSettings(r)
	s.adminGetSettings(w, r)
}

func (s *Server) adminReorderModels(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Store.SetModelOrder(r.Context(), in.IDs); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	s.adminListModels(w, r)
}

// adoptImageModel makes the first newly added image model the one conversations
// draw with, when none is set yet (adding one means the admin wants drawing).
func (s *Server) adoptImageModel(r *http.Request, added []*store.Model) {
	if cur, _ := s.Store.GetSetting(r.Context(), "image_model_id"); cur != "" {
		return
	}
	for _, m := range added {
		if m.Kind == store.KindImage && m.Enabled {
			if err := s.Store.SetSetting(r.Context(), "image_model_id", m.ID); err != nil {
				s.Log.Error("set image model", "err", err)
			}
			return
		}
	}
}

// adminTestModel sends one short real request with the model's current settings,
// so wrong ids or unsupported reasoning settings show up before the family hits them.
func (s *Server) adminTestModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Think bool `json:"think"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	m, err := s.Store.GetModel(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	p, err := s.Store.GetProvider(r.Context(), m.ProviderID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if m.Kind == store.KindImage {
		s.testImageModel(w, r, m, p)
		return
	}
	if m.Kind == store.KindEmbedding {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		start := time.Now()
		vectors, usage, err := s.Chat.Client(p).Embed(ctx, m.UpstreamModel, []string{"小暖记忆测试"})
		out := map[string]any{"ok": err == nil, "duration_ms": time.Since(start).Milliseconds(), "reasoning": false, "effort": "", "first_token_ms": 0}
		if err != nil {
			out["message"] = providerError(err)
		} else {
			out["reply"] = fmt.Sprintf("向量维度 %d", len(vectors[0]))
			if usage != nil {
				out["prompt_tokens"], out["completion_tokens"], out["cost"] = usage.PromptTokens, usage.CompletionTokens, usage.Cost
			}
		}
		writeJSON(w, 200, out)
		return
	}
	req := llm.Request{
		Model: m.UpstreamModel,
		Messages: []llm.Message{
			{Role: "system", Content: "你是一个家庭 AI 助手。"},
			{Role: "user", Content: "请用一句话介绍你自己。"},
		},
		ReasoningEffort: m.DefaultEffort,
	}
	if in.Think && m.ThinkEffort != "" {
		req.ReasoningEffort = m.ThinkEffort
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	start := time.Now()
	var first time.Duration
	var reply, reasoning strings.Builder
	res, err := s.Chat.Client(p).Stream(ctx, req, func(ev llm.Event) {
		if first == 0 {
			first = time.Since(start)
		}
		reply.WriteString(ev.Content)
		reasoning.WriteString(ev.Reasoning)
	})
	out := map[string]any{
		"ok": err == nil, "effort": req.ReasoningEffort,
		"first_token_ms": first.Milliseconds(), "duration_ms": time.Since(start).Milliseconds(),
		"reply": truncate(strings.TrimSpace(reply.String()), 200), "reasoning": reasoning.Len() > 0,
	}
	if err != nil {
		out["message"] = providerError(err)
	} else if res != nil && res.Usage != nil {
		cost := res.Usage.Cost
		if cost <= 0 {
			cost = m.EstimateCost(res.Usage.PromptTokens, res.Usage.CompletionTokens)
		}
		out["prompt_tokens"], out["completion_tokens"], out["cost"] = res.Usage.PromptTokens, res.Usage.CompletionTokens, cost
	}
	writeJSON(w, 200, out)
}

// testImageModel draws one small test picture and stores it as the admin's file.
func (s *Server) testImageModel(w http.ResponseWriter, r *http.Request, m *store.Model, p *store.Provider) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	start := time.Now()
	res, err := s.Chat.Client(p).GenerateImage(ctx, llm.ImageRequest{
		Model: m.UpstreamModel, Prompt: "A cute small orange cat sitting on a sunny windowsill, soft watercolor illustration", Size: "1024x1024", Quality: "low",
	})
	out := map[string]any{"ok": err == nil, "duration_ms": time.Since(start).Milliseconds()}
	if err != nil {
		out["message"] = providerError(err)
		writeJSON(w, 200, out)
		return
	}
	f, err := s.Chat.SaveImage(r.Context(), currentUser(r).ID, res.Data)
	if err != nil {
		out["ok"], out["message"] = false, "图片保存失败："+err.Error()
		writeJSON(w, 200, out)
		return
	}
	cost := 0.0
	if u := res.Usage; u != nil {
		cost = u.Cost
		if cost <= 0 {
			cost = m.EstimateCost(u.PromptTokens, u.CompletionTokens)
		}
	}
	out["file_id"], out["width"], out["height"], out["cost"] = f.ID, f.Width, f.Height, cost
	writeJSON(w, 200, out)
}
