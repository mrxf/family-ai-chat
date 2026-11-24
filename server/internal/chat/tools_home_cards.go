package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"nuan/internal/store"
	"strconv"
	"strings"
)

func shortHomeRef(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
func homeCardCandidates(pins []*store.PinnedComponent) string {
	var b strings.Builder
	for _, p := range pins {
		title, _ := summarizeComponentForQuote(p.Component)
		fmt.Fprintf(&b, "%s [%s] message_id=%s component_id=%s\n", title, shortHomeRef(p.ID), p.MessageID, p.ComponentID)
	}
	return b.String()
}

func (s *Service) defaultPinTarget(ctx context.Context, t *toolTurn) (string, string) {
	if t.lastCreatedMsgID != "" && t.lastCreatedCompID != "" {
		return t.lastCreatedMsgID, t.lastCreatedCompID
	}
	if q := t.quotedComponent; q != nil {
		return q.MessageID, q.ComponentID
	}
	before := ""
	for {
		messages, err := s.Store.ListMessages(ctx, t.convID, before, 100)
		if err != nil || len(messages) == 0 {
			break
		}
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			if m.Role != "assistant" {
				continue
			}
			for j := len(m.Components) - 1; j >= 0; j-- {
				c := m.Components[j]
				if c.Kind != "ask_user" && c.Kind != "app_action" {
					return m.ID, c.ID
				}
			}
		}
		before = messages[0].ID
		if len(messages) < 100 {
			break
		}
	}

	return "", ""
}

func (s *Service) execListHomeCards(ctx context.Context, t *toolTurn, raw string) string {
	if t.private {
		return "隐私对话不支持首页管理。"
	}
	var args struct {
		Keyword string `json:"keyword"`
	}
	_ = json.Unmarshal([]byte(raw), &args)
	pins, err := s.Store.ListPinnedComponents(ctx, t.user.ID)
	if err != nil {
		return "读取首页失败：" + err.Error()
	}
	matches := []*store.PinnedComponent{}
	for _, p := range pins {
		title, summary := summarizeComponentForQuote(p.Component)
		if strings.Contains(strings.ToLower(title+" "+summary), strings.ToLower(strings.TrimSpace(args.Keyword))) {
			matches = append(matches, p)
		}
	}
	status := "猜你想问：当前客户端未提供状态"
	if t.clientCtx != nil {
		status = fmt.Sprintf("猜你想问：显示=%t", t.clientCtx.SuggestionsPinned)
	}
	t.run.addDoneStep("component", "已查看首页卡片", "")
	return status + "\n" + homeCardCandidates(matches)
}

func (s *Service) execReorderHomeCards(ctx context.Context, t *toolTurn, raw string) string {
	if t.private {
		return "隐私对话不支持首页管理。"
	}
	var args struct {
		Ref      string `json:"component_ref"`
		Position any    `json:"position"`
	}
	if json.Unmarshal([]byte(raw), &args) != nil || strings.TrimSpace(args.Ref) == "" {
		return "请指定首页卡片和位置。"
	}
	pins, err := s.Store.ListPinnedComponents(ctx, t.user.ID)
	if err != nil {
		return "读取首页失败：" + err.Error()
	}
	index := -1
	for i, p := range pins {
		if p.ID == args.Ref || shortHomeRef(p.ID) == args.Ref || p.ComponentID == args.Ref {
			if index >= 0 {
				return "标识匹配多张卡片，请使用完整ID。"
			}
			index = i
		}
	}
	if index < 0 {
		return "未找到首页卡片：\n" + homeCardCandidates(pins)
	}
	pos := -1
	switch v := args.Position.(type) {
	case string:
		switch v {
		case "first":
			pos = 0
		case "last":
			pos = len(pins) - 1
		default:
			n, e := strconv.Atoi(v)
			if e == nil {
				pos = n - 1
			}
		}
	case float64:
		if v == float64(int(v)) {
			pos = int(v) - 1
		}
	}
	if pos < 0 || pos >= len(pins) {
		return fmt.Sprintf("位置应为first、last或1到%d。", len(pins))
	}
	ids := []string{}
	for i, p := range pins {
		if i != index {
			ids = append(ids, p.ID)
		}
	}
	ids = append(ids, "")
	copy(ids[pos+1:], ids[pos:])
	ids[pos] = pins[index].ID
	if err := s.Store.ReorderPinnedComponents(ctx, t.user.ID, ids); err != nil {
		return "排序失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已调整首页卡片顺序", "")
	return "已调整首页卡片顺序。"
}

func (s *Service) execPinComponent(ctx context.Context, t *toolTurn, rawArgs string) string {
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
	if msgID == "" && compID == "" {
		msgID, compID = s.defaultPinTarget(ctx, t)
	}
	if msgID == "" || compID == "" {
		return "没找到可以固定的卡片，请先让用户指明是哪一张。"
	}
	pins, listErr := s.Store.ListPinnedComponents(ctx, t.user.ID)
	if listErr != nil {
		return "读取首页失败：" + listErr.Error()
	}
	for _, p := range pins {
		if p.MessageID == msgID && p.ComponentID == compID {
			return "已经在首页了。"
		}
	}
	pin, err := s.Store.PinComponent(ctx, t.user.ID, msgID, compID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPinLimit):
			return "首页最多固定 20 张卡片，请先取消固定一些不常用的卡片。"
		case errors.Is(err, store.ErrComponentNotPinnable):
			return "这张卡片不支持固定到首页。"
		case errors.Is(err, store.ErrNotFound):
			return "未找到指定卡片，请先调用 list_components 确认 ID。"
		default:
			return "固定卡片失败：" + err.Error()
		}
	}
	t.lastPinnedMsgID = msgID
	t.lastPinnedCompID = compID
	title, _ := summarizeComponentForQuote(pin.Component)
	t.run.addDoneStep("component", "已固定到首页："+clipRunes(title, 20), "")
	return fmt.Sprintf("已将卡片「%s」固定到首页看板（内部标识 message_id=%s component_id=%s，勿向用户输出）。", title, msgID, compID)
}

func (s *Service) execUnpinComponent(ctx context.Context, t *toolTurn, rawArgs string) string {
	if msg := s.checkComponentManageGuard(t); msg != "" {
		return msg
	}
	var args struct {
		Keyword     string `json:"keyword"`
		MessageID   string `json:"message_id"`
		ComponentID string `json:"component_id"`
	}
	_ = json.Unmarshal([]byte(rawArgs), &args)
	msgID := strings.TrimSpace(args.MessageID)
	compID := strings.TrimSpace(args.ComponentID)
	if strings.TrimSpace(args.Keyword) != "" {
		pins, err := s.Store.ListPinnedComponents(ctx, t.user.ID)
		if err != nil {
			return "读取首页失败：" + err.Error()
		}
		matches := []*store.PinnedComponent{}
		titles := []string{}
		for _, p := range pins {
			title, summary := summarizeComponentForQuote(p.Component)
			titles = append(titles, title)
			if strings.Contains(strings.ToLower(title+" "+summary), strings.ToLower(strings.TrimSpace(args.Keyword))) {
				matches = append(matches, p)
			}
		}
		if len(matches) == 0 {
			return "没有匹配卡片。当前首页：" + strings.Join(titles, "、")
		}
		if len(matches) > 1 {
			return "多张卡片匹配，请调用ask_user_question让用户选择：\n" + homeCardCandidates(matches)
		}
		msgID = matches[0].MessageID
		compID = matches[0].ComponentID
	}
	if msgID == "" || compID == "" {
		return "参数错误：需要提供 message_id 和 component_id。"
	}
	if err := s.Store.UnpinComponent(ctx, t.user.ID, msgID, compID); err != nil {
		return "取消固定失败：" + err.Error()
	}
	t.run.addDoneStep("component", "已从首页取消固定卡片", "")
	return "已将该卡片从首页看板取消固定。"
}
