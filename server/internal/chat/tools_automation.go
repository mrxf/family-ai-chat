package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const (
	skillKeyAutomation = "automation"
	maxAutomationCalls = 8
)

var listScheduledTasksTool = llm.Tool{
	Name:        "list_scheduled_tasks",
	Description: "List all AI scheduled automation tasks configured by the user (including schedule rule, enabled status, bound target component, next run time, and last run summary).",
	Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
}

var createScheduledTaskTool = llm.Tool{
	Name: "create_scheduled_task",
	Description: "Create an autonomous background AI scheduled task that wakes up on schedule to search the web, analyze existing schedules/components, and update pinned cards in place. " +
		"Use this (NOT create_schedule) whenever the user wants the AI to periodically DO something (e.g. fetch news every morning at 07:00 and update a card, or review daily tasks every evening).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Concise Chinese task title (1-80 chars), e.g. \"每日大模型新闻更新\", \"每晚日程与待办复盘\".",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Clear, self-contained instruction for what the AI should do on every scheduled run (include concrete tool steps, e.g. search latest AI news -> update target card via update_component with prepend_items + max_items:6).",
			},
			"schedule_kind": map[string]any{
				"type":        "string",
				"enum":        []string{"daily", "weekdays", "weekly", "monthly", "interval", "once"},
				"description": "Schedule frequency: \"daily\" (default), \"weekdays\" (Mon-Fri), \"weekly\", \"monthly\", \"interval\", or \"once\".",
			},
			"time_of_day": map[string]any{
				"type":        "string",
				"description": "24-hour \"HH:MM\" in Asia/Shanghai (required for daily/weekdays/weekly/monthly), e.g. \"07:00\", \"21:30\".",
			},
			"weekdays": map[string]any{
				"type":        "string",
				"description": "Only for schedule_kind=\"weekly\": comma-separated ISO weekdays \"1\"..\"7\" (1=Mon..7=Sun), e.g. \"1,3,5\".",
			},
			"day_of_month": map[string]any{
				"type":        "integer",
				"description": "Only for schedule_kind=\"monthly\": day 1..31.",
			},
			"interval_minutes": map[string]any{
				"type":        "integer",
				"description": "Only for schedule_kind=\"interval\": repeat interval in minutes (minimum 15).",
			},
			"run_at": map[string]any{
				"type":        "string",
				"description": "Only for schedule_kind=\"once\": \"YYYY-MM-DD HH:MM\" in Asia/Shanghai.",
			},
			"target_entity_id": map[string]any{
				"type":        "string",
				"description": "Preferred canonical entity_id of the existing live component card to bind for in-place updates.",
			},
			"target_message_id": map[string]any{
				"type":        "string",
				"description": "Legacy fallback message_id of the existing component card (only needed if target_entity_id is unavailable).",
			},
			"target_component_id": map[string]any{
				"type":        "string",
				"description": "Legacy fallback component_id of the existing component card (paired with target_message_id).",
			},
			"run_immediately": map[string]any{
				"type":        "boolean",
				"description": "Optional: set true to also trigger one immediate background run right after creation.",
			},
		},
		"required": []string{"title", "prompt", "schedule_kind"},
	},
}

var updateScheduledTaskTool = llm.Tool{
	Name:        "update_scheduled_task",
	Description: "Update an existing AI scheduled task's title, prompt, schedule rule/time, enabled state (pause/resume), or bound target component by task_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":             map[string]any{"type": "string", "description": "Exact task_id from list_scheduled_tasks or read_skill snapshot."},
			"title":               map[string]any{"type": "string"},
			"prompt":              map[string]any{"type": "string"},
			"schedule_kind":       map[string]any{"type": "string", "enum": []string{"daily", "weekdays", "weekly", "monthly", "interval", "once"}},
			"time_of_day":         map[string]any{"type": "string"},
			"weekdays":            map[string]any{"type": "string"},
			"day_of_month":        map[string]any{"type": "integer"},
			"interval_minutes":    map[string]any{"type": "integer"},
			"run_at":              map[string]any{"type": "string"},
			"enabled":             map[string]any{"type": "boolean", "description": "true to resume, false to pause."},
			"target_entity_id":    map[string]any{"type": "string", "description": "Preferred canonical entity_id of the bound component card."},
			"target_message_id":   map[string]any{"type": "string"},
			"target_component_id": map[string]any{"type": "string"},
		},
		"required": []string{"task_id"},
	},
}

var deleteScheduledTaskTool = llm.Tool{
	Name:        "delete_scheduled_task",
	Description: "Permanently delete an AI scheduled task by task_id.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "Exact task_id to delete."},
		},
		"required": []string{"task_id"},
	},
}

var runScheduledTaskTool = llm.Tool{
	Name:        "run_scheduled_task",
	Description: "Trigger an AI scheduled task to run immediately in the background (without changing its regular future schedule).",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "Exact task_id to run now."},
		},
		"required": []string{"task_id"},
	},
}

const automationSkillSOP = `# AI 定时任务与自动化管理指南

你可以为用户创建、查看、暂停/恢复、修改、立即运行或删除**后台 AI 定时任务**。到点时服务端会自动唤醒 AI，执行联网查询、分析已有日程/待办/记忆，并**原地更新用户首页固定的卡片组件**。

## 一、明确区分「AI 定时任务 (automation)」与「日程提醒 (schedule)」
- **日程提醒 (` + "`schedule`" + `)**：到点在手机响铃提醒**人**去做某事（吃药、开会、复查、接孩子）。
- **AI 定时任务 (` + "`automation`" + `)**：到点唤醒 **AI** 在后台自动干活（如“每天早上 7 点帮我查最新大模型新闻并更新卡片”“每晚 9 点分析我今天的日程和未完成待办清单”“每 2 小时帮我关注某话题动态”）。

## 二、绑定卡片与“滚动更新”场景的最佳实践（非常重要）
当用户提出类似「每天早上 9 点帮我查询最新 AI 新闻，更新前三条，把原来的前三条放到后面三条」的需求时：
1. **检查是否已有目标卡片**：
   - 若用户本轮已引用某张卡片（上下文里已有其 ` + "`entity_id`" + ` 或 ` + "`message_id`" + ` / ` + "`component_id`" + `），直接将其绑定到 ` + "`create_scheduled_task`" + ` 的 ` + "`target_entity_id`" + `（优先传 ` + "`target_entity_id`" + `）。
   - 若未引用，先调 ` + "`ui_components`" + ` 技能的 ` + "`list_components`" + ` 看首页是否已有对应卡片。
   - **若还没有这张卡片**：强烈建议在本轮**先用 ` + "`web_search`" + ` 查出首批内容 → 调用 ` + "`show_feed`" + `（资讯/新闻类）或 ` + "`show_todo`" + `（任务类）创建卡片 → 调用 ` + "`pin_component`" + ` 固定到首页 → 再用新卡片的 ` + "`entity_id`" + ` 调用 ` + "`create_scheduled_task`" + ` 的 ` + "`target_entity_id`" + ` 绑定定时更新**！这样用户马上就能在首页看到初始卡片，且每天到点会自动原地滚动更新同一张卡片并递增版本号。
2. **写好自包含的任务指令 (` + "`prompt`" + `)**：
   - 后台定时执行时看不到本轮闲聊，所以 ` + "`prompt`" + ` 必须写清具体动作与规则！例如：
     - 「使用 web_search 查询近 24 小时最新的 3 条 AI 重磅新闻，然后调用 update_component 更新绑定的资讯卡片：用 prepend_items 插入这 3 条最新新闻（tag 设为「今日」），设置 previous_tag="往期" 且 max_items=6，让原来的前 3 条自动顺延到第 4-6 条。」
     - 「调用 list_schedules 查看今明两天日程，并调用 list_components 查看首页未完成待办清单，综合分析今天的完成情况与明天的优先事项，给出精炼复盘建议。」

## 三、表达规范
- 创建或修改后，用简洁亲切的话向用户确认：**任务名称、触发时间/周期、每次会自动做什么、已绑定哪张首页卡片（若有）**，并告知用户可随时在「AI 定时任务」页面手动试跑、暂停或查看每次执行记录。
- 严禁向用户输出内部 ` + "`task_id`" + `、` + "`entity_id`" + `、` + "`message_id`" + ` 或 ` + "`component_id`" + `。`

const automationLeaseSummary = "AI 定时任务技能（automation）当前已激活，可直接调用 list_scheduled_tasks / create_scheduled_task / update_scheduled_task / delete_scheduled_task / run_scheduled_task。"

func (s *Service) buildAutomationSkill() skills.Definition {
	return skills.Definition{
		Key:          skillKeyAutomation,
		Title:        "AI 定时任务与自动化",
		Description:  "创建和管理后台 AI 定时任务：按每天/工作日/每周/每月/固定间隔/单次自动唤醒 AI，定时联网搜新闻、定期分析已有日程与待办任务、原地滚动更新首页卡片组件。",
		WhenToUse:    "当用户要求 AI 定期自动执行某项分析、定时查询资讯并更新卡片、定期复盘已有日程/任务，或查看/修改/暂停/立即运行/删除 AI 定时任务时使用。",
		LeaseSummary: automationLeaseSummary,
		ToolHints: []skills.ToolHint{
			{Name: listScheduledTasksTool.Name, Brief: "查看用户已配置的所有 AI 定时任务与最近执行状态"},
			{Name: createScheduledTaskTool.Name, Brief: "新建 AI 定时任务（每天/工作日/每周/每月/间隔/单次，可绑定目标卡片原地更新）"},
			{Name: updateScheduledTaskTool.Name, Brief: "修改定时任务内容、执行时间、暂停/恢复或目标卡片"},
			{Name: deleteScheduledTaskTool.Name, Brief: "永久删除定时任务"},
			{Name: runScheduledTaskTool.Name, Brief: "立即在后台试跑一次指定定时任务"},
		},
		Tools: []llm.Tool{
			listScheduledTasksTool,
			createScheduledTaskTool,
			updateScheduledTaskTool,
			deleteScheduledTaskTool,
			runScheduledTaskTool,
		},
		Content: automationSkillSOP,
		Dynamic: func(sctx skills.Context) string {
			return s.renderAutomationSnapshot(sctx)
		},
		Available: func(sctx skills.Context) bool {
			return !sctx.Private && sctx.UserID != ""
		},
	}
}

func (s *Service) renderAutomationSnapshot(sctx skills.Context) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tasks, err := s.Store.ListScheduledTasks(ctx, sctx.UserID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 当前已有 AI 定时任务快照\n")
	if len(tasks) == 0 {
		b.WriteString("- 暂无已配置的 AI 定时任务。")
		return b.String()
	}
	for i, tk := range tasks {
		if i >= 20 {
			fmt.Fprintf(&b, "- …其余 %d 个任务可调用 list_scheduled_tasks 查看\n", len(tasks)-20)
			break
		}
		b.WriteString("- " + formatScheduledTaskForLLM(tk) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func formatScheduledTaskForLLM(tk *store.ScheduledTask) string {
	status := "已启用"
	if !tk.Enabled {
		status = "已暂停"
	}
	parts := []string{store.FormatTaskScheduleLabel(tk), status}
	if tk.NextRunAt > 0 {
		parts = append(parts, "下次:"+time.UnixMilli(tk.NextRunAt).In(shanghai).Format("01-02 15:04"))
	}
	if tk.TargetEntityID != "" {
		parts = append(parts, fmt.Sprintf("绑定实体:%s", tk.TargetEntityID))
	} else if tk.TargetMessageID != "" && tk.TargetComponentID != "" {
		parts = append(parts, fmt.Sprintf("绑定卡片:%s/%s", tk.TargetMessageID, tk.TargetComponentID))
	}
	if tk.LastStatus != "" {
		parts = append(parts, "上次:"+tk.LastStatus)
	}
	return fmt.Sprintf("[%s] %s（%s） | task_id=%s", strings.Join(parts, " · "), tk.Title, clipRunes(tk.Prompt, 60), tk.ID)
}

func (s *Service) execAutomationTool(ctx context.Context, t *toolTurn, name, rawArgs string) (string, bool) {
	switch name {
	case listScheduledTasksTool.Name:
		return s.execListScheduledTasks(ctx, t), true
	case createScheduledTaskTool.Name:
		return s.execCreateScheduledTask(ctx, t, rawArgs), true
	case updateScheduledTaskTool.Name:
		return s.execUpdateScheduledTask(ctx, t, rawArgs), true
	case deleteScheduledTaskTool.Name:
		return s.execDeleteScheduledTask(ctx, t, rawArgs), true
	case runScheduledTaskTool.Name:
		return s.execRunScheduledTask(ctx, t, rawArgs), true
	default:
		return "", false
	}
}

func (s *Service) checkAutomationGuard(t *toolTurn) string {
	if t.private {
		return "当前是隐私对话模式，暂不支持读取或修改 AI 定时任务。"
	}
	if t.automationCalls >= maxAutomationCalls {
		return "本轮操作定时任务的次数已达上限，请直接根据已有结果回复用户。"
	}
	t.automationCalls++
	return ""
}

func (s *Service) execListScheduledTasks(ctx context.Context, t *toolTurn) string {
	if msg := s.checkAutomationGuard(t); msg != "" {
		return msg
	}
	stepIdx := t.run.startStep("automation", "查看 AI 定时任务", "")
	tasks, err := s.Store.ListScheduledTasks(ctx, t.user.ID)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "查看定时任务失败", "")
		return "查询定时任务失败：" + err.Error()
	}
	t.run.finishStep(stepIdx, "done", "已查看定时任务", fmt.Sprintf("共 %d 个", len(tasks)))
	if len(tasks) == 0 {
		return "当前还没有配置任何 AI 定时任务。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "当前共有 %d 个 AI 定时任务（task_id 仅供内部工具使用，切勿输出给用户）：\n", len(tasks))
	for _, tk := range tasks {
		b.WriteString("- " + formatScheduledTaskForLLM(tk) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) execCreateScheduledTask(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkAutomationGuard(t); msg != "" {
		return msg
	}
	var args struct {
		Title             string `json:"title"`
		Prompt            string `json:"prompt"`
		ScheduleKind      string `json:"schedule_kind"`
		TimeOfDay         string `json:"time_of_day"`
		Weekdays          string `json:"weekdays"`
		DayOfMonth        int    `json:"day_of_month"`
		IntervalMinutes   int    `json:"interval_minutes"`
		RunAt             string `json:"run_at"`
		TargetEntityID    string `json:"target_entity_id"`
		TargetMessageID   string `json:"target_message_id"`
		TargetComponentID string `json:"target_component_id"`
		RunImmediately    bool   `json:"run_immediately"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "参数解析失败：需要合法的 JSON 对象（包含 title, prompt, schedule_kind）。"
	}
	var runAtMs int64
	if strings.TrimSpace(args.RunAt) != "" {
		if ms, ok := parseFlexibleScheduleTime(args.RunAt, false, time.Now().In(shanghai)); ok {
			runAtMs = ms
		} else {
			return fmt.Sprintf("run_at 时间格式无法解析：%q，请使用 \"YYYY-MM-DD HH:MM\"。", args.RunAt)
		}
	}
	targetEntID := strings.TrimSpace(args.TargetEntityID)
	targetMsgID := strings.TrimSpace(args.TargetMessageID)
	targetCompID := strings.TrimSpace(args.TargetComponentID)
	if targetEntID != "" {
		ent, err := s.Store.GetComponentEntity(ctx, targetEntID, t.user.ID)
		if err != nil {
			return "绑定的目标卡片不存在，请先调用 list_components 核对真实的 entity_id。"
		}
		if targetMsgID == "" {
			targetMsgID = ent.OriginMessageID
			targetCompID = ent.OriginComponentID
		}
	} else if targetMsgID != "" && targetCompID != "" {
		ent, err := s.Store.EnsureComponentEntityForMessage(ctx, targetMsgID, t.user.ID, targetCompID, "")
		if err != nil {
			return "绑定的目标卡片不存在，请先调用 list_components 核对真实的 entity_id 或 message_id + component_id。"
		}
		targetEntID = ent.ID
	} else {
		targetMsgID, targetCompID = "", ""
	}

	item := &store.ScheduledTask{
		UserID:            t.user.ID,
		Title:             args.Title,
		Prompt:            args.Prompt,
		ScheduleKind:      args.ScheduleKind,
		TimeOfDay:         args.TimeOfDay,
		Weekdays:          args.Weekdays,
		DayOfMonth:        args.DayOfMonth,
		IntervalMinutes:   args.IntervalMinutes,
		RunAt:             runAtMs,
		Enabled:           true,
		TargetEntityID:    targetEntID,
		TargetMessageID:   targetMsgID,
		TargetComponentID: targetCompID,
		Source:            "ai",
	}

	stepIdx := t.run.startStep("automation", "创建定时任务："+clipRunes(strings.TrimSpace(args.Title), 20), "")
	if err := s.Store.CreateScheduledTask(ctx, item); err != nil {
		t.run.finishStep(stepIdx, "error", "创建定时任务失败", err.Error())
		return "创建定时任务失败：" + err.Error()
	}
	label := store.FormatTaskScheduleLabel(item)
	t.run.finishStep(stepIdx, "done", "已创建定时任务："+clipRunes(item.Title, 20), label)

	extra := ""
	if args.RunImmediately && s.Automation != nil {
		if err := s.Automation.TriggerNow(t.user.ID, item.ID); err == nil {
			extra = "，并已立即触发一次后台试跑"
		}
	}
	nextStr := ""
	if item.NextRunAt > 0 {
		nextStr = fmt.Sprintf("，下次计划执行时间：%s", time.UnixMilli(item.NextRunAt).In(shanghai).Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("AI 定时任务「%s」创建成功（周期：%s%s%s；内部 task_id=%s，切勿向用户输出）。",
		item.Title, label, nextStr, extra, item.ID)
}

func (s *Service) execUpdateScheduledTask(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkAutomationGuard(t); msg != "" {
		return msg
	}
	var args struct {
		TaskID            string  `json:"task_id"`
		ID                string  `json:"id"`
		Title             *string `json:"title"`
		Prompt            *string `json:"prompt"`
		ScheduleKind      *string `json:"schedule_kind"`
		TimeOfDay         *string `json:"time_of_day"`
		Weekdays          *string `json:"weekdays"`
		DayOfMonth        *int    `json:"day_of_month"`
		IntervalMinutes   *int    `json:"interval_minutes"`
		RunAt             *string `json:"run_at"`
		Enabled           *bool   `json:"enabled"`
		TargetEntityID    *string `json:"target_entity_id"`
		TargetMessageID   *string `json:"target_message_id"`
		TargetComponentID *string `json:"target_component_id"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "参数解析失败：请检查 JSON 格式。"
	}
	id := firstNonEmpty(args.TaskID, args.ID)
	if id == "" {
		return "参数错误：需要提供真实的 task_id。"
	}
	tEnt := args.TargetEntityID
	tMsg := args.TargetMessageID
	tComp := args.TargetComponentID
	if tEnt != nil && strings.TrimSpace(*tEnt) != "" {
		eid := strings.TrimSpace(*tEnt)
		ent, err := s.Store.GetComponentEntity(ctx, eid, t.user.ID)
		if err != nil {
			return "绑定的目标卡片不存在，请先调用 list_components 核对真实的 entity_id。"
		}
		tEnt = &eid
		if tMsg == nil || strings.TrimSpace(*tMsg) == "" {
			om, oc := ent.OriginMessageID, ent.OriginComponentID
			tMsg, tComp = &om, &oc
		}
	} else if tMsg != nil && tComp != nil &&
		strings.TrimSpace(*tMsg) != "" && strings.TrimSpace(*tComp) != "" {
		ent, err := s.Store.EnsureComponentEntityForMessage(ctx, strings.TrimSpace(*tMsg), t.user.ID, strings.TrimSpace(*tComp), "")
		if err != nil {
			return "绑定的目标卡片不存在，请先调用 list_components 核对真实的 entity_id 或 message_id + component_id。"
		}
		eid := ent.ID
		tEnt = &eid
	}
	patch := store.ScheduledTaskPatch{
		Title:             args.Title,
		Prompt:            args.Prompt,
		ScheduleKind:      args.ScheduleKind,
		TimeOfDay:         args.TimeOfDay,
		Weekdays:          args.Weekdays,
		DayOfMonth:        args.DayOfMonth,
		IntervalMinutes:   args.IntervalMinutes,
		Enabled:           args.Enabled,
		TargetEntityID:    tEnt,
		TargetMessageID:   tMsg,
		TargetComponentID: tComp,
	}
	if args.RunAt != nil && strings.TrimSpace(*args.RunAt) != "" {
		if ms, ok := parseFlexibleScheduleTime(*args.RunAt, false, time.Now().In(shanghai)); ok {
			patch.RunAt = &ms
		} else {
			return fmt.Sprintf("run_at 格式无法解析：%q。", *args.RunAt)
		}
	}

	stepIdx := t.run.startStep("automation", "更新定时任务", "")
	updated, err := s.Store.UpdateScheduledTask(ctx, id, t.user.ID, patch)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "更新定时任务失败", err.Error())
		if errors.Is(err, store.ErrNotFound) {
			return "未找到该定时任务，请先调 list_scheduled_tasks 获取真实 task_id。"
		}
		return "更新定时任务失败：" + err.Error()
	}
	t.run.finishStep(stepIdx, "done", "已更新定时任务："+clipRunes(updated.Title, 20), store.FormatTaskScheduleLabel(updated))
	stateText := "已启用"
	if !updated.Enabled {
		stateText = "已暂停"
	}
	return fmt.Sprintf("定时任务「%s」已更新（%s · %s）。", updated.Title, store.FormatTaskScheduleLabel(updated), stateText)
}

func (s *Service) execDeleteScheduledTask(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkAutomationGuard(t); msg != "" {
		return msg
	}
	var args struct {
		TaskID string `json:"task_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	id := firstNonEmpty(args.TaskID, args.ID)
	if id == "" {
		return "参数错误：需要提供真实的 task_id。"
	}
	cur, err := s.Store.GetScheduledTask(ctx, id, t.user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "未找到该定时任务，可能已被删除。"
		}
		return "查询定时任务失败：" + err.Error()
	}
	stepIdx := t.run.startStep("automation", "删除定时任务："+clipRunes(cur.Title, 20), "")
	if err := s.Store.DeleteScheduledTask(ctx, id, t.user.ID); err != nil {
		t.run.finishStep(stepIdx, "error", "删除定时任务失败", "")
		return "删除定时任务失败：" + err.Error()
	}
	t.run.finishStep(stepIdx, "done", "已删除定时任务："+clipRunes(cur.Title, 20), "")
	return fmt.Sprintf("已永久删除定时任务「%s」。", cur.Title)
}

func (s *Service) execRunScheduledTask(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkAutomationGuard(t); msg != "" {
		return msg
	}
	var args struct {
		TaskID string `json:"task_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	id := firstNonEmpty(args.TaskID, args.ID)
	if id == "" {
		return "参数错误：需要提供真实的 task_id。"
	}
	cur, err := s.Store.GetScheduledTask(ctx, id, t.user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "未找到该定时任务，请先用 list_scheduled_tasks 确认真实 task_id。"
		}
		return "查询定时任务失败：" + err.Error()
	}
	if s.Automation == nil {
		return "后台定时任务执行器未启动。"
	}
	if err := s.Automation.TriggerNow(t.user.ID, cur.ID); err != nil {
		return "立即执行定时任务失败：" + err.Error()
	}
	t.run.addDoneStep("automation", "已触发立即执行："+clipRunes(cur.Title, 20), "后台执行中")
	return fmt.Sprintf("已触发定时任务「%s」在后台立即执行，稍后可在「AI 定时任务」或首页卡片查看最新结果。", cur.Title)
}
