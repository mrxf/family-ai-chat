package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
	Automation   *AutomationWorker

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
		svc.buildAutomationSkill(),
		svc.buildComponentsSkill(),
		svc.buildAskUserSkill(),
		svc.buildAppControlSkill(),
	)
	svc.Automation = NewAutomationWorker(svc)
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
	ClientContext   *ClientContext
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

	userMsg := &store.Message{
		Role:            "user",
		SenderUserID:    user.ID,
		SenderName:      user.DisplayName,
		SenderAvatar:    user.Avatar,
		Content:         in.Content,
		Attachments:     in.Attachments,
		QuotedComponent: quoted,
		Status:          store.StatusDone,
	}
	var asstModelID *string
	if model != nil {
		id := model.ID
		asstModelID = &id
	}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: asstModelID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, TruncateFrom: in.EditOf, Append: []*store.Message{userMsg, asst}}); err != nil {
		return nil, err
	}
	if conv.IsShared && len(in.Attachments) > 0 {
		fids := make([]string, 0, len(in.Attachments))
		for _, a := range in.Attachments {
			fids = append(fids, a.FileID)
		}
		_ = s.Store.GrantFilesToConversationMembers(ctx, conv.ID, fids)
	}
	s.startRun(user, conv, asst, model, provider, in.Think, autoPlan, in.Content, in)
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
	if s.Automation != nil {
		s.Automation.Shutdown(ctx)
	}
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

// FinalEvent is the payload pushed to subscribers when a run ends.
type FinalEvent struct {
	Message *store.Message `json:"message"`
}

func (s *Service) startRun(user *store.User, convOrig *store.Conversation, asst *store.Message, model *store.Model, provider *store.Provider, think bool, autoPlan *autoRunPlan, userText string, inputs ...SendInput) {
	convCopy := *convOrig
	conv := &convCopy
	run := &Run{MessageID: asst.ID, ConversationID: conv.ID, UserID: user.ID, UserName: user.DisplayName}
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
			asstID: asst.ID, userQuestion: userText, run: run, private: conv.Private, shared: conv.IsShared,
		}

		if len(inputs) > 0 {
			turn.clientCtx = inputs[0].ClientContext
			turn.quotedComponent = inputs[0].QuotedComponent
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
		if !conv.Private && !conv.IsShared && s.Memory != nil && s.Memory.Allowed(ctx, user, conv.AssistantID) {
			run.setActivity("正在翻看相关记忆…")
		}
		req, err := s.buildRequestWithOpts(ctx, user, conv, asst.ID, model, ts, buildOpts{autoEscalated: autoEscalated})
		tools := req.tools
		turn.foldedBeforeID = req.foldedBeforeID
		run.setActivity("")
		if err == nil && req.recalled && !conv.IsShared {
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
				req.Tools = nil
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
			if err != nil && !produced && req.ReasoningEffort != "" && isEffortBadRequest(err, req.ReasoningEffort) && ctx.Err() == nil {
				next := fallbackEffort(err, req.ReasoningEffort)
				s.Log.Warn("upstream rejected reasoning_effort, retrying", "message_id", asst.ID, "model", model.UpstreamModel, "from", req.ReasoningEffort, "to", next, "err", err)
				req.ReasoningEffort = next
				effortOverride, effortOverridden = next, true
				res, err = s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
			}
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
				firstToken = time.Since(started)
			}
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
