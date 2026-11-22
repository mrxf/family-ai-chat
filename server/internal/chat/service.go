package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/skills"
	"nuan/internal/store"
)

// Errors returned to the API layer.
var (
	ErrBusy            = errors.New("too many concurrent generations")
	ErrConvBusy        = errors.New("conversation is already generating")
	ErrNoModel         = errors.New("no model available")
	ErrEmptyMessage    = errors.New("empty message")
	ErrNothingToRedo   = errors.New("nothing to regenerate")
	ErrPrivateMismatch = errors.New("conversation privacy mismatch")
	errShutdown        = errors.New("server shutting down")
	errUserCancel      = errors.New("cancelled by user")
)

const (
	maxRunsPerUser   = 3
	maxImageMessages = 4 // only the most recent N user messages keep their images
)

type MemoryService interface {
	Allowed(context.Context, *store.User, *string) bool
	Inject(context.Context, *store.User, *string, string) memory.Context
	InjectTurn(context.Context, *store.User, memory.InjectRequest) memory.Context
	Recall(context.Context, *store.User, *string, string, memory.RecallRequest) string
	ReadConversation(context.Context, *store.User, *string, string, string) string
	StartImport(*store.User, string) (memory.ImportJob, error)
	ImportStatus(string) (memory.ImportJob, bool)
	CancelImport(string)
	DrainEmbeddings(context.Context)
}
type MemoryWorker interface {
	Kick(string, time.Duration)
	Shutdown(context.Context)
}

type Service struct {
	Store        *store.Store
	Hub          *Hub
	Log          *slog.Logger
	DataDir      string
	Memory       MemoryService
	MemoryWorker MemoryWorker
	Search       WebSearchService
	Skills       skills.Provider

	skillMu     sync.Mutex
	skillLeases map[string]map[string]*skillLease

	clientsMu sync.Mutex
	clients   map[string]*llm.Client // by provider id + updated_at

	routersMu sync.RWMutex
	routers   map[string]ModelRouter

	noTools sync.Map // model id@updated_at -> upstream rejected tools

	drawMu      sync.Mutex
	drawPending map[string]int // user id -> pictures being drawn (daily limit)

	compactMu      sync.Mutex
	compactRunning map[string]bool
	compactSlots   chan struct{}
	compactWg      sync.WaitGroup
	compactCtx     context.Context
	compactCancel  context.CancelFunc
	compactClosed  bool

	suggestMu      sync.Mutex
	suggestRunning map[string]bool
}

func NewService(st *store.Store, dataDir string, log *slog.Logger) *Service {
	cctx, ccancel := context.WithCancel(context.Background())
	svc := &Service{
		Store:          st,
		Hub:            NewHub(),
		Log:            log,
		DataDir:        dataDir,
		clients:        map[string]*llm.Client{},
		compactRunning: map[string]bool{},
		compactSlots:   make(chan struct{}, 2),
		compactCtx:     cctx,
		compactCancel:  ccancel,
		suggestRunning: map[string]bool{},
	}
	svc.RegisterRouter(&llmModelRouter{svc: svc})
	svc.Skills = skills.NewRegistry(
		svc.buildScheduleSkill(),
		svc.buildComponentsSkill(),
		svc.buildAskUserSkill(),
	)
	return svc
}

func (s *Service) bgCtx() context.Context {
	if s.compactCtx != nil {
		return s.compactCtx
	}
	return context.Background()
}

// SendInput is a new user message.
type SendInput struct {
	ConversationID  string // empty = create a new conversation
	AssistantID     string // used when creating a conversation
	ModelID         string // optional override; persisted on the conversation
	Content         string
	Attachments     []store.Attachment
	QuotedComponent *ComponentQuoteRef // optional component reference attached to this user message
	EditOf          string             // replace this user message and everything after it
	Think           bool               // deep thinking mode
	Private         bool               // ephemeral / private chat (no history, no memory, immediate file cleanup)
}

type SendResult struct {
	Conversation *store.Conversation `json:"conversation"`
	UserMessage  *store.Message      `json:"user_message,omitempty"`
	Assistant    *store.Message      `json:"assistant_message"`
}

// Send stores the user message, creates the assistant placeholder and starts generating.
func (s *Service) Send(ctx context.Context, user *store.User, in SendInput) (*SendResult, error) {
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" && len(in.Attachments) == 0 {
		return nil, ErrEmptyMessage
	}
	if s.Hub.ActiveForUser(user.ID) >= maxRunsPerUser {
		return nil, ErrBusy
	}
	for i, a := range in.Attachments {
		f, err := s.Store.GetFile(ctx, a.FileID, user.ID)
		if err != nil {
			return nil, fmt.Errorf("attachment %s: %w", a.FileID, err)
		}
		in.Attachments[i] = store.Attachment{FileID: f.ID, Mime: f.Mime, Width: f.Width, Height: f.Height}
	}

	var conv *store.Conversation
	var assistant *store.Assistant
	var err error
	switchedToAuto := false
	autoOn := s.AutoEnabled(ctx)

	if in.ConversationID == "" {
		conv = &store.Conversation{UserID: user.ID, Private: in.Private}
		if in.AssistantID != "" {
			a, err := s.Store.GetAssistant(ctx, in.AssistantID, user.ID)
			if err != nil {
				return nil, fmt.Errorf("assistant: %w", err)
			}
			assistant = a
			conv.AssistantID = &a.ID
		}
		switch {
		case in.ModelID == AutoModelID:
			conv.AutoMode = autoOn
		case in.ModelID != "":
			if _, mErr := s.Store.GetModel(ctx, in.ModelID); mErr == nil {
				conv.ModelID = &in.ModelID
			}
		default:
			pinnedByAssistant := assistant != nil && assistant.ModelID != nil && *assistant.ModelID != ""
			if !pinnedByAssistant && autoOn {
				if def, _ := s.Store.GetSetting(ctx, "default_model_id"); def == AutoModelID {
					conv.AutoMode = true
				}
			}
		}
		if err := s.Store.CreateConversation(ctx, conv); err != nil {
			return nil, err
		}
	} else {
		conv, err = s.Store.GetConversation(ctx, in.ConversationID, user.ID)
		if err != nil {
			return nil, err
		}
		if in.Private && !conv.Private {
			return nil, ErrPrivateMismatch
		}
		if s.Hub.ActiveInConversation(conv.ID) != nil {
			return nil, ErrConvBusy
		}
		switch {
		case in.ModelID == AutoModelID:
			if autoOn && !conv.AutoMode {
				on := true
				if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{AutoMode: &on}); err != nil {
					return nil, err
				}
				switchedToAuto = true
			}
		case in.ModelID != "":
			if _, mErr := s.Store.GetModel(ctx, in.ModelID); mErr == nil {
				if conv.AutoMode || conv.ModelID == nil || *conv.ModelID != in.ModelID {
					off := false
					if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{AutoMode: &off, ModelID: &in.ModelID}); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	editingFirstTurn := false
	if in.EditOf != "" {
		old, err := s.Store.GetMessageForUser(ctx, in.EditOf, user.ID)
		if err != nil || old.ConversationID != conv.ID || old.Role != "user" {
			return nil, store.ErrNotFound
		}
		if prior, pErr := s.Store.ListMessages(ctx, conv.ID, in.EditOf, 1); pErr == nil && len(prior) == 0 {
			editingFirstTurn = true
		}
	}

	needVision := len(in.Attachments) > 0
	model, provider, autoPlan, err := s.planTurnModel(ctx, conv, user.ID, in.Content, needVision, in.Think, switchedToAuto || editingFirstTurn, in.EditOf)
	if err != nil {
		return nil, err
	}

	var quoted *store.QuotedComponent
	if in.QuotedComponent != nil {
		quoted, err = s.resolveQuotedComponent(ctx, user.ID, conv, in.QuotedComponent)
		if err != nil {
			return nil, err
		}
	}

	userMsg := &store.Message{Role: "user", Content: in.Content, Attachments: in.Attachments, QuotedComponent: quoted, Status: store.StatusDone}
	var asstModelID *string
	if model != nil {
		id := model.ID
		asstModelID = &id
	}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: asstModelID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, TruncateFrom: in.EditOf, Append: []*store.Message{userMsg, asst}}); err != nil {
		return nil, err
	}
	s.startRun(user, conv, asst, model, provider, in.Think, autoPlan, in.Content)
	return &SendResult{Conversation: conv, UserMessage: userMsg, Assistant: asst}, nil
}

// Regenerate replaces the last assistant reply of a conversation.
func (s *Service) Regenerate(ctx context.Context, user *store.User, convID, modelID string, think bool) (*SendResult, error) {
	conv, err := s.Store.GetConversation(ctx, convID, user.ID)
	if err != nil {
		return nil, err
	}
	if s.Hub.ActiveInConversation(conv.ID) != nil {
		return nil, ErrConvBusy
	}
	if s.Hub.ActiveForUser(user.ID) >= maxRunsPerUser {
		return nil, ErrBusy
	}
	last, err := s.Store.ListMessages(ctx, conv.ID, "", 4)
	if err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, ErrNothingToRedo
	}
	truncate := ""
	tail := last[len(last)-1]
	if tail.Role == "assistant" {
		truncate = tail.ID
	}
	switchedToAuto := false
	autoOn := s.AutoEnabled(ctx)
	switch {
	case modelID == AutoModelID:
		if autoOn && !conv.AutoMode {
			on := true
			if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{AutoMode: &on}); err != nil {
				return nil, err
			}
			switchedToAuto = true
		}
	case modelID != "":
		if _, mErr := s.Store.GetModel(ctx, modelID); mErr == nil {
			off := false
			if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{AutoMode: &off, ModelID: &modelID}); err != nil {
				return nil, err
			}
		}
	}
	needVision := false
	lastUserText := ""
	userCount := 0
	for _, m := range last {
		if m.Role == "user" {
			userCount++
			lastUserText = m.Content
			if len(m.Attachments) > 0 {
				needVision = true
			}
		}
	}
	isFirstTurn := userCount <= 1 && (truncate == "" || len(last) <= 2)
	model, provider, autoPlan, err := s.planTurnModel(ctx, conv, user.ID, lastUserText, needVision, think, switchedToAuto || isFirstTurn, truncate)
	if err != nil {
		return nil, err
	}
	var asstModelID *string
	if model != nil {
		id := model.ID
		asstModelID = &id
	}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: asstModelID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, TruncateFrom: truncate, Append: []*store.Message{asst}}); err != nil {
		return nil, err
	}
	s.startRun(user, conv, asst, model, provider, think, autoPlan, lastUserText)
	return &SendResult{Conversation: conv, Assistant: asst}, nil
}

// Cancel stops a running generation owned by the user.
func (s *Service) Cancel(userID, messageID string) bool {
	r := s.Hub.Get(messageID)
	if r == nil || r.UserID != userID {
		return false
	}
	r.Cancel(errUserCancel)
	return true
}

func (s *Service) Shutdown(ctx context.Context) {
	s.Hub.Shutdown(ctx, errShutdown)
	s.compactMu.Lock()
	s.compactClosed = true
	cancel := s.compactCancel
	s.compactMu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() { s.compactWg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	if s.MemoryWorker != nil {
		s.MemoryWorker.Shutdown(ctx)
	}
}

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

	// Subsequent turn in Auto mode: keep the first-turn model unless dynamic updates are enabled.
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
			// Keep the first question (original topic anchor) plus the 2 latest user turns.
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

// FinalEvent is the payload pushed to subscribers when a run ends.
type FinalEvent struct {
	Message *store.Message `json:"message"`
}

func (s *Service) startRun(user *store.User, convOrig *store.Conversation, asst *store.Message, model *store.Model, provider *store.Provider, think bool, autoPlan *autoRunPlan, userText string) {
	convCopy := *convOrig
	conv := &convCopy
	run := &Run{MessageID: asst.ID, ConversationID: conv.ID, UserID: user.ID}
	if asst.ModelID != nil {
		run.modelID = *asst.ModelID
	}
	needTitle := conv.Title == "" && !conv.Private
	s.Hub.start(run, func(ctx context.Context) {
		started := time.Now()
		msg := *asst
		var firstToken time.Duration
		var usage llm.Usage
		turn := &toolTurn{
			user: user, convID: conv.ID, assistantID: conv.AssistantID,
			asstID: asst.ID, userQuestion: userText, run: run, private: conv.Private,
		}

		autoEscalated := false
		if autoPlan != nil {
			var asstMeta *store.Assistant
			if conv.AssistantID != nil {
				asstMeta, _ = s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID)
			}
			run.setActivity("正在挑选合适的模型…")
			dec, rerr := s.activeRouter(ctx).Route(ctx, RouteRequest{
				Mode:         autoPlan.mode,
				Candidates:   autoPlan.candidates,
				CurrentModel: autoPlan.currentModel,
				Assistant:    asstMeta,
				RecentTurns:  autoPlan.recentTurns,
				UserMessage:  autoPlan.userMessage,
				HasImages:    autoPlan.hasImages,
				DeepThink:    autoPlan.deepThink,
			})
			run.setActivity("")
			if dec != nil {
				turn.cost += dec.Cost
			}
			if rerr == nil && dec != nil && dec.Model != nil && dec.Provider != nil {
				if dec.Switched && autoPlan.currentModel != nil {
					prevTier := llm.ProfileModel(autoPlan.currentModel.UpstreamModel, autoPlan.currentModel.DisplayName).Tier
					newTier := llm.ProfileModel(dec.Model.UpstreamModel, dec.Model.DisplayName).Tier
					autoEscalated = newTier >= llm.TierStrong && newTier > prevTier
				}
				model, provider = dec.Model, dec.Provider
				id := model.ID
				msg.ModelID = &id
				run.setModelID(id)
				if conv.ModelID == nil || *conv.ModelID != id {
					pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, _ = s.Store.UpdateConversation(pctx, conv.ID, user.ID, store.ConversationPatch{ModelID: &id})
					pcancel()
					conv.ModelID = &id
				}
				s.Log.Info("auto routed model", "conv", conv.ID, "mode", autoPlan.mode, "model", model.UpstreamModel, "switched", dec.Switched, "reason", dec.Reason)
			}
		}

		if model == nil || provider == nil {
			needVis := autoPlan != nil && autoPlan.hasImages
			m, p, rerr := s.resolveModel(ctx, conv, user.ID, needVis)
			if rerr != nil {
				msg.Status = store.StatusError
				msg.Error = friendlyError(rerr)
				pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = s.Store.FinishMessage(pctx, &msg)
				cancel()
				run.finish(&FinalEvent{Message: &msg})
				return
			}
			model, provider = m, p
			id := model.ID
			msg.ModelID = &id
			run.setModelID(id)
		}

		if needTitle {
			go s.generateTitle(conv.ID, user.ID, model, provider, run)
		}

		// After a tool round the model continues in a new paragraph.
		needBreak, produced := false, false
		onEvent := func(ev llm.Event) {
			produced = true
			if firstToken == 0 {
				firstToken = time.Since(started)
			}
			if ev.Reasoning != "" || ev.Content != "" {
				run.setActivity("")
			}
			if ev.Reasoning != "" {
				run.appendReasoning(ev.Reasoning)
			}
			if ev.Content != "" {
				if needBreak {
					needBreak = false
					if run.contentLen() > 0 {
						ev.Content = "\n\n" + strings.TrimLeft(ev.Content, "\n")
					}
				}
				run.appendContent(ev.Content)
			}
		}
		addUsage := func(res *llm.Result, m *store.Model) {
			if res == nil || res.Usage == nil {
				return
			}
			u := res.Usage
			usage.PromptTokens += u.PromptTokens
			usage.CompletionTokens += u.CompletionTokens
			cost := u.Cost
			if cost <= 0 {
				cost = m.EstimateCost(u.PromptTokens, u.CompletionTokens)
			}
			usage.Cost += cost
		}

		ts := s.toolsFor(ctx, user, conv, model)
		if len(ts.leasedSkills) > 0 {
			turn.activeSkills = map[string]bool{}
			for k, v := range ts.leasedSkills {
				turn.activeSkills[k] = v
			}
		}
		if !conv.Private && s.Memory != nil && s.Memory.Allowed(ctx, user, conv.AssistantID) {
			run.setActivity("正在翻看相关记忆…")
		}
		req, err := s.buildRequestWithOpts(ctx, user, conv, asst.ID, model, ts, buildOpts{autoEscalated: autoEscalated})
		tools := req.tools
		turn.foldedBeforeID = req.foldedBeforeID
		run.setActivity("")
		if err == nil && req.recalled {
			run.addDoneStep("memory", "已关联相关记忆背景", "")
		}
		maxRounds := maxToolRounds
		skillBonus := 0
		effortOverride := ""
		effortOverridden := false
		for round := 0; err == nil && round < maxRounds; round++ {
			if effortOverridden {
				req.ReasoningEffort = effortOverride
			} else {
				req.ReasoningEffort = effortFor(model, think)
			}
			req.Tools = tools
			if round == maxRounds-1 {
				req.Tools = nil // last round: answer in words
			}
			if round > 0 {
				if turn.postActivity != "" {
					run.setActivity(turn.postActivity)
				} else {
					run.setActivity("正在整理回答…")
				}
			}
			roundStart := run.contentLen()
			roundReasoningStart := run.reasoningLen()
			produced = false
			var res *llm.Result
			res, err = s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
			// Upstream rejects reasoning_effort (e.g. "none"/"minimal" on reasoning-only models):
			// downgrade effort and retry with tools intact before blaming tools.
			if err != nil && !produced && req.ReasoningEffort != "" && isEffortBadRequest(err, req.ReasoningEffort) && ctx.Err() == nil {
				next := fallbackEffort(err, req.ReasoningEffort)
				s.Log.Warn("upstream rejected reasoning_effort, retrying", "message_id", asst.ID, "model", model.UpstreamModel, "from", req.ReasoningEffort, "to", next, "err", err)
				req.ReasoningEffort = next
				effortOverride, effortOverridden = next, true
				res, err = s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
			}
			// Upstream rejects tools: answer without them rather than fail.
			// Only cache the model as no-tools when retrying without tools actually succeeds,
			// so unrelated 400s (bad effort, context limits, etc.) never poison tool support.
			if err != nil && !produced && len(req.Tools) > 0 && isBadRequest(err) && ctx.Err() == nil {
				s.Log.Warn("upstream rejected tools, retrying without", "message_id", asst.ID, "model", model.UpstreamModel, "err", err)
				origTools := req.Tools
				origSys := req.Messages[0].Content
				req.Tools = nil
				req.Messages[0].Content = s.systemPromptWithTools(ctx, user, conv, toolSet{}, req.core, formatSummarySection(req.summaryText, false, false))
				retryRes, retryErr := s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
				if retryErr == nil {
					s.markToolsRejected(model)
					tools = nil
					res, err = retryRes, nil
				} else {
					req.Tools = origTools
					req.Messages[0].Content = origSys
				}
			}
			// Nothing reached the user yet: let the fallback model answer instead of showing an error.
			if err != nil && round == 0 && firstToken == 0 && ctx.Err() == nil {
				fm, fp := s.fallbackModel(ctx, model.ID)
				if fm == nil && autoPlan != nil {
					for _, cand := range autoPlan.candidates {
						if cand.Model.ID != model.ID {
							fm, fp = cand.Model, cand.Provider
							break
						}
					}
				}
				if fm != nil {
					s.Log.Warn("primary model failed, using fallback", "message_id", asst.ID, "model", model.UpstreamModel, "fallback", fm.UpstreamModel, "err", err)
					fts := s.toolsFor(ctx, user, conv, fm)
					freq, ferr := s.buildRequestWithOpts(ctx, user, conv, asst.ID, fm, fts, buildOpts{autoEscalated: autoEscalated})
					if ferr == nil {
						model, provider, tools, req = fm, fp, freq.tools, freq
						turn.foldedBeforeID = freq.foldedBeforeID
						if len(fts.leasedSkills) > 0 {
							turn.activeSkills = map[string]bool{}
							for k, v := range fts.leasedSkills {
								turn.activeSkills[k] = v
							}
						} else {
							turn.activeSkills = nil
						}
						id := fm.ID
						msg.ModelID = &id
						run.setModelID(id)
						req.ReasoningEffort = effortFor(fm, think)
						req.Tools = tools
						res, err = s.streamWithRetry(ctx, s.client(fp), req.Request, onEvent)
					}
				}
			}
			addUsage(res, model)
			if err != nil || res == nil || len(res.ToolCalls) == 0 || len(req.Tools) == 0 {
				break
			}
			if firstToken == 0 {
				firstToken = time.Since(started) // the model answered with tool calls only
			}
			// Hand the calls and their results back to the model for the next round.
			calls := res.ToolCalls
			for i := range calls {
				if calls[i].ID == "" {
					calls[i].ID = fmt.Sprintf("call_%d_%d", round, i)
				}
			}
			content, reasoning := run.text()
			req.Messages = append(req.Messages, llm.Message{
				Role:      "assistant",
				Content:   strings.TrimSpace(content[roundStart:]),
				Reasoning: strings.TrimSpace(reasoning[roundReasoningStart:]),
				ToolCalls: calls,
			})
			turn.postActivity = ""
			turn.unlockedTools = nil
			turn.usedReadSkill = false
			results, stopTools := s.runTools(ctx, turn, calls)
			for i, result := range results {
				req.Messages = append(req.Messages, llm.Message{Role: "tool", ToolCallID: calls[i].ID, Content: result})
			}
			if len(turn.unlockedTools) > 0 {
				tools = appendUniqueTools(tools, turn.unlockedTools...)
			}
			if turn.usedReadSkill && skillBonus < maxSkillBonusRounds {
				skillBonus++
				maxRounds++
			}
			if stopTools {
				tools = nil
			}
			needBreak = true
			if ctx.Err() != nil {
				err = context.Cause(ctx)
				break
			}
		}
		if !conv.Private && err == nil {
			s.tickSkillLeases(conv.ID, turn.activatedSkills, turn.usedSkillOwners)
		}

		run.setActivity("")
		msg.Content, msg.Reasoning = run.text()
		snap := run.Snapshot()
		msg.Attachments = snap.Attachments
		msg.Sources = snap.Sources
		msg.Components = snap.Components
		// Pictures or rich cards already shown to the user must not turn into an error
		// if the final wrap-up round hiccups.
		if err != nil && (len(msg.Attachments) > 0 || len(msg.Components) > 0) && ctx.Err() == nil {
			s.Log.Warn("reply failed after rendering media/components, keeping partial output", "message_id", asst.ID, "err", err)
			err = nil
			if strings.TrimSpace(msg.Content) == "" && len(msg.Attachments) > 0 && len(msg.Components) == 0 {
				msg.Content = "画好啦，看看喜不喜欢～需要调整的话告诉我。"
			}
		}
		msg.PromptTokens, msg.CompletionTokens = usage.PromptTokens, usage.CompletionTokens
		msg.Cost = usage.Cost + turn.cost
		msg.FirstTokenMs = firstToken.Milliseconds()
		msg.DurationMs = time.Since(started).Milliseconds()
		cause := context.Cause(ctx)
		switch {
		case err == nil:
			msg.Status = store.StatusDone
		case errors.Is(cause, errUserCancel):
			msg.Status = store.StatusCancelled
		case errors.Is(cause, errShutdown):
			msg.Status = store.StatusInterrupted
		default:
			msg.Status = store.StatusError
			msg.Error = friendlyError(err)
			s.Log.Warn("generation failed", "message_id", msg.ID, "model", model.UpstreamModel, "err", err)
		}
		pctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Store.FinishMessage(pctx, &msg); err != nil {
			s.Log.Error("persist message", "message_id", msg.ID, "err", err)
		} else if msg.Status == store.StatusDone {
			if !conv.Private && s.MemoryWorker != nil {
				if history, err := s.Store.ListMessages(pctx, conv.ID, msg.ID, 1); err == nil && len(history) > 0 && memory.Trigger(history[0].Content) {
					s.MemoryWorker.Kick(conv.ID, 5*time.Second)
				}
			}
			replyTokens := estimateTokens(msg.Content)
			if req.compactAfterTurn || req.uncompactedTokens+replyTokens >= req.budgets.compactTrigger {
				s.maybeCompact(compactHint{
					conv:           conv,
					user:           user,
					turnModel:      model,
					turnProvider:   provider,
					lowWaterTokens: req.budgets.lowWater,
					coldTailTokens: req.budgets.coldTail,
				})
			}
		}
		_ = s.Store.TouchConversation(pctx, conv.ID)
		s.Log.Info("generation finished", "message_id", msg.ID, "model", model.UpstreamModel, "status", msg.Status,
			"first_token_ms", msg.FirstTokenMs, "duration_ms", msg.DurationMs, "tokens", msg.CompletionTokens, "images", len(msg.Attachments))
		run.finish(&FinalEvent{Message: &msg})
	})
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
	// Repair Auto judge model first so AutoEnabled reflects the repaired state.
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
	return true // network errors
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

// ---- context building ----

const basePrompt = `你是「小暖」，一位耐心、温暖、可靠的 AI 助手，正在为一个家庭的成员提供帮助。
- 用简体中文回答（除非用户使用其他语言）。先给结论，再给必要的解释；语言通俗易懂，避免堆砌术语。
- 回答长度与问题匹配：简单问题简短回答，复杂问题再分点展开。适度使用 Markdown（小标题、列表、加粗），不要过度排版。
- 涉及健康、用药、法律、投资等问题时，给出有用的信息，同时提醒在必要时咨询医生或专业人士。
- 如果用户描述的情况疑似电信诈骗（如陌生人要求转账、索要验证码、冒充公检法或客服、高回报投资等），要明确、直接地提醒用户注意防骗。
- 不确定的事情就说不确定，不要编造。`

const privatePrompt = `

# 隐私对话模式
本次对话为「隐私对话」，对话结束后记录与图片会自动清除，系统不会读取也不会保存任何跨对话记忆。如果用户请你在以后的对话里「记住」某件事，请温和说明当前是隐私对话、关闭后不会留存；需要长期记住的内容可以退出隐私对话后再说，或在「设置 → 小暖记住的事」里手动添加。`

func (s *Service) systemPrompt(ctx context.Context, user *store.User, conv *store.Conversation, canDraw bool, extra ...string) string {
	return s.systemPromptWithTools(ctx, user, conv, toolSet{canDraw: canDraw}, extra...)
}

// systemPromptWithTools assembles the system prompt. extra[0] is core memory (non-private only);
// extra[1] is the optional conversation rolling summary section (# 本次对话的前情提要).
func (s *Service) systemPromptWithTools(ctx context.Context, user *store.User, conv *store.Conversation, ts toolSet, extra ...string) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString(s.drawPrompt(ctx, ts.canDraw))
	b.WriteString(s.searchPrompt(ctx, ts))
	if ts.canRecall && !conv.Private {
		b.WriteString(memoryToolPrompt)
	}
	if ts.skillCatalog != "" && !conv.Private {
		b.WriteString(ts.skillCatalog)
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && strings.TrimSpace(a.SystemPrompt) != "" {
			fmt.Fprintf(&b, "\n\n# 本次对话的角色设定（%s）\n%s", a.Name, a.SystemPrompt)
		}
	}
	now := time.Now().In(shanghai)
	fmt.Fprintf(&b, "\n\n当前日期：%s（%s）。", now.Format("2006年1月2日"), weekdays[now.Weekday()])
	if conv.Private {
		b.WriteString(privatePrompt)
	} else {
		if user.DisplayName != "" {
			fmt.Fprintf(&b, "\n正在和你对话的用户称呼是「%s」。", user.DisplayName)
		}
		if len(extra) > 0 {
			b.WriteString(extra[0])
		}
	}
	if len(extra) > 1 && extra[1] != "" {
		b.WriteString(extra[1])
	}
	return b.String()
}

var weekdays = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

var shanghai = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// estimateTokens is a rough, conservative estimate: CJK ≈ 1 token per rune, others ≈ 4 bytes per token.
func estimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if r >= 0x2E80 {
			cjk++
		} else {
			other += utf8.RuneLen(r)
		}
	}
	return cjk + other/4 + 4
}

const imageTokens = 1100

type chatRequest struct {
	llm.Request
	tools             []llm.Tool
	core              string
	summaryText       string
	foldedBeforeID    string
	recalled          bool
	uncompactedTokens int
	compactAfterTurn  bool
	budgets           contextBudgets
}

type buildOpts struct {
	autoEscalated bool // Auto mode just switched up from a lighter model to T4/T5 flagship
}

func (s *Service) buildRequest(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, canDraw bool) (chatRequest, error) {
	return s.buildRequestWithTools(ctx, user, conv, asstID, model, toolSet{canDraw: canDraw})
}

func (s *Service) buildRequestWithTools(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, ts toolSet) (chatRequest, error) {
	return s.buildRequestWithOpts(ctx, user, conv, asstID, model, ts, buildOpts{})
}

func (s *Service) buildRequestWithOpts(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, ts toolSet, opts buildOpts) (chatRequest, error) {
	convSummary, summaryCursor, _ := s.Store.GetConversationSummary(ctx, conv.ID)
	history, hasOlderInDB, err := s.Store.ListMessagesAfter(ctx, conv.ID, summaryCursor, asstID, historyFetchLimit)
	if err != nil {
		return chatRequest{}, err
	}
	lastUser := -1
	var recentUsers []string
	uncompactedTokens := 0
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if !(m.Role == "assistant" && len(m.Attachments) == 0 && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "")) {
			uncompactedTokens += estimateTokens(m.Content) + len(m.Attachments)*imageTokens
		}
		if m.Role == "user" {
			if lastUser < 0 {
				lastUser = i
			}
			if txt := strings.TrimSpace(m.Content); txt != "" && len(recentUsers) < 3 {
				recentUsers = append([]string{txt}, recentUsers...)
			}
		}
	}
	mem := memory.Context{}
	if s.Memory != nil && !conv.Private {
		mem = s.Memory.InjectTurn(ctx, user, memory.InjectRequest{
			ConversationID:    conv.ID,
			ConversationTitle: conv.Title,
			AssistantID:       conv.AssistantID,
			UserMessages:      recentUsers,
		})
	}

	summaryText := strings.TrimSpace(convSummary)
	if summaryText == "" && hasOlderInDB && !conv.Private {
		if eps, eErr := s.Store.ConversationEpisodes(ctx, conv.ID, user.ID); eErr == nil && len(eps) > 0 {
			summaryText = strings.Join(eps, "\n- ")
			if len(eps) > 1 {
				summaryText = "- " + summaryText
			}
		}
	}
	if hasOlderInDB && ts.allowTools && !ts.canReadEarlier {
		ts.canReadEarlier = true
		ts.tools = append(append([]llm.Tool(nil), ts.tools...), readEarlierTool)
	}

	summarySection := formatSummarySection(summaryText, hasOlderInDB, ts.canReadEarlier)
	sys := s.systemPromptWithTools(ctx, user, conv, ts, mem.Core, summarySection)
	note := "（系统附注，不是用户说的话，回复时不要写出这种附注：现在是 " + time.Now().In(shanghai).Format("15:04") + "。"
	if !conv.Private {
		if schedBrief := s.UpcomingScheduleBrief(ctx, user.ID, time.Now()); schedBrief != "" {
			note += schedBrief
		}
		if ts.leaseNote != "" {
			note += ts.leaseNote
		}
	}
	if mem.Recall != "" {
		note += "可能相关的往事（请留意每条前的时间跨度；较早的短期症状、情绪或临时安排通常已结束，仅作背景参考——除非用户本轮主动提起，不要把旧事当成正在发生的新事追问或续聊）：" + mem.Recall
	}
	note += "）\n"

	rawWindow := model.ContextWindow
	if rawWindow <= 0 {
		rawWindow = 128000
	}
	reserve := model.MaxOutput
	if reserve <= 0 {
		reserve = 8192
	}
	hardBudget := rawWindow - reserve - estimateTokens(sys)
	budgets := computeContextBudgets(hardBudget)

	// Pictures are numbered (图N) so the model can name the one to modify.
	var num map[string]int
	if ts.canDraw {
		num = map[string]int{}
		if refs, rErr := s.Store.ListConversationImageRefs(ctx, conv.ID, asstID, 500); rErr == nil {
			for i, a := range refs {
				num[a.FileID] = i + 1
			}
		}
	}

	type pickedItem struct {
		lm   llm.Message
		id   string
		cost int
	}
	var picked []pickedItem
	imagesLeft := maxImageMessages
	totalCost := 0
	coldCost := 0
	coldCount := 0
	inColdPast := false
	userTurnsPicked := 0
	droppedInSlice := false
	foldedByCold := false
	var prevCreatedAt int64

	// Walk backwards, keeping recent history within active / cold-resume / escalation budgets.
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if prevCreatedAt > 0 && m.CreatedAt > 0 && prevCreatedAt-m.CreatedAt >= ColdResumeIdle.Milliseconds() {
			inColdPast = true
		}
		if m.CreatedAt > 0 {
			prevCreatedAt = m.CreatedAt
		}

		drew := m.Role == "assistant" && len(m.Attachments) > 0
		hasComps := m.Role == "assistant" && len(m.Components) > 0
		if m.Role == "assistant" && !drew && !hasComps && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "") {
			continue
		}
		lm := llm.Message{Role: m.Role, Content: m.Content}
		if m.Role == "user" && m.QuotedComponent != nil {
			lm.Content = quotedComponentPromptNote(m.QuotedComponent) + lm.Content
		}
		if hasComps {
			lm.Content = componentNote(m.Components) + lm.Content
		}
		if drew {
			lm.Content = drawnNote(m.Attachments, num) + lm.Content
		}
		cost := estimateTokens(lm.Content)
		if m.Role == "user" && len(m.Attachments) > 0 {
			if model.Vision && imagesLeft > 0 {
				imagesLeft--
				parts := []llm.Part{}
				for _, a := range m.Attachments {
					url, err := s.dataURL(ctx, a.FileID, user.ID)
					if err != nil {
						continue
					}
					parts = append(parts, llm.Part{Type: "image_url", ImageURL: &llm.ImageURL{URL: url}})
					cost += imageTokens
				}
				text := lm.Content
				if text == "" {
					text = "（用户发送了图片）"
				}
				if refs := refList(m.Attachments, num); refs != "" {
					text = "（这条消息里的图片：" + refs + "）\n" + text
				}
				lm.Parts = append(parts, llm.Part{Type: "text", Text: text})
			} else {
				n := fmt.Sprintf("%d 张图片", len(m.Attachments))
				if refs := refList(m.Attachments, num); refs != "" {
					n += "（" + refs + "）"
				}
				lm.Content = fmt.Sprintf("[用户发送了 %s，当前已不可见]\n%s", n, lm.Content)
			}
		}
		if i == lastUser {
			lm.Content = note + lm.Content
			if len(lm.Parts) > 0 {
				for j := range lm.Parts {
					if lm.Parts[j].Type == "text" {
						lm.Parts[j].Text = note + lm.Parts[j].Text
						break
					}
				}
			}
			cost += estimateTokens(note)
		}

		if len(picked) > 0 {
			if totalCost+cost > budgets.hardBudget {
				droppedInSlice = true
				break
			}
			if inColdPast && ((coldCost+cost > budgets.coldTail && coldCount >= 2) || coldCount >= coldResumeTailMessages) {
				droppedInSlice = true
				foldedByCold = true
				break
			}
			if opts.autoEscalated && userTurnsPicked >= 1 && totalCost+cost > budgets.lowWater {
				droppedInSlice = true
				break
			}
			if !inColdPast && userTurnsPicked >= minProtectedUserTurns && totalCost+cost > budgets.activeCap {
				droppedInSlice = true
				break
			}
		}

		totalCost += cost
		if inColdPast {
			coldCost += cost
			coldCount++
		}
		if m.Role == "user" {
			userTurnsPicked++
		}
		picked = append(picked, pickedItem{lm: lm, id: m.ID, cost: cost})
	}
	// Drop leading assistant messages so history starts with the user.
	for len(picked) > 0 && picked[len(picked)-1].lm.Role == "assistant" {
		totalCost -= picked[len(picked)-1].cost
		picked = picked[:len(picked)-1]
	}
	if len(picked) == 0 {
		return chatRequest{}, ErrEmptyMessage
	}

	foldedEarlier := hasOlderInDB || droppedInSlice
	foldedBeforeID := ""
	if foldedEarlier {
		foldedBeforeID = picked[len(picked)-1].id
		if summaryText == "" && !conv.Private {
			if eps, eErr := s.Store.ConversationEpisodes(ctx, conv.ID, user.ID); eErr == nil && len(eps) > 0 {
				summaryText = strings.Join(eps, "\n- ")
				if len(eps) > 1 {
					summaryText = "- " + summaryText
				}
			}
		}
		if ts.allowTools && !ts.canReadEarlier {
			ts.canReadEarlier = true
			ts.tools = append(append([]llm.Tool(nil), ts.tools...), readEarlierTool)
		}
		if updatedSection := formatSummarySection(summaryText, true, ts.canReadEarlier); updatedSection != summarySection {
			sys = s.systemPromptWithTools(ctx, user, conv, ts, mem.Core, updatedSection)
			newHard := rawWindow - reserve - estimateTokens(sys)
			for len(picked) > 1 && totalCost > newHard {
				totalCost -= picked[len(picked)-1].cost
				picked = picked[:len(picked)-1]
				for len(picked) > 0 && picked[len(picked)-1].lm.Role == "assistant" {
					totalCost -= picked[len(picked)-1].cost
					picked = picked[:len(picked)-1]
				}
			}
			if len(picked) == 0 {
				return chatRequest{}, ErrEmptyMessage
			}
			foldedBeforeID = picked[len(picked)-1].id
		}
	}

	msgs := make([]llm.Message, 0, len(picked)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: sys})
	for i := len(picked) - 1; i >= 0; i-- {
		msgs = append(msgs, picked[i].lm)
	}
	req := chatRequest{
		Request:           llm.Request{Model: model.UpstreamModel, Messages: msgs, MaxTokens: model.MaxOutput},
		tools:             ts.tools,
		core:              mem.Core,
		summaryText:       summaryText,
		foldedBeforeID:    foldedBeforeID,
		recalled:          mem.Recall != "",
		uncompactedTokens: uncompactedTokens,
		compactAfterTurn:  foldedByCold || (summaryCursor == "" && hasOlderInDB),
		budgets:           budgets,
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && a.Temperature != nil {
			req.Temperature = a.Temperature
		}
	}
	return req, nil
}

// drawnNote tells the model which pictures it drew in an earlier reply
// (the pictures themselves are not sent back). num gives their numbers (图N)
// when the model can draw.
func drawnNote(atts []store.Attachment, num map[string]int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "（系统附注，不是对话内容，回复时不要写这种附注：这条回复里你调用 generate_image 画了 %d 张图并展示给了用户", len(atts))
	for i, a := range atts {
		label := fmt.Sprintf("第 %d 张", i+1)
		if n := num[a.FileID]; n > 0 {
			label += fmt.Sprintf("是图%d", n)
			if src := num[a.Source]; src > 0 {
				label += fmt.Sprintf("，由图%d 修改而来", src)
			}
		}
		if a.Prompt != "" {
			fmt.Fprintf(&b, "；%s，提示词：%s", label, clip(a.Prompt, 300))
		} else if num != nil {
			fmt.Fprintf(&b, "；%s", label)
		}
	}
	b.WriteString("）\n")
	return b.String()
}

// refList is "图3、图4" for a message's pictures (empty when not numbering).
func refList(atts []store.Attachment, num map[string]int) string {
	var parts []string
	for _, a := range atts {
		if n := num[a.FileID]; n > 0 {
			parts = append(parts, fmt.Sprintf("图%d", n))
		}
	}
	return strings.Join(parts, "、")
}

func (s *Service) dataURL(ctx context.Context, fileID, userID string) (string, error) {
	f, err := s.Store.GetFile(ctx, fileID, userID)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}
	return "data:" + f.Mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// ---- titles ----

func (s *Service) generateTitle(convID, userID string, model *store.Model, provider *store.Provider, run *Run) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, err := s.Store.ListMessages(ctx, convID, "", 4)
	if err != nil || len(msgs) == 0 {
		return
	}
	first := ""
	for _, m := range msgs {
		if m.Role == "user" {
			first = m.Content
			if first == "" && len(m.Attachments) > 0 {
				first = "（一张图片）"
			}
			break
		}
	}
	if first == "" {
		return
	}
	if utf8.RuneCountInString(first) > 600 {
		first = string([]rune(first)[:600])
	}
	if tm, err := s.Store.GetSetting(ctx, "title_model_id"); err == nil && tm != "" {
		if m, p, ok := s.usableModel(ctx, tm); ok {
			model, provider = m, p
		}
	}
	req := llm.Request{
		Model: model.UpstreamModel,
		Messages: []llm.Message{
			{Role: "system", Content: "你是标题生成器。根据用户的第一句话，生成一个不超过 12 个汉字的简短中文标题，概括对话主题。只输出标题本身，不要标点、引号或任何解释。"},
			{Role: "user", Content: first},
		},
		MaxTokens: 0,
	}
	req.ReasoningEffort = model.DefaultEffort
	title, _, err := s.client(provider).Complete(ctx, req)
	if err != nil {
		s.Log.Warn("title generation failed", "conv", convID, "err", err)
		return
	}
	title = cleanTitle(title)
	if title == "" {
		return
	}
	if ok, err := s.Store.SetConversationTitle(ctx, convID, title); err == nil && ok {
		run.setTitle(title)
	}
}

func cleanTitle(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.Trim(t, " \t\"'“”‘’「」《》#*。.")
	t = strings.TrimPrefix(t, "标题：")
	t = strings.TrimPrefix(t, "标题:")
	if utf8.RuneCountInString(t) > 20 {
		t = string([]rune(t)[:20])
	}
	return strings.TrimSpace(t)
}
