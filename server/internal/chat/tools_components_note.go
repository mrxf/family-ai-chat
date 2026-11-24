package chat

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"nuan/internal/store"
)

// ---------------- Multi-Turn History Summary Note ----------------

// componentNote builds a concise system note summarizing components rendered in an earlier
// assistant message (including live user interactions like checked Todo items, Quiz choice,
// or submitted AskUser preferences) so follow-up turns stay context-aware without polluting m.Content.
func componentNote(comps []store.MessageComponent) string {
	return componentNoteWithMessageID("", comps)
}

func componentNoteWithMessageID(messageID string, comps []store.MessageComponent) string {
	if len(comps) == 0 {
		return ""
	}
	var parts []string
	for _, c := range comps {
		ref := ""
		if messageID != "" && c.ID != "" && c.Kind != "ask_user" {
			ref = fmt.Sprintf(" [内部ID: message_id=%s, component_id=%s]", messageID, c.ID)
		}
		switch c.Kind {
		case "weather":
			var d weatherCardData
			if json.Unmarshal(c.Data, &d) == nil && d.Location != "" {
				switch {
				case d.Low != nil && d.High != nil:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s %.0f~%.0f℃」%s", d.Location, d.Condition, *d.Low, *d.High, ref))
				case d.Temp != nil:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s %.0f℃」%s", d.Location, d.Condition, *d.Temp, ref))
				default:
					parts = append(parts, fmt.Sprintf("天气卡「%s %s」%s", d.Location, d.Condition, ref))
				}
			}
		case "todo":
			var d todoCardData
			var st todoCardState
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			doneSet := map[string]bool{}
			for _, id := range st.DoneIDs {
				doneSet[id] = true
			}
			var doneNames, todoNames []string
			for _, it := range d.Items {
				if doneSet[it.ID] {
					doneNames = append(doneNames, it.Text)
				} else {
					todoNames = append(todoNames, it.Text)
				}
			}
			summary := fmt.Sprintf("互动清单「%s」（已完成 %d/%d", d.Title, len(doneNames), len(d.Items))
			if len(doneNames) > 0 {
				summary += "，已勾选：" + clipRunes(strings.Join(doneNames, "、"), 80)
			}
			if len(todoNames) > 0 {
				summary += "；未勾选：" + clipRunes(strings.Join(todoNames, "、"), 80)
			}
			summary += "）" + ref
			parts = append(parts, summary)
		case "feed":
			var d feedCardData
			if json.Unmarshal(c.Data, &d) == nil {
				var heads []string
				for i, it := range d.Items {
					if i >= 4 {
						break
					}
					heads = append(heads, clipRunes(it.Title, 24))
				}
				parts = append(parts, fmt.Sprintf("资讯列表「%s」（共 %d 条：%s）%s", d.Title, len(d.Items), strings.Join(heads, " / "), ref))
			}
		case "chart":
			var d chartCardData
			if json.Unmarshal(c.Data, &d) == nil {
				parts = append(parts, fmt.Sprintf("数据图表「%s」(%s, %d项)%s", d.Title, d.ChartType, len(d.Labels), ref))
			}
		case "quiz":
			var d quizCardData
			var st quizCardState
			st.SelectedIndex = -1
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			status := "用户尚未点选"
			if st.SelectedIndex >= 0 && st.SelectedIndex < len(d.Options) {
				verdict := "回答错误"
				if st.SelectedIndex == d.AnswerIndex {
					verdict = "回答正确"
				}
				status = fmt.Sprintf("用户已选「%s」，%s", d.Options[st.SelectedIndex], verdict)
			}
			parts = append(parts, fmt.Sprintf("问答测验卡「%s」（%s）%s", clipRunes(d.Question, 36), status, ref))
		case "metrics":
			var d metricsCardData
			if json.Unmarshal(c.Data, &d) == nil {
				var kvs []string
				for i, m := range d.Metrics {
					if i >= 4 {
						break
					}
					kvs = append(kvs, m.Label+":"+m.Value+m.Unit)
				}
				parts = append(parts, fmt.Sprintf("指标看板「%s」（%s）%s", d.Title, strings.Join(kvs, "，"), ref))
			}
		case "ask_user":
			var d askUserCardData
			var st askUserCardState
			_ = json.Unmarshal(c.Data, &d)
			_ = json.Unmarshal(c.State, &st)
			var qs []string
			for _, q := range d.Questions {
				qs = append(qs, clipRunes(q.Question, 24))
			}
			status := "用户未点选此卡片（若用户已聊起新话题，直接顺应新话题回复，切勿强行追问）"
			if st.Submitted {
				status = "用户已在卡片确认提交"
			} else if st.Dismissed {
				status = "用户已主动跳过关闭该询问卡（请尊重用户自己的话题，不要追问未选问题）"
			}
			parts = append(parts, fmt.Sprintf("交互询问卡（问题：%s；%s）", strings.Join(qs, " / "), status))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "（系统附注，不是对话内容，回复时不要写这种附注，也不要输出内部ID：这条回复里你向用户展示了以下交互卡片——" + strings.Join(parts, "；") + "）\n"
}

// ---------------- Parsing Helpers ----------------

func strVal(vals ...any) string {
	for _, v := range vals {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func anyToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(round2(x), 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func numVal(vals ...any) (float64, bool) {
	for _, v := range vals {
		switch x := v.(type) {
		case float64:
			if !math.IsNaN(x) && !math.IsInf(x, 0) {
				return x, true
			}
		case int:
			return float64(x), true
		case string:
			clean := strings.TrimSpace(x)
			for _, suf := range []string{"℃", "°C", "°", "%"} {
				clean = strings.TrimSuffix(clean, suf)
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(clean), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return f, true
			}
		}
	}
	return 0, false
}

func floatSlice(vals ...any) []float64 {
	for _, v := range vals {
		arr, ok := v.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		out := make([]float64, 0, min(len(arr), 16))
		for _, elem := range arr {
			if len(out) >= 16 {
				break
			}
			if n, ok := numVal(elem); ok {
				out = append(out, round2(n))
			} else {
				out = append(out, 0)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

func stripOptionPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		c := s[0]
		if (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') || (c >= '1' && c <= '6') {
			rest := s[1:]
			for _, sep := range []string{". ", "、", "．", ") ", "）", ".", ":"} {
				if strings.HasPrefix(rest, sep) {
					return strings.TrimSpace(rest[len(sep):])
				}
			}
		}
	}
	return s
}
