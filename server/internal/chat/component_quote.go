package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nuan/internal/store"
)

// ComponentQuoteRef identifies an existing message component referenced by a user message.
type ComponentQuoteRef struct {
	MessageID   string `json:"message_id"`
	ComponentID string `json:"component_id"`
}

// resolveQuotedComponent loads the live component data + state from the referenced message
// and snapshots it so the new conversation remains self-contained even if the source is deleted later.
func (s *Service) resolveQuotedComponent(ctx context.Context, userID string, targetConv *store.Conversation, ref *ComponentQuoteRef) (*store.QuotedComponent, error) {
	if ref == nil || strings.TrimSpace(ref.MessageID) == "" || strings.TrimSpace(ref.ComponentID) == "" {
		return nil, nil
	}
	msg, err := s.Store.GetMessageForUser(ctx, ref.MessageID, userID)
	if err != nil {
		return nil, err
	}
	srcConv, err := s.Store.GetConversation(ctx, msg.ConversationID, userID)
	if err != nil {
		return nil, err
	}
	if srcConv.Private && (targetConv == nil || targetConv.ID != srcConv.ID) {
		return nil, store.ErrNotFound
	}
	var comp *store.MessageComponent
	for i := range msg.Components {
		if msg.Components[i].ID == ref.ComponentID {
			cp := msg.Components[i]
			comp = &cp
			break
		}
	}
	if comp == nil {
		return nil, store.ErrNotFound
	}
	title, summary := summarizeComponentForQuote(*comp)
	return &store.QuotedComponent{
		ConversationID:    srcConv.ID,
		ConversationTitle: srcConv.Title,
		MessageID:         msg.ID,
		ComponentID:       comp.ID,
		Kind:              comp.Kind,
		Title:             title,
		Summary:           summary,
		Data:              append(json.RawMessage(nil), comp.Data...),
		State:             append(json.RawMessage(nil), comp.State...),
	}, nil
}

func summarizeComponentForQuote(c store.MessageComponent) (title, summary string) {
	switch c.Kind {
	case "weather":
		var d weatherCardData
		_ = json.Unmarshal(c.Data, &d)
		title = strings.TrimSpace(d.Location)
		if title == "" {
			title = "天气预报"
		}
		if d.DateLabel != "" {
			title += " · " + d.DateLabel
		}
		var parts []string
		if d.Condition != "" {
			parts = append(parts, d.Condition)
		}
		if d.Temp != nil {
			parts = append(parts, fmt.Sprintf("%g°C", *d.Temp))
		} else if d.Low != nil && d.High != nil {
			parts = append(parts, fmt.Sprintf("%g~%g°C", *d.Low, *d.High))
		}
		if len(d.Forecast) > 0 {
			parts = append(parts, fmt.Sprintf("含 %d 天预报", len(d.Forecast)))
		}
		summary = strings.Join(parts, " · ")
	case "todo":
		var d todoCardData
		var st todoCardState
		_ = json.Unmarshal(c.Data, &d)
		_ = json.Unmarshal(c.State, &st)
		title = strings.TrimSpace(d.Title)
		if title == "" {
			title = "待办清单"
		}
		doneMap := map[string]bool{}
		hasState := len(c.State) > 0 && strings.Contains(string(c.State), "done_ids")
		if hasState {
			for _, id := range st.DoneIDs {
				doneMap[id] = true
			}
		} else {
			for _, it := range d.Items {
				if it.Done {
					doneMap[it.ID] = true
				}
			}
		}
		doneCount := 0
		for _, it := range d.Items {
			if doneMap[it.ID] {
				doneCount++
			}
		}
		summary = fmt.Sprintf("已完成 %d/%d 项", doneCount, len(d.Items))
	case "chart":
		var d chartCardData
		_ = json.Unmarshal(c.Data, &d)
		title = strings.TrimSpace(d.Title)
		if title == "" {
			title = "数据图表"
		}
		summary = strings.TrimSpace(d.Subtitle)
		if summary == "" && len(d.Labels) > 0 {
			summary = fmt.Sprintf("%d 组数据", len(d.Labels))
		}
	case "metrics":
		var d metricsCardData
		_ = json.Unmarshal(c.Data, &d)
		title = strings.TrimSpace(d.Title)
		if title == "" {
			title = "核心指标"
		}
		var kvs []string
		for i, m := range d.Metrics {
			if i >= 3 {
				break
			}
			kvs = append(kvs, m.Label+" "+m.Value+m.Unit)
		}
		summary = strings.Join(kvs, " · ")
	case "quiz":
		var d quizCardData
		var st quizCardState
		_ = json.Unmarshal(c.Data, &d)
		_ = json.Unmarshal(c.State, &st)
		title = clipRunes(strings.TrimSpace(d.Question), 36)
		if title == "" {
			title = "互动小测"
		}
		if st.SelectedIndex >= 0 && st.SelectedIndex < len(d.Options) {
			if st.SelectedIndex == d.AnswerIndex {
				summary = "已作答（答对）"
			} else {
				summary = "已作答（待复习）"
			}
		} else {
			summary = "尚未作答"
		}
	default:
		title = "引用卡片"
	}
	return title, summary
}

// quotedComponentPromptNote builds detailed structured context for the LLM when a user
// message quotes a previously generated component (including its latest interactive state).
func quotedComponentPromptNote(q *store.QuotedComponent) string {
	if q == nil || q.Kind == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("（用户引用了以下卡片发起提问，请结合卡片中的最新数据与状态直接回答，不要输出这条附注：\n")
	switch q.Kind {
	case "todo":
		var d todoCardData
		var st todoCardState
		_ = json.Unmarshal(q.Data, &d)
		_ = json.Unmarshal(q.State, &st)
		doneMap := map[string]bool{}
		hasState := len(q.State) > 0 && strings.Contains(string(q.State), "done_ids")
		if hasState {
			for _, id := range st.DoneIDs {
				doneMap[id] = true
			}
		} else {
			for _, it := range d.Items {
				if it.Done {
					doneMap[it.ID] = true
				}
			}
		}
		var doneItems, pendingItems []string
		for _, it := range d.Items {
			label := it.Text
			if it.Note != "" {
				label += "（" + it.Note + "）"
			}
			if doneMap[it.ID] {
				doneItems = append(doneItems, label)
			} else {
				pendingItems = append(pendingItems, label)
			}
		}
		fmt.Fprintf(&b, "- 类型：待办清单《%s》\n", q.Title)
		if len(doneItems) > 0 {
			fmt.Fprintf(&b, "- 已完成（%d 项）：%s\n", len(doneItems), strings.Join(doneItems, "、"))
		} else {
			b.WriteString("- 已完成：无\n")
		}
		if len(pendingItems) > 0 {
			fmt.Fprintf(&b, "- 未完成（%d 项）：%s\n", len(pendingItems), strings.Join(pendingItems, "、"))
		} else {
			b.WriteString("- 未完成：全部已完成\n")
		}
	case "weather":
		var d weatherCardData
		_ = json.Unmarshal(q.Data, &d)
		fmt.Fprintf(&b, "- 类型：天气卡片《%s》\n", q.Title)
		fmt.Fprintf(&b, "- 天气概况：%s", d.Condition)
		if d.Temp != nil {
			fmt.Fprintf(&b, "，当前 %g°C", *d.Temp)
		}
		if d.Low != nil && d.High != nil {
			fmt.Fprintf(&b, "，温度范围 %g~%g°C", *d.Low, *d.High)
		}
		if d.Wind != "" {
			fmt.Fprintf(&b, "，风力 %s", d.Wind)
		}
		if d.Humidity != "" {
			fmt.Fprintf(&b, "，湿度 %s", d.Humidity)
		}
		if d.Tip != "" {
			fmt.Fprintf(&b, "，贴士：%s", d.Tip)
		}
		b.WriteString("\n")
		if len(d.Forecast) > 0 {
			var days []string
			for _, f := range d.Forecast {
				days = append(days, fmt.Sprintf("%s %s %g~%g°C", f.Day, f.Condition, f.Low, f.High))
			}
			fmt.Fprintf(&b, "- 多日预报：%s\n", strings.Join(days, "；"))
		}
	case "metrics":
		var d metricsCardData
		_ = json.Unmarshal(q.Data, &d)
		fmt.Fprintf(&b, "- 类型：指标看板《%s》\n", q.Title)
		for _, m := range d.Metrics {
			line := fmt.Sprintf("  * %s：%s%s", m.Label, m.Value, m.Unit)
			if m.Delta != "" {
				line += "（" + m.Delta + "）"
			}
			b.WriteString(line + "\n")
		}
		if d.Summary != "" {
			fmt.Fprintf(&b, "- 小结：%s\n", d.Summary)
		}
	case "chart":
		var d chartCardData
		_ = json.Unmarshal(q.Data, &d)
		fmt.Fprintf(&b, "- 类型：数据图表《%s》（%s，单位：%s）\n", q.Title, d.ChartType, d.Unit)
		fmt.Fprintf(&b, "- 标签：%s\n", strings.Join(d.Labels, "、"))
		for _, s := range d.Series {
			vals := make([]string, len(s.Values))
			for i, v := range s.Values {
				vals[i] = fmt.Sprintf("%g", v)
			}
			fmt.Fprintf(&b, "- 系列 %s：%s\n", s.Name, strings.Join(vals, "、"))
		}
	case "quiz":
		var d quizCardData
		var st quizCardState
		_ = json.Unmarshal(q.Data, &d)
		_ = json.Unmarshal(q.State, &st)
		fmt.Fprintf(&b, "- 类型：互动小测\n- 题目：%s\n- 选项：%s\n", d.Question, strings.Join(d.Options, " / "))
		if d.AnswerIndex >= 0 && d.AnswerIndex < len(d.Options) {
			fmt.Fprintf(&b, "- 正确答案：%s\n", d.Options[d.AnswerIndex])
		}
		if st.SelectedIndex >= 0 && st.SelectedIndex < len(d.Options) {
			fmt.Fprintf(&b, "- 用户选择：%s\n", d.Options[st.SelectedIndex])
		}
		if d.Explanation != "" {
			fmt.Fprintf(&b, "- 解析：%s\n", d.Explanation)
		}
	default:
		fmt.Fprintf(&b, "- 卡片：%s（%s）\n", q.Title, q.Summary)
	}
	b.WriteString("）\n")
	return b.String()
}
