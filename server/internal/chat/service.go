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

type Service struct {
	Store   *store.Store
	Hub     *Hub
	Log     *slog.Logger
	DataDir string

	clientsMu sync.Mutex
	clients   map[string]*llm.Client // by provider id + updated_at
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

	model, provider, err := s.resolveModel(ctx, conv, user.ID)
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
	model, provider, err := s.resolveModel(ctx, conv, user.ID)
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

func (s *Service) Shutdown(ctx context.Context) { s.Hub.Shutdown(ctx, errShutdown) }

// resolveModel picks the model: conversation override → assistant → global default → first enabled.
func (s *Service) resolveModel(ctx context.Context, conv *store.Conversation, userID string) (*store.Model, *store.Provider, error) {
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

func (s *Service) usableModel(ctx context.Context, id string) (*store.Model, *store.Provider, bool) {
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

func (s *Service) client(p *store.Provider) *llm.Client {
	key := fmt.Sprintf("%s@%d", p.ID, p.UpdatedAt)
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c, ok := s.clients[key]
	if !ok {
		c = llm.NewClient(p.BaseURL, p.APIKey)
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

		if needTitle {
			go s.generateTitle(conv.ID, user.ID, model, provider, run)
		}

		req, err := s.buildRequest(ctx, user, conv, asst.ID, model)
		if err == nil {
			req.ReasoningEffort = model.DefaultEffort
			if think && model.ThinkEffort != "" {
				req.ReasoningEffort = model.ThinkEffort
			}
			var res *llm.Result
			res, err = s.streamWithRetry(ctx, s.client(provider), req, func(ev llm.Event) {
				if firstToken == 0 {
					firstToken = time.Since(started)
				}
				if ev.Reasoning != "" {
					run.appendReasoning(ev.Reasoning)
				}
				if ev.Content != "" {
					run.appendContent(ev.Content)
				}
			})
			if res != nil && res.Usage != nil {
				msg.PromptTokens = res.Usage.PromptTokens
				msg.CompletionTokens = res.Usage.CompletionTokens
			}
		}

		msg.Content, msg.Reasoning = run.text()
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
		}
		_ = s.Store.TouchConversation(pctx, conv.ID)
		s.Log.Info("generation finished", "message_id", msg.ID, "model", model.UpstreamModel, "status", msg.Status,
			"first_token_ms", msg.FirstTokenMs, "duration_ms", msg.DurationMs, "tokens", msg.CompletionTokens)
		run.finish(&FinalEvent{Message: &msg})
	})
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

func (s *Service) systemPrompt(ctx context.Context, user *store.User, conv *store.Conversation) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	now := time.Now().In(shanghai)
	fmt.Fprintf(&b, "\n\n当前时间：%s（%s）。", now.Format("2006年1月2日 15:04"), weekdays[now.Weekday()])
	if user.DisplayName != "" {
		fmt.Fprintf(&b, "\n正在和你对话的用户称呼是「%s」。", user.DisplayName)
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && strings.TrimSpace(a.SystemPrompt) != "" {
			fmt.Fprintf(&b, "\n\n# 本次对话的角色设定（%s）\n%s", a.Name, a.SystemPrompt)
		}
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

func (s *Service) buildRequest(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model) (llm.Request, error) {
	history, err := s.Store.ListMessages(ctx, conv.ID, asstID, 500)
	if err != nil {
		return llm.Request{}, err
	}
	sys := s.systemPrompt(ctx, user, conv)

	budget := model.ContextWindow
	if budget <= 0 {
		budget = 128000
	}
	reserve := model.MaxOutput
	if reserve <= 0 {
		reserve = 8192
	}
	budget -= reserve + estimateTokens(sys)

	// Walk backwards, keeping as much recent history as fits.
	var picked []llm.Message
	imagesLeft := maxImageMessages
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role == "assistant" && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "") {
			continue
		}
		lm := llm.Message{Role: m.Role, Content: m.Content}
		cost := estimateTokens(m.Content)
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
				lm.Parts = append(parts, llm.Part{Type: "text", Text: text})
			} else {
				lm.Content = fmt.Sprintf("[用户发送了 %d 张图片，当前已不可见]\n%s", len(m.Attachments), m.Content)
			}
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
		return llm.Request{}, ErrEmptyMessage
	}
	req := llm.Request{Model: model.UpstreamModel, Messages: msgs, MaxTokens: model.MaxOutput}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && a.Temperature != nil {
			req.Temperature = a.Temperature
		}
	}
	return req, nil
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
