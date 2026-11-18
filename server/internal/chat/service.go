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
	"nuan/internal/store"
)

// Errors returned to the API layer.
var (
	ErrBusy          = errors.New("too many concurrent generations")
	ErrConvBusy      = errors.New("conversation is already generating")
	ErrNoModel       = errors.New("no model available")
	ErrEmptyMessage  = errors.New("empty message")
	ErrNothingToRedo = errors.New("nothing to regenerate")
	errShutdown      = errors.New("server shutting down")
	errUserCancel    = errors.New("cancelled by user")
)

const (
	maxRunsPerUser   = 3
	maxImageMessages = 4 // only the most recent N user messages keep their images
)

type MemoryService interface {
	Inject(context.Context, *store.User, *string, string) memory.Context
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

	clientsMu sync.Mutex
	clients   map[string]*llm.Client // by provider id + updated_at

	noTools sync.Map // model id@updated_at -> upstream rejected tools

	drawMu      sync.Mutex
	drawPending map[string]int // user id -> pictures being drawn (daily limit)
}

func NewService(st *store.Store, dataDir string, log *slog.Logger) *Service {
	return &Service{Store: st, Hub: NewHub(), Log: log, DataDir: dataDir, clients: map[string]*llm.Client{}}
}

// SendInput is a new user message.
type SendInput struct {
	ConversationID string // empty = create a new conversation
	AssistantID    string // used when creating a conversation
	ModelID        string // optional override; persisted on the conversation
	Content        string
	Attachments    []store.Attachment
	EditOf         string // replace this user message and everything after it
	Think          bool   // deep thinking mode
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
	var err error
	if in.ConversationID == "" {
		conv = &store.Conversation{UserID: user.ID}
		if in.AssistantID != "" {
			a, err := s.Store.GetAssistant(ctx, in.AssistantID, user.ID)
			if err != nil {
				return nil, fmt.Errorf("assistant: %w", err)
			}
			conv.AssistantID = &a.ID
		}
		if in.ModelID != "" {
			conv.ModelID = &in.ModelID
		}
		if err := s.Store.CreateConversation(ctx, conv); err != nil {
			return nil, err
		}
	} else {
		conv, err = s.Store.GetConversation(ctx, in.ConversationID, user.ID)
		if err != nil {
			return nil, err
		}
		if s.Hub.ActiveInConversation(conv.ID) != nil {
			return nil, ErrConvBusy
		}
		if in.ModelID != "" && (conv.ModelID == nil || *conv.ModelID != in.ModelID) {
			if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{ModelID: &in.ModelID}); err != nil {
				return nil, err
			}
		}
	}

	if in.EditOf != "" {
		old, err := s.Store.GetMessageForUser(ctx, in.EditOf, user.ID)
		if err != nil || old.ConversationID != conv.ID || old.Role != "user" {
			return nil, store.ErrNotFound
		}
	}

	model, provider, err := s.resolveModel(ctx, conv, user.ID, len(in.Attachments) > 0)
	if err != nil {
		return nil, err
	}

	userMsg := &store.Message{Role: "user", Content: in.Content, Attachments: in.Attachments, Status: store.StatusDone}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: &model.ID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, TruncateFrom: in.EditOf, Append: []*store.Message{userMsg, asst}}); err != nil {
		return nil, err
	}
	s.startRun(user, conv, asst, model, provider, in.Think)
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
	last, err := s.Store.ListMessages(ctx, conv.ID, "", 2)
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
	if modelID != "" {
		if conv, err = s.Store.UpdateConversation(ctx, conv.ID, user.ID, store.ConversationPatch{ModelID: &modelID}); err != nil {
			return nil, err
		}
	}
	needVision := false
	for _, m := range last {
		if m.Role == "user" && len(m.Attachments) > 0 {
			needVision = true
		}
	}
	model, provider, err := s.resolveModel(ctx, conv, user.ID, needVision)
	if err != nil {
		return nil, err
	}
	asst := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: &model.ID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{ConversationID: conv.ID, TruncateFrom: truncate, Append: []*store.Message{asst}}); err != nil {
		return nil, err
	}
	s.startRun(user, conv, asst, model, provider, think)
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

// FinalEvent is the payload pushed to subscribers when a run ends.
type FinalEvent struct {
	Message *store.Message `json:"message"`
}

func (s *Service) startRun(user *store.User, conv *store.Conversation, asst *store.Message, model *store.Model, provider *store.Provider, think bool) {
	run := &Run{MessageID: asst.ID, ConversationID: conv.ID, UserID: user.ID}
	needTitle := conv.Title == ""
	s.Hub.start(run, func(ctx context.Context) {
		started := time.Now()
		msg := *asst
		var firstToken time.Duration
		var usage llm.Usage
		turn := &toolTurn{user: user, convID: conv.ID, asstID: asst.ID, run: run}

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

		tools := s.toolsFor(ctx, model)
		req, err := s.buildRequest(ctx, user, conv, asst.ID, model, tools != nil)
		for round := 0; err == nil && round < maxToolRounds; round++ {
			req.ReasoningEffort = effortFor(model, think)
			req.Tools = tools
			if round == maxToolRounds-1 {
				req.Tools = nil // last round: answer in words
			}
			roundStart := run.contentLen()
			produced = false
			var res *llm.Result
			res, err = s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
			// Upstream rejects tools: answer without them rather than fail.
			if err != nil && !produced && len(req.Tools) > 0 && isBadRequest(err) && ctx.Err() == nil {
				s.Log.Warn("upstream rejected tools, retrying without", "message_id", asst.ID, "model", model.UpstreamModel, "err", err)
				s.markToolsRejected(model)
				tools, req.Tools = nil, nil
				req.Messages[0].Content = s.systemPrompt(ctx, user, conv, false, req.core)
				res, err = s.streamWithRetry(ctx, s.client(provider), req.Request, onEvent)
			}
			// Nothing reached the user yet: let the fallback model answer instead of showing an error.
			if err != nil && round == 0 && firstToken == 0 && ctx.Err() == nil {
				if fm, fp := s.fallbackModel(ctx, model.ID); fm != nil {
					s.Log.Warn("primary model failed, using fallback", "message_id", asst.ID, "model", model.UpstreamModel, "fallback", fm.UpstreamModel, "err", err)
					ftools := s.toolsFor(ctx, fm)
					freq, ferr := s.buildRequest(ctx, user, conv, asst.ID, fm, ftools != nil)
					if ferr == nil {
						model, provider, tools, req = fm, fp, ftools, freq
						msg.ModelID = &fm.ID
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
			content, _ := run.text()
			req.Messages = append(req.Messages, llm.Message{Role: "assistant", Content: strings.TrimSpace(content[roundStart:]), ToolCalls: calls})
			for i, result := range s.runTools(ctx, turn, calls) {
				req.Messages = append(req.Messages, llm.Message{Role: "tool", ToolCallID: calls[i].ID, Content: result})
			}
			needBreak = true
			if ctx.Err() != nil {
				err = context.Cause(ctx)
				break
			}
		}

		msg.Content, msg.Reasoning = run.text()
		msg.Attachments = run.Snapshot().Attachments
		// The pictures are what the user asked for: a failure while the model
		// was only wrapping up must not turn the reply into an error.
		if err != nil && len(msg.Attachments) > 0 && ctx.Err() == nil {
			s.Log.Warn("reply failed after drawing, keeping the pictures", "message_id", asst.ID, "err", err)
			err = nil
			if strings.TrimSpace(msg.Content) == "" {
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
		} else if msg.Status == store.StatusDone && s.MemoryWorker != nil {
			if history, err := s.Store.ListMessages(pctx, conv.ID, msg.ID, 1); err == nil && len(history) > 0 && memory.Trigger(history[0].Content) {
				s.MemoryWorker.Kick(conv.ID, 5*time.Second)
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
	def, err := s.Store.GetSetting(ctx, "default_model_id")
	if err != nil {
		return err
	}
	if _, _, ok := s.usableModel(ctx, def); !ok {
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

func (s *Service) systemPrompt(ctx context.Context, user *store.User, conv *store.Conversation, canDraw bool, core ...string) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString(s.drawPrompt(ctx, canDraw))
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && strings.TrimSpace(a.SystemPrompt) != "" {
			fmt.Fprintf(&b, "\n\n# 本次对话的角色设定（%s）\n%s", a.Name, a.SystemPrompt)
		}
	}
	now := time.Now().In(shanghai)
	fmt.Fprintf(&b, "\n\n当前日期：%s（%s）。", now.Format("2006年1月2日"), weekdays[now.Weekday()])
	if user.DisplayName != "" {
		fmt.Fprintf(&b, "\n正在和你对话的用户称呼是「%s」。", user.DisplayName)
	}
	if len(core) > 0 {
		b.WriteString(core[0])
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
	core string
}

func (s *Service) buildRequest(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, canDraw bool) (chatRequest, error) {
	history, err := s.Store.ListMessages(ctx, conv.ID, asstID, 500)
	if err != nil {
		return chatRequest{}, err
	}
	lastUser := -1
	query := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			if lastUser < 0 {
				lastUser = i
				query = history[i].Content
				if utf8.RuneCountInString(query) >= 8 {
					break
				}
			} else {
				query = history[i].Content + "\n" + query
				break
			}
		}
	}
	mem := memory.Context{}
	if s.Memory != nil {
		mem = s.Memory.Inject(ctx, user, conv.AssistantID, query)
	}
	sys := s.systemPrompt(ctx, user, conv, canDraw, mem.Core)
	note := "（系统附注，不是用户说的话，回复时不要写出这种附注：现在是 " + time.Now().In(shanghai).Format("15:04") + "。"
	if mem.Recall != "" {
		note += "可能相关的往事：" + mem.Recall
	}
	note += "）\n"

	budget := model.ContextWindow
	if budget <= 0 {
		budget = 128000
	}
	reserve := model.MaxOutput
	if reserve <= 0 {
		reserve = 8192
	}
	budget -= reserve + estimateTokens(sys)

	// Pictures are numbered (图N) so the model can name the one to modify.
	var num map[string]int
	if canDraw {
		num = map[string]int{}
		for i, a := range collectRefs(history) {
			num[a.FileID] = i + 1
		}
	}

	// Walk backwards, keeping as much recent history as fits.
	var picked []llm.Message
	imagesLeft := maxImageMessages
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		drew := m.Role == "assistant" && len(m.Attachments) > 0
		if m.Role == "assistant" && !drew && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "") {
			continue
		}
		lm := llm.Message{Role: m.Role, Content: m.Content}
		if drew {
			lm.Content = drawnNote(m.Attachments, num) + m.Content
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
				text := m.Content
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
				lm.Content = fmt.Sprintf("[用户发送了 %s，当前已不可见]\n%s", n, m.Content)
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
		if budget-cost < 0 && len(picked) > 0 {
			break
		}
		budget -= cost
		picked = append(picked, lm)
	}
	// Drop leading assistant messages so history starts with the user.
	for len(picked) > 0 && picked[len(picked)-1].Role == "assistant" {
		picked = picked[:len(picked)-1]
	}
	msgs := make([]llm.Message, 0, len(picked)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: sys})
	for i := len(picked) - 1; i >= 0; i-- {
		msgs = append(msgs, picked[i])
	}
	if len(msgs) == 1 {
		return chatRequest{}, ErrEmptyMessage
	}
	req := chatRequest{Request: llm.Request{Model: model.UpstreamModel, Messages: msgs, MaxTokens: model.MaxOutput}, core: mem.Core}
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
