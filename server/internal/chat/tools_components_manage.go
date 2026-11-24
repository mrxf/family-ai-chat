package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nuan/internal/store"
)

func (s *Service) checkComponentManageGuard(t *toolTurn) string {
	if t.private {
		return "当前是隐私对话模式，暂不支持查询或修改跨对话组件。"
	}
	if t.componentManageCalls >= maxComponentManageCalls {
		return "本轮操作组件的次数已达上限，请直接根据已有结果回复用户。"
	}
	t.componentManageCalls++
	return ""
}

func (s *Service) execListComponents(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentManageGuard(t); msg != "" {
		return msg
	}
	var args struct {
		Scope   string `json:"scope"`
		Keyword string `json:"keyword"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	scope := strings.ToLower(strings.TrimSpace(args.Scope))
	if scope == "" {
		scope = "all"
	}

	stepIdx := t.run.startStep("component", "查看已有卡片组件", "")
	t.run.setActivity("正在读取卡片组件…")
	t.postActivity = "正在整理卡片内容…"

	var records []store.ComponentRecord
	var err error
	switch scope {
	case "pinned":
		records, err = s.Store.ListUserComponents(ctx, t.user.ID, "", false, 20)
	case "conversation":
		all, e := s.Store.ListUserComponents(ctx, t.user.ID, t.convID, false, 30)
		err = e
		for _, r := range all {
			if r.ConversationID == t.convID {
				records = append(records, r)
			}
		}
	default:
		records, err = s.Store.ListUserComponents(ctx, t.user.ID, t.convID, true, 24)
	}
	if err != nil {
		t.run.finishStep(stepIdx, "error", "读取卡片组件失败", "")
		return "查询卡片组件失败：" + err.Error()
	}

	kw := strings.ToLower(strings.TrimSpace(args.Keyword))
	var filtered []store.ComponentRecord
	for _, r := range records {
		if kw != "" {
			title, summary := summarizeComponentForQuote(r.Component)
			hay := strings.ToLower(title + " " + summary + " " + string(r.Component.Data) + " " + r.ConversationTitle)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	t.run.finishStep(stepIdx, "done", "已查看卡片组件", fmt.Sprintf("找到 %d 张卡片", len(filtered)))

	if len(filtered) == 0 {
		return "当前没有找到匹配的卡片组件。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 张卡片组件（message_id / component_id 仅供后续工具调用，不要输出给用户）：\n", len(filtered))
	for i, r := range filtered {
		if i >= 15 {
			fmt.Fprintf(&b, "…其余 %d 张已省略\n", len(filtered)-15)
			break
		}
		title, summary := summarizeComponentForQuote(r.Component)
		pinTag := "未固定"
		if r.Pinned {
			pinTag = "已固定在首页"
		}
		fmt.Fprintf(&b, "\n### #%d [%s] 《%s》（%s）\n", i+1, r.Component.Kind, title, pinTag)
		fmt.Fprintf(&b, "- message_id: %s\n- component_id: %s\n", r.MessageID, r.ComponentID)
		if summary != "" {
			fmt.Fprintf(&b, "- 摘要：%s\n", summary)
		}
		fmt.Fprintf(&b, "- data: %s\n", string(r.Component.Data))
		if len(r.Component.State) > 0 && string(r.Component.State) != "{}" {
			fmt.Fprintf(&b, "- state: %s\n", string(r.Component.State))
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Service) execUpdateComponent(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentManageGuard(t); msg != "" {
		return msg
	}
	var args struct {
		MessageID     string         `json:"message_id"`
		ComponentID   string         `json:"component_id"`
		Data          map[string]any `json:"data"`
		PrependItems  []any          `json:"prepend_items"`
		AppendItems   []any          `json:"append_items"`
		RemoveItemIDs []string       `json:"remove_item_ids"`
		MaxItems      int            `json:"max_items"`
		DoneIDs       *[]string      `json:"done_ids"`
		PreviousTag   string         `json:"previous_tag"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "参数解析失败：需要合法的 JSON 对象（包含 message_id 与 component_id）。"
	}
	msgID := strings.TrimSpace(args.MessageID)
	compID := strings.TrimSpace(args.ComponentID)
	if msgID == "" || compID == "" {
		return "参数错误：update_component 需要提供真实的 message_id 和 component_id（可先调 list_components 查询）。"
	}

	_, cur, err := s.Store.GetMessageComponent(ctx, msgID, t.user.ID, compID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Sprintf("未找到指定组件（message_id=%q component_id=%q），请先调用 list_components 核对真实 ID。", msgID, compID)
		}
		return "读取原组件失败：" + err.Error()
	}
	if cur.Kind == "ask_user" {
		return "ask_user 询问卡片不支持原地修改内容。"
	}

	var nextData json.RawMessage
	var nextState *json.RawMessage

	switch cur.Kind {
	case "feed":
		nextData, err = applyFeedUpdate(cur.Data, args.Data, args.PrependItems, args.AppendItems, args.RemoveItemIDs, args.MaxItems, args.PreviousTag)
	case "todo":
		nextData, nextState, err = applyTodoUpdate(cur.Data, cur.State, args.Data, args.PrependItems, args.AppendItems, args.RemoveItemIDs, args.MaxItems, args.DoneIDs)
	default:
		merged := mergeJSONObject(cur.Data, args.Data)
		rawMerged, mErr := json.Marshal(merged)
		if mErr != nil {
			err = mErr
		} else {
			nextData, err = NormalizeComponentData(cur.Kind, rawMerged)
		}
	}
	if err != nil {
		return "更新组件参数无效：" + err.Error()
	}

	stepIdx := t.run.startStep("component", "更新卡片内容", "")
	t.run.setActivity("正在更新卡片内容…")
	t.postActivity = "正在整理更新结果…"

	_, updatedComp, err := s.Store.UpdateMessageComponent(ctx, msgID, t.user.ID, compID, nextData, nextState)
	if err != nil {
		t.run.finishStep(stepIdx, "error", "更新卡片失败", err.Error())
		return "保存更新后的组件失败：" + err.Error()
	}
	if liveRun := s.Hub.Get(msgID); liveRun != nil && liveRun.UserID == t.user.ID {
		liveRun.UpdateComponent(compID, updatedComp.Data, updatedComp.State)
	}
	t.updatedComponents++
	t.lastUpdatedMsgID = msgID
	t.lastUpdatedCompID = compID

	title, summary := summarizeComponentForQuote(*updatedComp)
	t.run.finishStep(stepIdx, "done", "已更新卡片："+clipRunes(title, 20), summary)
	return fmt.Sprintf("卡片「%s」已原地更新成功（%s；所有引用处和首页看板已实时同步最新内容）。", title, summary)
}

func mergeJSONObject(base json.RawMessage, patch map[string]any) map[string]any {
	merged := map[string]any{}
	_ = json.Unmarshal(base, &merged)
	for k, v := range patch {
		merged[k] = v
	}
	return merged
}

func applyFeedUpdate(curRaw json.RawMessage, patch map[string]any, prependRaw, appendRaw []any, removeIDs []string, maxItems int, previousTag string) (json.RawMessage, error) {
	var cur feedCardData
	_ = json.Unmarshal(curRaw, &cur)

	if patch != nil {
		if t := clipRunes(strVal(patch["title"]), 60); t != "" {
			cur.Title = t
		}
		if _, hasSub := patch["subtitle"]; hasSub {
			cur.Subtitle = clipRunes(strVal(patch["subtitle"]), 80)
		}
		if itemsRaw, ok := patch["items"].([]any); ok {
			cur.Items = parseFeedItems(itemsRaw, nil)
		}
	}

	removeSet := make(map[string]bool, len(removeIDs))
	for _, id := range removeIDs {
		removeSet[strings.TrimSpace(id)] = true
	}
	if len(removeSet) > 0 {
		kept := cur.Items[:0]
		for _, it := range cur.Items {
			if !removeSet[it.ID] {
				kept = append(kept, it)
			}
		}
		cur.Items = kept
	}

	usedIDs := make(map[string]bool, len(cur.Items))
	for _, it := range cur.Items {
		if it.ID != "" {
			usedIDs[it.ID] = true
		}
	}
	if len(prependRaw) > 0 {
		prepended := parseFeedItems(prependRaw, usedIDs)
		if prevTag := clipRunes(strings.TrimSpace(previousTag), 18); prevTag != "" {
			for i := range cur.Items {
				cur.Items[i].Tag = prevTag
			}
		}
		cur.Items = append(prepended, cur.Items...)
	}
	if len(appendRaw) > 0 {
		appended := parseFeedItems(appendRaw, usedIDs)
		cur.Items = append(cur.Items, appended...)
	}
	if maxItems > 0 && len(cur.Items) > maxItems {
		cur.Items = cur.Items[:maxItems]
	}
	if len(cur.Items) > 20 {
		cur.Items = cur.Items[:20]
	}
	if len(cur.Items) == 0 {
		return nil, fmt.Errorf("更新后资讯列表不能为空，至少保留 1 条")
	}
	if cur.Title == "" {
		cur.Title = "动态资讯"
	}
	cur.UpdatedAt = store.Now()
	return json.Marshal(cur)
}

func applyTodoUpdate(curRaw, curStateRaw json.RawMessage, patch map[string]any, prependRaw, appendRaw []any, removeIDs []string, maxItems int, explicitDoneIDs *[]string) (json.RawMessage, *json.RawMessage, error) {
	var cur todoCardData
	var st todoCardState
	_ = json.Unmarshal(curRaw, &cur)
	_ = json.Unmarshal(curStateRaw, &st)

	doneSet := map[string]bool{}
	if len(curStateRaw) > 0 && strings.Contains(string(curStateRaw), "done_ids") {
		for _, id := range st.DoneIDs {
			doneSet[id] = true
		}
	} else {
		for _, it := range cur.Items {
			if it.Done {
				doneSet[it.ID] = true
			}
		}
	}

	if patch != nil {
		if t := clipRunes(strVal(patch["title"]), 60); t != "" {
			cur.Title = t
		}
		if _, hasSub := patch["subtitle"]; hasSub {
			cur.Subtitle = clipRunes(strVal(patch["subtitle"]), 60)
		}
		if itemsRaw, ok := patch["items"].([]any); ok {
			items, newDone := parseTodoItems(itemsRaw, nil)
			cur.Items = items
			doneSet = map[string]bool{}
			for _, id := range newDone {
				doneSet[id] = true
			}
		}
	}

	removeSet := make(map[string]bool, len(removeIDs))
	for _, id := range removeIDs {
		removeSet[strings.TrimSpace(id)] = true
	}
	if len(removeSet) > 0 {
		kept := cur.Items[:0]
		for _, it := range cur.Items {
			if !removeSet[it.ID] {
				kept = append(kept, it)
			} else {
				delete(doneSet, it.ID)
			}
		}
		cur.Items = kept
	}

	usedIDs := make(map[string]bool, len(cur.Items))
	for _, it := range cur.Items {
		if it.ID != "" {
			usedIDs[it.ID] = true
		}
	}
	if len(prependRaw) > 0 {
		prepended, newDone := parseTodoItems(prependRaw, usedIDs)
		for _, id := range newDone {
			doneSet[id] = true
		}
		cur.Items = append(prepended, cur.Items...)
	}
	if len(appendRaw) > 0 {
		appended, newDone := parseTodoItems(appendRaw, usedIDs)
		for _, id := range newDone {
			doneSet[id] = true
		}
		cur.Items = append(cur.Items, appended...)
	}
	if maxItems > 0 && len(cur.Items) > maxItems {
		cur.Items = cur.Items[:maxItems]
	}
	if len(cur.Items) > 30 {
		cur.Items = cur.Items[:30]
	}
	if len(cur.Items) == 0 {
		return nil, nil, fmt.Errorf("更新后待办清单不能为空，至少保留 1 条")
	}
	if explicitDoneIDs != nil {
		doneSet = map[string]bool{}
		for _, id := range *explicitDoneIDs {
			doneSet[strings.TrimSpace(id)] = true
		}
	}
	orderedDone := []string{}
	for i := range cur.Items {
		if doneSet[cur.Items[i].ID] {
			cur.Items[i].Done = true
			orderedDone = append(orderedDone, cur.Items[i].ID)
		} else {
			cur.Items[i].Done = false
		}
	}
	if cur.Title == "" {
		cur.Title = "待办清单"
	}
	cur.UpdatedAt = store.Now()
	rawData, err := json.Marshal(cur)
	if err != nil {
		return nil, nil, err
	}
	rawState, err := json.Marshal(todoCardState{DoneIDs: orderedDone})
	if err != nil {
		return nil, nil, err
	}
	stateMsg := json.RawMessage(rawState)
	return rawData, &stateMsg, nil
}

func (s *Service) execDeleteComponent(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentManageGuard(t); msg != "" {
		return msg
	}
	var args struct {
		MessageID   string `json:"message_id"`
		ComponentID string `json:"component_id"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	msgID := strings.TrimSpace(args.MessageID)
	compID := strings.TrimSpace(args.ComponentID)
	if msgID == "" || compID == "" {
		return "参数错误：需要提供 message_id 和 component_id。"
	}
	if err := s.Store.DeleteMessageComponent(ctx, msgID, t.user.ID, compID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "未找到该卡片，可能已被删除。"
		}
		return "删除卡片失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已删除卡片组件", "")
	return "已永久删除该卡片，并同步从首页看板移除。"
}
