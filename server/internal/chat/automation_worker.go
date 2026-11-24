package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const (
	automationPollInterval = 20 * time.Second
	automationCatchupGrace = 2 * time.Hour
	automationRunTimeout   = 4 * time.Minute
	automationMaxWorkers   = 2
	automationMaxRounds    = 7
)

// ErrTaskAlreadyRunning is returned when a manual run is requested for a task currently executing.
var ErrTaskAlreadyRunning = errors.New("task is already running")

// AutomationWorker polls due ScheduledTasks and runs them autonomously in the background.
type AutomationWorker struct {
	svc    *Service
	slots  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running map[string]bool
	started bool
	closed  bool
}

// NewAutomationWorker constructs a background worker bound to svc.
func NewAutomationWorker(svc *Service) *AutomationWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &AutomationWorker{
		svc:     svc,
		slots:   make(chan struct{}, automationMaxWorkers),
		ctx:     ctx,
		cancel:  cancel,
		running: map[string]bool{},
	}
}

// Start launches the polling loop once. Safe to call multiple times.
func (w *AutomationWorker) Start() {
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		// Clean up runs interrupted by a prior process crash.
		pctx, pcancel := context.WithTimeout(w.ctx, 5*time.Second)
		if n, err := w.svc.Store.MarkInterruptedTaskRuns(pctx); err == nil && n > 0 {
			w.svc.Log.Info("marked interrupted scheduled task runs", "count", n)
		}
		pcancel()

		// Let startup settle briefly before first scan.
		select {
		case <-time.After(5 * time.Second):
		case <-w.ctx.Done():
			return
		}
		w.pollOnce()

		ticker := time.NewTicker(automationPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-w.ctx.Done():
				return
			case <-ticker.C:
				w.pollOnce()
			}
		}
	}()
}

// Shutdown stops polling and waits for in-flight automation runs until ctx expires.
func (w *AutomationWorker) Shutdown(ctx context.Context) {
	w.mu.Lock()
	w.closed = true
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// TriggerNow launches a manual background execution for taskID owned by userID.
func (w *AutomationWorker) TriggerNow(userID, taskID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	task, err := w.svc.Store.GetScheduledTask(ctx, taskID, userID)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return errShutdown
	}
	if w.running[task.ID] {
		w.mu.Unlock()
		return ErrTaskAlreadyRunning
	}
	w.running[task.ID] = true
	w.mu.Unlock()

	_ = w.svc.Store.MarkScheduledTaskRunning(ctx, task.ID, task.UserID)
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.clearRunning(task.ID)
		w.runWithSlot(task, "manual")
	}()
	return nil
}

func (w *AutomationWorker) clearRunning(taskID string) {
	w.mu.Lock()
	delete(w.running, taskID)
	w.mu.Unlock()
}

func (w *AutomationWorker) pollOnce() {
	nowMs := store.Now()
	pctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	due, err := w.svc.Store.ListDueScheduledTasks(pctx, nowMs, 10)
	cancel()
	if err != nil {
		if w.ctx.Err() == nil {
			w.svc.Log.Warn("poll scheduled tasks failed", "err", err)
		}
		return
	}
	graceMs := int64(automationCatchupGrace / time.Millisecond)
	for _, item := range due {
		if w.ctx.Err() != nil {
			return
		}
		w.mu.Lock()
		if w.running[item.ID] || w.closed {
			w.mu.Unlock()
			continue
		}
		w.running[item.ID] = true
		w.mu.Unlock()

		cctx, ccancel := context.WithTimeout(w.ctx, 5*time.Second)
		claimed, shouldRun, err := w.svc.Store.AdvanceScheduledTaskNextRun(cctx, item.ID, nowMs, graceMs)
		ccancel()
		if err != nil || claimed == nil || !shouldRun {
			w.clearRunning(item.ID)
			continue
		}
		w.wg.Add(1)
		go func(tk *store.ScheduledTask) {
			defer w.wg.Done()
			defer w.clearRunning(tk.ID)
			w.runWithSlot(tk, "schedule")
		}(claimed)
	}
}

func (w *AutomationWorker) runWithSlot(task *store.ScheduledTask, trigger string) {
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-w.ctx.Done():
		return
	}
	runCtx, cancel := context.WithTimeout(w.ctx, automationRunTimeout)
	defer cancel()
	w.svc.ExecuteScheduledTask(runCtx, task, trigger)
}

// ExecuteScheduledTask runs one scheduled AI task end-to-end, persisting the archive
// conversation turn, tool side-effects (e.g. component updates), and ScheduledTaskRun record.
func (s *Service) ExecuteScheduledTask(ctx context.Context, task *store.ScheduledTask, trigger string) *store.ScheduledTaskRun {
	runRec := &store.ScheduledTaskRun{
		TaskID:        task.ID,
		UserID:        task.UserID,
		TriggerSource: trigger,
		Status:        store.TaskRunStatusRunning,
		StartedAt:     store.Now(),
	}
	_ = s.Store.CreateScheduledTaskRun(ctx, runRec)

	finishErr := func(err error) *store.ScheduledTaskRun {
		runRec.Status = store.TaskRunStatusError
		runRec.Error = clipRunes(friendlyError(err), 300)
		runRec.FinishedAt = store.Now()
		pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.Store.FinishScheduledTaskRun(pctx, runRec)
		cancel()
		s.Log.Warn("scheduled task failed", "task_id", task.ID, "title", task.Title, "err", err)
		return runRec
	}

	user, err := s.Store.GetUser(ctx, task.UserID)
	if err != nil || user.Disabled {
		if err == nil {
			err = errors.New("用户已禁用")
		}
		return finishErr(err)
	}

	conv, err := s.ensureTaskConversation(ctx, user, task)
	if err != nil {
		return finishErr(err)
	}
	runRec.ConversationID = conv.ID

	model, provider, err := s.resolveTaskModel(ctx, task, conv, user.ID)
	if err != nil {
		return finishErr(err)
	}

	nowStr := time.Now().In(shanghai).Format("2006-01-02 15:04")
	triggerLabel := "定时触发"
	if trigger == "manual" {
		triggerLabel = "手动立即执行"
	}
	userContent := fmt.Sprintf("【%s · %s】\n%s", triggerLabel, nowStr, task.Prompt)
	userMsg := &store.Message{Role: "user", Content: userContent, Status: store.StatusDone}
	asstModelID := model.ID
	asstMsg := &store.Message{Role: "assistant", Status: store.StatusStreaming, ModelID: &asstModelID}
	if err := s.Store.ApplyTurn(ctx, store.Turn{
		ConversationID: conv.ID,
		Append:         []*store.Message{userMsg, asstMsg},
	}); err != nil {
		return finishErr(err)
	}
	runRec.MessageID = asstMsg.ID

	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	run := &Run{
		MessageID:      asstMsg.ID,
		ConversationID: conv.ID,
		UserID:         user.ID,
		modelID:        model.ID,
		done:           make(chan struct{}),
		subs:           map[chan struct{}]struct{}{},
	}
	unregisterRun := s.Hub.registerDetached(run, cancelRun)
	finishedRun := false
	defer func() {
		if !finishedRun {
			run.finish(asstMsg)
		}
		unregisterRun()
	}()
	run.setActivity("正在执行定时任务…")
	turn := &toolTurn{
		user:             user,
		convID:           conv.ID,
		assistantID:      conv.AssistantID,
		asstID:           asstMsg.ID,
		userQuestion:     task.Prompt,
		run:              run,
		private:          false,
		activeSkills:     map[string]bool{},
		automation:       true,
		automationTaskID: task.ID,
	}

	ts := s.toolsFor(ctx, user, conv, model)
	tools := []llm.Tool{}
	for _, tool := range ts.tools {
		if !isSettingsTool(tool.Name) {
			tools = append(tools, tool)
		}
	}
	if s.Skills != nil {
		sctx := s.skillContext(user, conv)
		for _, key := range []string{skillKeyComponents, "schedule", skillKeyAutomation} {
			if def, ok := s.Skills.Get(key, sctx); ok {
				turn.activeSkills[def.Key] = true
				tools = appendUniqueTools(tools, def.Tools...)
			}
		}
	}

	targetBlock := s.buildTaskTargetContext(ctx, user.ID, task)
	sysPrompt := s.buildAutomationSystemPrompt(ctx, user, conv, ts, task, targetBlock)
	messages := []llm.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userContent},
	}

	started := time.Now()
	var usage llm.Usage
	var genErr error
	toolCallCount := 0
	for round := 0; round < automationMaxRounds; round++ {
		req := llm.Request{
			Model:           model.UpstreamModel,
			Messages:        messages,
			MaxTokens:       model.MaxOutput,
			ReasoningEffort: model.DefaultEffort,
			Tools:           tools,
		}
		if round == automationMaxRounds-1 {
			req.Tools = nil
		}
		roundStart := run.contentLen()
		roundReasoningStart := run.reasoningLen()
		res, err := s.streamWithRetry(runCtx, s.client(provider), req, func(ev llm.Event) {
			if ev.Reasoning != "" {
				run.setActivity("")
				run.appendReasoning(ev.Reasoning)
			}
			if ev.Content != "" {
				run.setActivity("")
				run.appendContent(ev.Content)
			}
		})
		if res != nil && res.Usage != nil {
			usage.PromptTokens += res.Usage.PromptTokens
			usage.CompletionTokens += res.Usage.CompletionTokens
			cost := res.Usage.Cost
			if cost <= 0 {
				cost = model.EstimateCost(res.Usage.PromptTokens, res.Usage.CompletionTokens)
			}
			usage.Cost += cost
		}
		if err != nil {
			genErr = err
			break
		}
		if res == nil || len(res.ToolCalls) == 0 || len(req.Tools) == 0 {
			break
		}
		calls := res.ToolCalls
		toolCallCount += len(calls)
		for i := range calls {
			if calls[i].ID == "" {
				calls[i].ID = fmt.Sprintf("auto_call_%d_%d", round, i)
			}
		}
		content, reasoning := run.text()
		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   strings.TrimSpace(content[roundStart:]),
			Reasoning: strings.TrimSpace(reasoning[roundReasoningStart:]),
			ToolCalls: calls,
		})
		results, stopTools := s.runTools(runCtx, turn, calls)
		for i, result := range results {
			messages = append(messages, llm.Message{Role: "tool", ToolCallID: calls[i].ID, Content: result})
		}
		if stopTools {
			tools = nil
		}
		if run.contentLen() > roundStart {
			run.appendContent("\n\n")
		}
	}

	snap := run.Snapshot()
	asstMsg.Content = strings.TrimSpace(snap.Content)
	asstMsg.Reasoning = strings.TrimSpace(snap.Reasoning)
	asstMsg.Attachments = snap.Attachments
	asstMsg.Sources = snap.Sources
	asstMsg.Components = snap.Components
	asstMsg.PromptTokens = usage.PromptTokens
	asstMsg.CompletionTokens = usage.CompletionTokens
	asstMsg.Cost = usage.Cost + turn.cost
	asstMsg.DurationMs = time.Since(started).Milliseconds()

	if genErr != nil && (turn.updatedComponents > 0 || len(asstMsg.Components) > 0) {
		genErr = nil
	}
	if genErr != nil {
		asstMsg.Status = store.StatusError
		asstMsg.Error = friendlyError(genErr)
	} else {
		asstMsg.Status = store.StatusDone
		if asstMsg.Content == "" && turn.updatedComponents > 0 {
			asstMsg.Content = "定时任务已执行完成，目标卡片内容已更新。"
		}
	}

	pctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.Store.FinishMessage(pctx, asstMsg)
	_ = s.Store.TouchConversation(pctx, conv.ID)
	run.finish(asstMsg)
	finishedRun = true

	// Auto-bind target component if the task wasn't bound yet and this run created/pinned one.
	if task.TargetEntityID == "" && task.TargetMessageID == "" {
		bindMsg, bindComp := turn.lastPinnedMsgID, turn.lastPinnedCompID
		if bindMsg == "" || bindComp == "" {
			bindMsg, bindComp = turn.lastCreatedMsgID, turn.lastCreatedCompID
		}
		if bindMsg != "" && bindComp != "" {
			_ = s.Store.BindScheduledTaskTarget(pctx, task.ID, task.UserID, bindMsg, bindComp, false)
		}
	}

	runRec.ToolCalls = toolCallCount
	runRec.UpdatedComponents = turn.updatedComponents
	runRec.FinishedAt = store.Now()
	if genErr != nil {
		runRec.Status = store.TaskRunStatusError
		runRec.Error = clipRunes(friendlyError(genErr), 300)
	} else {
		runRec.Status = store.TaskRunStatusSuccess
		runRec.Summary = clipRunes(strings.Join(strings.Fields(asstMsg.Content), " "), 180)
		if runRec.Summary == "" && turn.updatedComponents > 0 {
			runRec.Summary = fmt.Sprintf("已更新 %d 张卡片", turn.updatedComponents)
		}
	}
	_ = s.Store.FinishScheduledTaskRun(pctx, runRec)
	s.Log.Info("scheduled task finished", "task_id", task.ID, "title", task.Title, "status", runRec.Status,
		"tool_calls", toolCallCount, "updated_components", turn.updatedComponents, "ms", asstMsg.DurationMs)
	return runRec
}

func (s *Service) ensureTaskConversation(ctx context.Context, user *store.User, task *store.ScheduledTask) (*store.Conversation, error) {
	if task.ConversationID != nil && *task.ConversationID != "" {
		if conv, err := s.Store.GetConversation(ctx, *task.ConversationID, user.ID); err == nil {
			return conv, nil
		}
	}
	conv := &store.Conversation{
		UserID: user.ID,
		Title:  clipRunes("定时 · "+task.Title, 48),
	}
	if err := s.Store.CreateConversation(ctx, conv); err != nil {
		return nil, err
	}
	cid := conv.ID
	task.ConversationID = &cid
	_ = s.Store.BindScheduledTaskConversation(ctx, task.ID, user.ID, conv.ID)
	return conv, nil
}

func (s *Service) resolveTaskModel(ctx context.Context, task *store.ScheduledTask, conv *store.Conversation, userID string) (*store.Model, *store.Provider, error) {
	if task.ModelID != nil && *task.ModelID != "" {
		if m, p, ok := s.usableModel(ctx, *task.ModelID); ok {
			return m, p, nil
		}
	}
	if m, p, err := s.pickModel(ctx, conv, userID); err == nil && m.Tools {
		return m, p, nil
	}
	models, err := s.Store.ListModels(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range models {
		if mm, p, ok := s.usableModel(ctx, m.ID); ok && mm.Tools {
			return mm, p, nil
		}
	}
	return s.pickModel(ctx, conv, userID)
}

func (s *Service) buildTaskTargetContext(ctx context.Context, userID string, task *store.ScheduledTask) string {
	var entityID string
	var comp store.MessageComponent
	var version int

	if task.TargetEntityID != "" {
		ent, err := s.Store.GetComponentEntity(ctx, task.TargetEntityID, userID)
		if err != nil {
			return fmt.Sprintf("（此前绑定的目标卡片 entity_id=%s 已不存在；如需要请先创建新卡片并调用 pin_component 固定到首页。）", task.TargetEntityID)
		}
		entityID = ent.ID
		version = ent.CurrentVersion
		comp = store.MessageComponent{
			ID:       ent.OriginComponentID,
			EntityID: ent.ID,
			Kind:     ent.Kind,
			Version:  ent.CurrentVersion,
			Data:     ent.Data,
			State:    ent.State,
		}
	} else {
		if task.TargetMessageID == "" || task.TargetComponentID == "" {
			return "（当前定时任务尚未绑定目标卡片；如果任务要求维护/更新卡片，可先调用 list_components 查找已有卡片，或创建一张新卡片并调用 pin_component 固定到首页。）"
		}
		_, mc, err := s.Store.GetMessageComponent(ctx, task.TargetMessageID, userID, task.TargetComponentID)
		if err != nil {
			return fmt.Sprintf("（此前绑定的目标卡片 message_id=%s component_id=%s 已不存在；如需要请先创建新卡片并调用 pin_component 固定到首页。）",
				task.TargetMessageID, task.TargetComponentID)
		}
		comp = *mc
		entityID = mc.EntityID
		version = mc.Version
		if entityID != "" {
			if ent, err := s.Store.GetComponentEntity(ctx, entityID, userID); err == nil {
				version = ent.CurrentVersion
				comp.Data = ent.Data
				comp.State = ent.State
			}
		}
	}
	title, summary := summarizeComponentForQuote(comp)
	var b strings.Builder
	fmt.Fprintf(&b, "## 已绑定的目标卡片（请直接调用 update_component 原地更新，不要重复创建新卡片）\n")
	if entityID != "" {
		fmt.Fprintf(&b, "- entity_id: %s (当前版本 v%d)\n", entityID, max(1, version))
	}
	if task.TargetMessageID != "" && task.TargetComponentID != "" {
		fmt.Fprintf(&b, "- message_id: %s\n- component_id: %s\n", task.TargetMessageID, task.TargetComponentID)
	}
	fmt.Fprintf(&b, "- kind: %s\n- 标题：%s（%s）\n", comp.Kind, title, summary)
	fmt.Fprintf(&b, "- 当前 data JSON：%s\n", string(comp.Data))
	if len(comp.State) > 0 && string(comp.State) != "{}" {
		fmt.Fprintf(&b, "- 当前 state JSON：%s\n", string(comp.State))
	}
	return b.String()
}

func (s *Service) buildAutomationSystemPrompt(ctx context.Context, user *store.User, conv *store.Conversation, ts toolSet, task *store.ScheduledTask, targetBlock string) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString(s.searchPrompt(ctx, ts))
	if ts.canRecall {
		b.WriteString(memoryToolPrompt)
	}
	now := time.Now().In(shanghai)
	fmt.Fprintf(&b, "\n\n# 后台定时任务执行模式\n当前精准时间：%s（%s）。\n你正在后台自动执行用户「%s」配置的定时任务「%s」（规则：%s）。\n",
		now.Format("2006-01-02 15:04"), weekdays[now.Weekday()], user.DisplayName, task.Title, store.FormatTaskScheduleLabel(task))
	b.WriteString(`执行准则：
1. 你已自动解锁所有常用工具（web_search、web_fetch、list_components、update_component、show_feed/show_todo/show_weather/show_metrics/show_chart、pin_component、list_schedules 等），请直接调用完成任务，无需再问用户确认。
2. 更新已绑定的列表类卡片（如“更新前三条，把原来的前三条放到后面三条”）时，优先使用 update_component 的 prepend_items + max_items（如 prepend_items 放新 3 条，max_items 设为 6，feed 可配 previous_tag="往期"），确保旧条目零丢失、顺序精准！
3. 如果需要分析已有任务/日程，请调用 list_schedules 和 list_components 获取真实数据再分析。
4. 完成后输出简洁清晰的中文执行摘要（2-5 句话），说明本次查到了什么、更新了什么，不要输出内部 ID。
`)
	b.WriteString("\n" + targetBlock)
	return b.String()
}
