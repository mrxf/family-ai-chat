package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

// ResolveMemoryModel supplies model/client selection to the independent memory service.
func (s *Service) ResolveMemoryModel(ctx context.Context, kind string) (*store.Model, *llm.Client, error) {
	keys := []string{"memory_model_id", "title_model_id", "default_model_id"}
	want := store.KindChat
	if kind == "embedding" {
		keys = []string{"embedding_model_id"}
		want = store.KindEmbedding
	}
	for _, k := range keys {
		id, err := s.Store.GetSetting(ctx, k)
		if err != nil {
			return nil, nil, err
		}
		if m, p, ok := s.usableAny(ctx, id); ok && m.Kind == want {
			return m, s.client(p), nil
		}
	}
	if kind != "embedding" {
		ms, err := s.Store.ListModels(ctx, true)
		if err != nil {
			return nil, nil, err
		}
		if len(ms) > 0 {
			if m, p, ok := s.usableAny(ctx, ms[0].ID); ok {
				return m, s.client(p), nil
			}
		}
	}
	if kind == "embedding" {
		return nil, nil, nil
	}
	return nil, nil, ErrNoModel
}

// resolveModel picks the model: conversation override → assistant → global default → first enabled.
// When the new message carries images and the pick cannot see them, a vision model is used
// for this reply instead (otherwise the images would silently be dropped).
func (s *Service) resolveModel(ctx context.Context, conv *store.Conversation, userID string, needVision bool) (*store.Model, *store.Provider, error) {
	m, p, err := s.pickModel(ctx, conv, userID)
	if err != nil || !needVision || m.Vision {
		return m, p, err
	}
	if vm, vp := s.firstVisionModel(ctx); vm != nil {
		return vm, vp, nil
	}
	return m, p, nil
}

// firstVisionModel returns the first usable vision model: fallback → default → list order.
func (s *Service) firstVisionModel(ctx context.Context) (*store.Model, *store.Provider) {
	ids := []string{}
	for _, k := range []string{"fallback_model_id", "default_model_id"} {
		if v, _ := s.Store.GetSetting(ctx, k); v != "" {
			ids = append(ids, v)
		}
	}
	if ms, err := s.Store.ListModels(ctx, true); err == nil {
		for _, m := range ms {
			ids = append(ids, m.ID)
		}
	}
	for _, id := range ids {
		if m, p, ok := s.usableModel(ctx, id); ok && m.Vision {
			return m, p
		}
	}
	return nil, nil
}

func (s *Service) pickModel(ctx context.Context, conv *store.Conversation, userID string) (*store.Model, *store.Provider, error) {
	candidates := []string{}
	if conv.ModelID != nil {
		candidates = append(candidates, *conv.ModelID)
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, userID); err == nil && a.ModelID != nil {
			candidates = append(candidates, *a.ModelID)
		}
	}
	if def, _ := s.Store.GetSetting(ctx, "default_model_id"); def != "" {
		candidates = append(candidates, def)
	}
	for _, id := range candidates {
		if m, p, ok := s.usableModel(ctx, id); ok {
			return m, p, nil
		}
	}
	models, err := s.Store.ListModels(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range models {
		if mm, p, ok := s.usableModel(ctx, m.ID); ok {
			return mm, p, nil
		}
	}
	return nil, nil, ErrNoModel
}

// usableModel returns an enabled chat model whose provider is enabled.
func (s *Service) usableModel(ctx context.Context, id string) (*store.Model, *store.Provider, bool) {
	m, p, ok := s.usableAny(ctx, id)
	if !ok || m.Kind != store.KindChat {
		return nil, nil, false
	}
	return m, p, true
}

// usableAny is usableModel for every kind of model.
func (s *Service) usableAny(ctx context.Context, id string) (*store.Model, *store.Provider, bool) {
	if id == "" {
		return nil, nil, false
	}
	m, err := s.Store.GetModel(ctx, id)
	if err != nil || !m.Enabled {
		return nil, nil, false
	}
	p, err := s.Store.GetProvider(ctx, m.ProviderID)
	if err != nil || !p.Enabled {
		return nil, nil, false
	}
	return m, p, true
}

// Client returns the (cached) upstream client of a provider.
func (s *Service) Client(p *store.Provider) *llm.Client { return s.client(p) }

// UsableModel returns an enabled chat model whose provider is enabled.
func (s *Service) UsableModel(ctx context.Context, id string) (*store.Model, *store.Provider, bool) {
	return s.usableModel(ctx, id)
}

func (s *Service) client(p *store.Provider) *llm.Client {
	key := fmt.Sprintf("%s@%d", p.ID, p.UpdatedAt)
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c, ok := s.clients[key]
	if !ok {
		c = llm.NewClient(p.Kind, p.BaseURL, p.APIKey)
		s.clients[key] = c
	}
	return c
}

type autoRunPlan struct {
	mode         RouteMode
	candidates   []RouteCandidate
	currentModel *store.Model
	recentTurns  []RouteTurnSummary
	userMessage  string
	hasImages    bool
	deepThink    bool
}

// planTurnModel decides whether this turn resolves synchronously to a fixed model
// or runs Auto routing inside startRun.
func (s *Service) planTurnModel(ctx context.Context, conv *store.Conversation, userID, userText string, needVision, think, forceInitial bool, beforeMsgID string) (*store.Model, *store.Provider, *autoRunPlan, error) {
	if !conv.AutoMode || !s.AutoEnabled(ctx) {
		m, p, err := s.resolveModel(ctx, conv, userID, needVision)
		return m, p, nil, err
	}
	candidates, err := s.buildRouteCandidates(ctx, needVision)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(candidates) == 1 {
		c := candidates[0]
		if conv.ModelID == nil || *conv.ModelID != c.Model.ID {
			id := c.Model.ID
			_, _ = s.Store.UpdateConversation(ctx, conv.ID, userID, store.ConversationPatch{ModelID: &id})
			conv.ModelID = &id
		}
		return c.Model, c.Provider, nil, nil
	}

	var curModel *store.Model
	var curProv *store.Provider
	if conv.ModelID != nil {
		if m, p, ok := s.usableModel(ctx, *conv.ModelID); ok && (!needVision || m.Vision) {
			curModel, curProv = m, p
		}
	}

	if curModel == nil || forceInitial {
		return nil, nil, &autoRunPlan{
			mode:        RouteInitial,
			candidates:  candidates,
			userMessage: userText,
			hasImages:   needVision,
			deepThink:   think,
		}, nil
	}

	if !s.AutoDynamic(ctx) {
		return curModel, curProv, nil, nil
	}
	if !needVision && isShortContinuation(userText) {
		return curModel, curProv, nil, nil
	}

	var recent []RouteTurnSummary
	if hist, hErr := s.Store.ListMessages(ctx, conv.ID, beforeMsgID, 8); hErr == nil {
		for _, m := range hist {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				recent = append(recent, RouteTurnSummary{UserText: m.Content})
			}
		}
		if len(recent) > 3 {
			recent = []RouteTurnSummary{recent[0], recent[len(recent)-2], recent[len(recent)-1]}
		}
	}
	return curModel, curProv, &autoRunPlan{
		mode:         RouteDynamic,
		candidates:   candidates,
		currentModel: curModel,
		recentTurns:  recent,
		userMessage:  userText,
		hasImages:    needVision,
		deepThink:    think,
	}, nil
}

func isBadRequest(err error) bool {
	var ae *llm.APIError
	return errors.As(err, &ae) && (ae.Status == 400 || ae.Status == 422)
}

func isEffortBadRequest(err error, effort string) bool {
	var ae *llm.APIError
	if !errors.As(err, &ae) || (ae.Status != 400 && ae.Status != 422) {
		return false
	}
	msg := strings.ToLower(ae.Message)
	hasReject := strings.Contains(msg, "unsupported") || strings.Contains(msg, "not supported") ||
		strings.Contains(msg, "invalid") || strings.Contains(msg, "unrecognized") || strings.Contains(msg, "unknown")
	if !hasReject {
		return false
	}
	if strings.Contains(msg, "reasoning_effort") || strings.Contains(msg, "reasoning.effort") || strings.Contains(msg, "reasoning") {
		return true
	}
	if e := strings.ToLower(strings.TrimSpace(effort)); e != "" {
		return strings.Contains(msg, "'"+e+"'") || strings.Contains(msg, `"`+e+`"`)
	}
	return false
}

func fallbackEffort(err error, effort string) string {
	e := strings.ToLower(strings.TrimSpace(effort))
	var ae *llm.APIError
	if errors.As(err, &ae) && (e == "none" || e == "minimal") {
		msg := strings.ToLower(ae.Message)
		if strings.Contains(msg, "'low'") || strings.Contains(msg, `"low"`) {
			return "low"
		}
	}
	return ""
}

func effortFor(m *store.Model, think bool) string {
	if think && m.ThinkEffort != "" {
		return m.ThinkEffort
	}
	return m.DefaultEffort
}

// fallbackModel returns the configured fallback model when it is usable and differs from primaryID.
func (s *Service) fallbackModel(ctx context.Context, primaryID string) (*store.Model, *store.Provider) {
	id, _ := s.Store.GetSetting(ctx, "fallback_model_id")
	if id == "" || id == primaryID {
		return nil, nil
	}
	m, p, ok := s.usableModel(ctx, id)
	if !ok {
		return nil, nil
	}
	return m, p
}

// RepairSettings keeps the model settings pointing at usable models after the admin
// deletes or disables providers and models: a broken default moves to the first usable
// model (vision preferred), a broken fallback or title model is cleared.
func (s *Service) RepairSettings(ctx context.Context) error {
	autoOn, _ := s.Store.GetSetting(ctx, SettingAutoEnabled)
	autoJudge, _ := s.Store.GetSetting(ctx, SettingAutoModelID)
	if autoJudge != "" {
		if _, _, ok := s.usableModel(ctx, autoJudge); !ok {
			nextJudge := ""
			if autoOn == "1" {
				if jm, _ := s.resolveAutoJudge(ctx); jm != nil {
					nextJudge = jm.ID
				}
			}
			if err := s.Store.SetSetting(ctx, SettingAutoModelID, nextJudge); err != nil {
				return err
			}
			if nextJudge == "" && autoOn == "1" {
				if err := s.Store.SetSetting(ctx, SettingAutoEnabled, ""); err != nil {
					return err
				}
			}
		}
	} else if autoOn == "1" {
		if jm, _ := s.resolveAutoJudge(ctx); jm != nil {
			if err := s.Store.SetSetting(ctx, SettingAutoModelID, jm.ID); err != nil {
				return err
			}
		} else if err := s.Store.SetSetting(ctx, SettingAutoEnabled, ""); err != nil {
			return err
		}
	}

	def, err := s.Store.GetSetting(ctx, "default_model_id")
	if err != nil {
		return err
	}
	defValid := false
	if def == AutoModelID {
		defValid = s.AutoEnabled(ctx)
	} else {
		_, _, defValid = s.usableModel(ctx, def)
	}
	if !defValid {
		next := ""
		if vm, _ := s.firstVisionModel(ctx); vm != nil {
			next = vm.ID
		} else if ms, err := s.Store.ListModels(ctx, true); err == nil && len(ms) > 0 {
			next = ms[0].ID
		}
		if next != def {
			s.Log.Info("default model changed", "from", def, "to", next)
			if err := s.Store.SetSetting(ctx, "default_model_id", next); err != nil {
				return err
			}
		}
	}
	for _, k := range []string{"fallback_model_id", "title_model_id", "image_model_id", "memory_model_id", "embedding_model_id"} {
		v, err := s.Store.GetSetting(ctx, k)
		if err != nil {
			return err
		}
		m, _, ok := s.usableAny(ctx, v)
		want := store.KindChat
		if k == "image_model_id" {
			want = store.KindImage
		}
		if k == "embedding_model_id" {
			want = store.KindEmbedding
		}
		if v != "" && (!ok || m.Kind != want) {
			if err := s.Store.SetSetting(ctx, k, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// streamWithRetry retries once if the upstream fails before producing any output.
func (s *Service) streamWithRetry(ctx context.Context, c *llm.Client, req llm.Request, on func(llm.Event)) (*llm.Result, error) {
	for attempt := 0; ; attempt++ {
		produced := false
		res, err := c.Stream(ctx, req, func(ev llm.Event) { produced = true; on(ev) })
		if err == nil || produced || attempt >= 1 || ctx.Err() != nil || !retryable(err) {
			return res, err
		}
		s.Log.Info("retrying upstream", "err", err)
		select {
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func retryable(err error) bool {
	var ae *llm.APIError
	if errors.As(err, &ae) {
		return ae.Retryable()
	}
	return true
}

func friendlyError(err error) string {
	var ae *llm.APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Status == 429:
			return "请求太频繁了，请稍等一会儿再试"
		case ae.Status == 401 || ae.Status == 403:
			return "模型服务认证失败，请联系管理员"
		case ae.Status == 402:
			return "模型服务的余额不足了，请联系管理员充值"
		case ae.Status == 404:
			return "这个模型暂时用不了（上游找不到它），请联系管理员"
		case ae.Status == 400:
			return "模型无法处理这条消息：" + ae.Message
		case ae.Status >= 500:
			return "模型服务暂时不可用，请稍后再试"
		}
		return ae.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "模型响应超时，请稍后再试"
	}
	return "网络连接出了点问题，请重试"
}
