package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"nuan/internal/auth"
	"nuan/internal/chat"
	"nuan/internal/store"
)

// ---- auth & profile ----

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Device   string `json:"device"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	ipKey, userKey := "ip:"+clientIP(r), "user:"+strings.ToLower(in.Username)
	if s.Limiter.Blocked(ipKey, userKey) {
		writeErr(w, 429, "too_many_attempts", "尝试次数太多，请 15 分钟后再试")
		return
	}
	u, err := s.Store.GetUserByUsername(r.Context(), in.Username)
	if err != nil || u.Disabled || !auth.VerifyPassword(u.PasswordHash, in.Password) {
		s.Limiter.Fail(ipKey, userKey)
		writeErr(w, 401, "bad_credentials", "用户名或密码不正确")
		return
	}
	s.Limiter.Reset(userKey)
	tok, err := s.Sessions.Create(r.Context(), u.ID, truncate(in.Device, 100))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok, "user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_ = s.Sessions.Revoke(r.Context(), bearer(r))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, currentUser(r))
}

// bootstrap returns everything the client needs on launch in one round trip.
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ctx := r.Context()
	models, err := s.Store.ListModels(ctx, true)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	assistants, err := s.Store.ListAssistants(ctx, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	def, _ := s.Store.GetSetting(ctx, "default_model_id")
	autoDyn, _ := s.Store.GetSetting(ctx, chat.SettingAutoDynamic)
	voiceInput, voiceMode := s.voiceStatus(ctx)
	webSearch, _, webSearchKind := s.searchStatus(ctx)
	writeJSON(w, 200, map[string]any{
		"user":             u,
		"models":           publicModels(models),
		"assistants":       assistants,
		"default_model_id": def,
		"features": map[string]any{
			"voice_input":      voiceInput,
			"voice_mode":       voiceMode,
			"web_search":       webSearch,
			"search_provider":  webSearchKind,
			"image_generation": s.Chat.ImageAvailable(ctx),
			"images_left":      s.Chat.ImagesLeftToday(ctx, u),
			"auto_model":       s.Chat.AutoEnabled(ctx),
			"auto_dynamic":     autoDyn == "1",
		},
		"server_version": s.Cfg.AppVersion,
	})
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		DisplayName *string         `json:"display_name"`
		Avatar      *string         `json:"avatar"`
		Settings    json.RawMessage `json:"settings"`
		OldPassword string          `json:"old_password"`
		NewPassword string          `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	p := store.UserPatch{Avatar: in.Avatar}
	if in.DisplayName != nil {
		n := strings.TrimSpace(*in.DisplayName)
		if n == "" || utf8.RuneCountInString(n) > 20 {
			writeErr(w, 400, "bad_name", "称呼需要 1–20 个字")
			return
		}
		p.DisplayName = &n
	}
	if len(in.Settings) > 0 {
		var settings map[string]json.RawMessage
		if json.Unmarshal(in.Settings, &settings) != nil || settings == nil || len(in.Settings) > 16<<10 {
			writeErr(w, 400, "bad_settings", "设置格式不正确")
			return
		}
		p.Settings = in.Settings
	}
	if in.NewPassword != "" {
		if !auth.VerifyPassword(u.PasswordHash, in.OldPassword) {
			writeErr(w, 400, "bad_password", "原密码不正确")
			return
		}
		if len(in.NewPassword) < 6 {
			writeErr(w, 400, "weak_password", "新密码至少 6 位")
			return
		}
		h, err := auth.HashPassword(in.NewPassword)
		if err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
		p.PasswordHash = &h
	}
	nu, err := s.Store.UpdateUser(r.Context(), u.ID, p)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, nu)
}

// ---- models & assistants ----

type publicModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Vision      bool   `json:"vision"`
	Think       bool   `json:"think"` // supports deep thinking
}

func publicModels(ms []*store.Model) []publicModel {
	out := make([]publicModel, 0, len(ms))
	for _, m := range ms {
		out = append(out, publicModel{m.ID, m.DisplayName, m.Description, m.Vision, m.ThinkEffort != ""})
	}
	return out
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	ms, err := s.Store.ListModels(r.Context(), true)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, publicModels(ms))
}

func (s *Server) listAssistants(w http.ResponseWriter, r *http.Request) {
	as, err := s.Store.ListAssistants(r.Context(), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, as)
}

type assistantInput struct {
	UseMemory    *bool    `json:"use_memory"`
	Name         *string  `json:"name"`
	Avatar       *string  `json:"avatar"`
	Description  *string  `json:"description"`
	SystemPrompt *string  `json:"system_prompt"`
	Greeting     *string  `json:"greeting"`
	Suggestions  []string `json:"suggestions"`
	ModelID      *string  `json:"model_id"`
	Temperature  *float64 `json:"temperature"`
	Sort         *int     `json:"sort"`
	Shared       *bool    `json:"shared"` // admin only: visible to everyone
}

func (in *assistantInput) apply(a *store.Assistant) {
	if in.UseMemory != nil {
		a.UseMemory = *in.UseMemory
	}
	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Avatar != nil {
		a.Avatar = *in.Avatar
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if in.SystemPrompt != nil {
		a.SystemPrompt = *in.SystemPrompt
	}
	if in.Greeting != nil {
		a.Greeting = *in.Greeting
	}
	if in.Suggestions != nil {
		a.Suggestions = in.Suggestions
	}
	if in.ModelID != nil {
		if *in.ModelID == "" {
			a.ModelID = nil
		} else {
			a.ModelID = in.ModelID
		}
	}
	if in.Temperature != nil {
		a.Temperature = in.Temperature
	}
	if in.Sort != nil {
		a.Sort = *in.Sort
	}
}

func (s *Server) createAssistant(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in assistantInput
	if !decode(w, r, &in) {
		return
	}
	a := &store.Assistant{OwnerID: &u.ID, UseMemory: true}
	in.apply(a)
	if a.Name == "" {
		writeErr(w, 400, "bad_name", "请填写名称")
		return
	}
	if in.Shared != nil && *in.Shared && u.IsAdmin() {
		a.OwnerID = nil
	}
	if err := s.Store.UpsertAssistant(r.Context(), a); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) updateAssistant(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	a, err := s.Store.GetAssistant(r.Context(), chi.URLParam(r, "id"), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if a.OwnerID == nil && !u.IsAdmin() {
		writeErr(w, 403, "forbidden", "只有管理员可以修改共享助手")
		return
	}
	var in assistantInput
	if !decode(w, r, &in) {
		return
	}
	in.apply(a)
	if err := s.Store.UpsertAssistant(r.Context(), a); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) deleteAssistant(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	owner := u.ID
	if u.IsAdmin() {
		owner = ""
	}
	if err := s.Store.DeleteAssistant(r.Context(), chi.URLParam(r, "id"), owner); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- conversations ----

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	cs, err := s.Store.ListConversations(r.Context(), store.ConversationQuery{
		UserID: currentUser(r).ID, Before: before, Limit: limit, Search: q.Get("q"),
	})
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": cs})
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		AssistantID string `json:"assistant_id"`
		ModelID     string `json:"model_id"`
		Title       string `json:"title"`
		Private     bool   `json:"private"`
	}
	if !decode(w, r, &in) {
		return
	}
	c := &store.Conversation{UserID: u.ID, Title: truncate(strings.TrimSpace(in.Title), 60), Private: in.Private}
	if in.AssistantID != "" {
		if _, err := s.Store.GetAssistant(r.Context(), in.AssistantID, u.ID); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
		c.AssistantID = &in.AssistantID
	}
	switch {
	case in.ModelID == chat.AutoModelID:
		c.AutoMode = s.Chat.AutoEnabled(r.Context())
	case in.ModelID != "":
		if _, err := s.Store.GetModel(r.Context(), in.ModelID); err == nil {
			c.ModelID = &in.ModelID
		}
	}
	if err := s.Store.CreateConversation(r.Context(), c); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, c)
}

// getConversation returns the conversation plus its latest messages and any running generation.
func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	c, err := s.Store.GetConversation(r.Context(), chi.URLParam(r, "id"), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	const page = 60
	msgs, err := s.Store.ListMessages(r.Context(), c.ID, "", page+1)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	hasMore := len(msgs) > page
	if hasMore {
		msgs = msgs[1:]
	}
	active := ""
	if run := s.Chat.Hub.ActiveInConversation(c.ID); run != nil {
		active = run.MessageID
	}
	writeJSON(w, 200, map[string]any{"conversation": c, "messages": msgs, "has_more": hasMore, "active_message_id": active})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	c, err := s.Store.GetConversation(r.Context(), chi.URLParam(r, "id"), u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	msgs, err := s.Store.ListMessages(r.Context(), c.ID, r.URL.Query().Get("before"), limit+1)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[1:]
	}
	writeJSON(w, 200, map[string]any{"items": msgs, "has_more": hasMore})
}

func (s *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       *string `json:"title"`
		Pinned      *bool   `json:"pinned"`
		ModelID     *string `json:"model_id"`
		AssistantID *string `json:"assistant_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Title != nil {
		t := truncate(strings.TrimSpace(*in.Title), 60)
		in.Title = &t
	}
	patch := store.ConversationPatch{
		Title: in.Title, Pinned: in.Pinned, AssistantID: in.AssistantID,
	}
	if in.ModelID != nil {
		switch *in.ModelID {
		case chat.AutoModelID:
			on := s.Chat.AutoEnabled(r.Context())
			patch.AutoMode = &on
		case "":
			off := false
			patch.AutoMode = &off
			patch.ModelID = in.ModelID
		default:
			if _, err := s.Store.GetModel(r.Context(), *in.ModelID); err != nil {
				writeErr(w, 400, "bad_model", "模型不存在")
				return
			}
			off := false
			patch.AutoMode = &off
			patch.ModelID = in.ModelID
		}
	}
	c, err := s.Store.UpdateConversation(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID, patch)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := currentUser(r)
	// Detached timeout so closing the app mid-delete still finishes wiping a private chat.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Chat.DeleteConversation(ctx, id, u.ID); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- chat ----

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConversationID string             `json:"conversation_id"`
		AssistantID    string             `json:"assistant_id"`
		ModelID        string             `json:"model_id"`
		Content        string             `json:"content"`
		Attachments    []store.Attachment `json:"attachments"`
		EditOf         string             `json:"edit_of"`
		Think          bool               `json:"think"`
		Private        bool               `json:"private"`
	}
	if !decode(w, r, &in) {
		return
	}
	if utf8.RuneCountInString(in.Content) > 100_000 {
		writeErr(w, 400, "too_long", "消息太长了")
		return
	}
	if len(in.Attachments) > 9 {
		writeErr(w, 400, "too_many_files", "一次最多发送 9 张图片")
		return
	}
	res, err := s.Chat.Send(r.Context(), currentUser(r), chat.SendInput{
		ConversationID: in.ConversationID, AssistantID: in.AssistantID, ModelID: in.ModelID,
		Content: in.Content, Attachments: in.Attachments, EditOf: in.EditOf, Think: in.Think, Private: in.Private,
	})
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) regenerate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ModelID string `json:"model_id"`
		Think   bool   `json:"think"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	res, err := s.Chat.Regenerate(r.Context(), currentUser(r), chi.URLParam(r, "id"), in.ModelID, in.Think)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	ok := s.Chat.Cancel(currentUser(r).ID, chi.URLParam(r, "id"))
	writeJSON(w, 200, map[string]any{"ok": ok})
}

// stream is the single SSE endpoint for a generation. Every (re)connection
// first receives a `snapshot` with the full text so far, then `delta`s,
// then a final `done` with the persisted message.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id := chi.URLParam(r, "id")
	msg, err := s.Store.GetMessageForUser(r.Context(), id, u.ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "internal", "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)

	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	run := s.Chat.Hub.Get(id)
	if run == nil || run.UserID != u.ID {
		// Already finished (or interrupted): replay the stored result.
		send("snapshot", map[string]any{
			"content": msg.Content, "reasoning": msg.Reasoning, "status": msg.Status,
			"attachments": nonNil(msg.Attachments), "sources": nonNilSources(msg.Sources),
			"components": nonNilComponents(msg.Components),
		})
		send("done", chat.FinalEvent{Message: msg})
		return
	}

	wake, unsub := run.Subscribe()
	defer unsub()
	snap := run.Snapshot()
	curModel := ""
	if msg.ModelID != nil {
		curModel = *msg.ModelID
	}
	snapPayload := map[string]any{
		"content": snap.Content, "reasoning": snap.Reasoning, "status": store.StatusStreaming, "title": snap.Title,
		"activity": snap.Activity, "activity_at": snap.ActivityAt,
		"steps": nonNilSteps(snap.Steps), "attachments": nonNil(snap.Attachments),
		"sources": nonNilSources(snap.Sources), "components": nonNilComponents(snap.Components),
	}
	if snap.ModelID != "" {
		snapPayload["model_id"] = snap.ModelID
		curModel = snap.ModelID
	}
	if !send("snapshot", snapPayload) {
		return
	}
	offC, offR, title := len(snap.Content), len(snap.Reasoning), snap.Title
	activity, stepsSeq, compsSeq, nAtt, nSrc := snap.Activity, snap.StepsSeq, snap.ComponentsSeq, len(snap.Attachments), len(snap.Sources)
	if snap.Final != nil {
		send("done", snap.Final)
		return
	}

	const minInterval = 35 * time.Millisecond // coalesce tokens into ~30 fps updates
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	last := time.Time{}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-wake:
			if d := time.Since(last); d < minInterval {
				time.Sleep(minInterval - d)
			}
			last = time.Now()
			snap := run.Snapshot()
			if snap.ModelID != "" && snap.ModelID != curModel {
				curModel = snap.ModelID
				send("model", map[string]string{"conversation_id": run.ConversationID, "model_id": snap.ModelID})
			}
			if snap.StepsSeq != stepsSeq {
				stepsSeq = snap.StepsSeq
				if !send("steps", map[string]any{"steps": nonNilSteps(snap.Steps)}) {
					return
				}
			}
			if len(snap.Sources) != nSrc {
				nSrc = len(snap.Sources)
				if !send("sources", map[string]any{"sources": nonNilSources(snap.Sources)}) {
					return
				}
			}
			delta := map[string]string{}
			if len(snap.Reasoning) > offR {
				delta["reasoning"] = snap.Reasoning[offR:]
				offR = len(snap.Reasoning)
			}
			if len(snap.Content) > offC {
				delta["content"] = snap.Content[offC:]
				offC = len(snap.Content)
			}
			if len(delta) > 0 && !send("delta", delta) {
				return
			}
			if snap.ComponentsSeq != compsSeq {
				compsSeq = snap.ComponentsSeq
				if !send("components", map[string]any{"components": nonNilComponents(snap.Components)}) {
					return
				}
			}
			if len(snap.Attachments) != nAtt {
				nAtt = len(snap.Attachments)
				send("attachments", map[string]any{"attachments": snap.Attachments})
			}
			if snap.Activity != activity {
				activity = snap.Activity
				send("activity", map[string]any{"text": activity, "at": snap.ActivityAt})
			}
			if snap.Title != "" && snap.Title != title {
				title = snap.Title
				send("title", map[string]string{"conversation_id": run.ConversationID, "title": title})
			}
			if snap.Final != nil {
				send("done", snap.Final)
				return
			}
		}
	}
}

func nonNil(a []store.Attachment) []store.Attachment {
	if a == nil {
		return []store.Attachment{}
	}
	return a
}

func nonNilSteps(s []chat.RunStep) []chat.RunStep {
	if s == nil {
		return []chat.RunStep{}
	}
	return s
}

func nonNilSources(s []store.WebSource) []store.WebSource {
	if s == nil {
		return []store.WebSource{}
	}
	return s
}

func nonNilComponents(c []store.MessageComponent) []store.MessageComponent {
	if c == nil {
		return []store.MessageComponent{}
	}
	return c
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
