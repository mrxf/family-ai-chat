package chat

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/store"
)

// ---- context building ----

const basePrompt = `你是「小暖」，一位耐心、温暖、可靠的 AI 助手，正在为一个家庭的成员提供帮助。
- 用简体中文回答（除非用户使用其他语言）。先给结论，再给必要的解释；语言通俗易懂，避免堆砌术语。
- 回答长度与问题匹配：简单问题简短回答，复杂问题再分点展开。适度使用 Markdown（小标题、列表、加粗），不要过度排版。
- 涉及健康、用药、法律、投资等问题时，给出有用的信息，同时提醒在必要时咨询医生或专业人士。
- 如果用户描述的情况疑似电信诈骗（如陌生人要求转账、索要验证码、冒充公检法或客服、高回报投资等），要明确、直接地提醒用户注意防骗。
- 不确定的事情就说不确定，不要编造。`

const privatePrompt = `

# 隐私对话模式
本次对话为「隐私对话」，对话结束后记录与图片会自动清除，系统不会读取也不会保存任何跨对话记忆。如果用户请你在以后的对话里「记住」某件事，请温和说明当前是隐私对话、关闭后不会留存；需要长期记住的内容可以退出隐私对话后再说，或在「设置 → 小暖记住的事」里手动添加。`

func (s *Service) systemPrompt(ctx context.Context, user *store.User, conv *store.Conversation, canDraw bool, extra ...string) string {
	return s.systemPromptWithTools(ctx, user, conv, toolSet{canDraw: canDraw}, extra...)
}

// systemPromptWithTools assembles the system prompt. extra[0] is core memory (non-private only);
// extra[1] is the optional conversation rolling summary section (# 本次对话的前情提要).
func (s *Service) systemPromptWithTools(ctx context.Context, user *store.User, conv *store.Conversation, ts toolSet, extra ...string) string {
	var b strings.Builder
	b.WriteString(basePrompt)
	b.WriteString(s.drawPrompt(ctx, ts.canDraw))
	b.WriteString(s.searchPrompt(ctx, ts))
	if ts.canRecall && !conv.Private {
		b.WriteString(memoryToolPrompt)
	}
	if ts.skillCatalog != "" && !conv.Private {
		b.WriteString(ts.skillCatalog)
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && strings.TrimSpace(a.SystemPrompt) != "" {
			fmt.Fprintf(&b, "\n\n# 本次对话的角色设定（%s）\n%s", a.Name, a.SystemPrompt)
		}
	}
	now := time.Now().In(shanghai)
	fmt.Fprintf(&b, "\n\n当前日期：%s（%s）。", now.Format("2006年1月2日"), weekdays[now.Weekday()])
	if conv.Private {
		b.WriteString(privatePrompt)
	} else {
		if user.DisplayName != "" {
			fmt.Fprintf(&b, "\n正在和你对话的用户称呼是「%s」。", user.DisplayName)
		}
		if len(extra) > 0 {
			b.WriteString(extra[0])
		}
	}
	if len(extra) > 1 && extra[1] != "" {
		b.WriteString(extra[1])
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
	tools             []llm.Tool
	core              string
	summaryText       string
	foldedBeforeID    string
	recalled          bool
	uncompactedTokens int
	compactAfterTurn  bool
	budgets           contextBudgets
}

type buildOpts struct {
	autoEscalated bool // Auto mode just switched up from a lighter model to T4/T5 flagship
}

func (s *Service) buildRequest(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, canDraw bool) (chatRequest, error) {
	return s.buildRequestWithTools(ctx, user, conv, asstID, model, toolSet{canDraw: canDraw})
}

func (s *Service) buildRequestWithTools(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, ts toolSet) (chatRequest, error) {
	return s.buildRequestWithOpts(ctx, user, conv, asstID, model, ts, buildOpts{})
}

func (s *Service) buildRequestWithOpts(ctx context.Context, user *store.User, conv *store.Conversation, asstID string, model *store.Model, ts toolSet, opts buildOpts) (chatRequest, error) {
	convSummary, summaryCursor, _ := s.Store.GetConversationSummary(ctx, conv.ID)
	history, hasOlderInDB, err := s.Store.ListMessagesAfter(ctx, conv.ID, summaryCursor, asstID, historyFetchLimit)
	if err != nil {
		return chatRequest{}, err
	}
	lastUser := -1
	var recentUsers []string
	uncompactedTokens := 0
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if !(m.Role == "assistant" && len(m.Attachments) == 0 && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "")) {
			uncompactedTokens += estimateTokens(m.Content) + len(m.Attachments)*imageTokens
		}
		if m.Role == "user" {
			if lastUser < 0 {
				lastUser = i
			}
			if txt := strings.TrimSpace(m.Content); txt != "" && len(recentUsers) < 3 {
				recentUsers = append([]string{txt}, recentUsers...)
			}
		}
	}
	mem := memory.Context{}
	if s.Memory != nil && !conv.Private {
		mem = s.Memory.InjectTurn(ctx, user, memory.InjectRequest{
			ConversationID:    conv.ID,
			ConversationTitle: conv.Title,
			AssistantID:       conv.AssistantID,
			UserMessages:      recentUsers,
		})
	}

	summaryText := strings.TrimSpace(convSummary)
	if summaryText == "" && hasOlderInDB && !conv.Private {
		if eps, eErr := s.Store.ConversationEpisodes(ctx, conv.ID, user.ID); eErr == nil && len(eps) > 0 {
			summaryText = strings.Join(eps, "\n- ")
			if len(eps) > 1 {
				summaryText = "- " + summaryText
			}
		}
	}
	if hasOlderInDB && ts.allowTools && !ts.canReadEarlier {
		ts.canReadEarlier = true
		ts.tools = append(append([]llm.Tool(nil), ts.tools...), readEarlierTool)
	}

	summarySection := formatSummarySection(summaryText, hasOlderInDB, ts.canReadEarlier)
	sys := s.systemPromptWithTools(ctx, user, conv, ts, mem.Core, summarySection)
	note := "（系统附注，不是用户说的话，回复时不要写出这种附注：现在是 " + time.Now().In(shanghai).Format("15:04") + "。"
	if !conv.Private {
		if schedBrief := s.UpcomingScheduleBrief(ctx, user.ID, time.Now()); schedBrief != "" {
			note += schedBrief
		}
		if ts.leaseNote != "" {
			note += ts.leaseNote
		}
	}
	if mem.Recall != "" {
		note += "可能相关的往事（请留意每条前的时间跨度；较早的短期症状、情绪或临时安排通常已结束，仅作背景参考——除非用户本轮主动提起，不要把旧事当成正在发生的新事追问或续聊）：" + mem.Recall
	}
	note += "）\n"

	rawWindow := model.ContextWindow
	if rawWindow <= 0 {
		rawWindow = 128000
	}
	reserve := model.MaxOutput
	if reserve <= 0 {
		reserve = 8192
	}
	hardBudget := rawWindow - reserve - estimateTokens(sys)
	budgets := computeContextBudgets(hardBudget)

	var num map[string]int
	if ts.canDraw {
		num = map[string]int{}
		if refs, rErr := s.Store.ListConversationImageRefs(ctx, conv.ID, asstID, 500); rErr == nil {
			for i, a := range refs {
				num[a.FileID] = i + 1
			}
		}
	}

	type pickedItem struct {
		lm   llm.Message
		id   string
		cost int
	}
	var picked []pickedItem
	imagesLeft := maxImageMessages
	totalCost := 0
	coldCost := 0
	coldCount := 0
	inColdPast := false
	userTurnsPicked := 0
	droppedInSlice := false
	foldedByCold := false
	var prevCreatedAt int64

	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if prevCreatedAt > 0 && m.CreatedAt > 0 && prevCreatedAt-m.CreatedAt >= ColdResumeIdle.Milliseconds() {
			inColdPast = true
		}
		if m.CreatedAt > 0 {
			prevCreatedAt = m.CreatedAt
		}

		drew := m.Role == "assistant" && len(m.Attachments) > 0
		hasComps := m.Role == "assistant" && len(m.Components) > 0
		if m.Role == "assistant" && !drew && !hasComps && (m.Status == store.StatusError || strings.TrimSpace(m.Content) == "") {
			continue
		}
		lm := llm.Message{Role: m.Role, Content: m.Content}
		if m.Role == "user" && m.QuotedComponent != nil {
			lm.Content = quotedComponentPromptNote(m.QuotedComponent) + lm.Content
		}
		if hasComps {
			lm.Content = componentNoteWithMessageID(m.ID, m.Components) + lm.Content
		}
		if drew {
			lm.Content = drawnNote(m.Attachments, num) + lm.Content
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
				text := lm.Content
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
				lm.Content = fmt.Sprintf("[用户发送了 %s，当前已不可见]\n%s", n, lm.Content)
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

		if len(picked) > 0 {
			if totalCost+cost > budgets.hardBudget {
				droppedInSlice = true
				break
			}
			if inColdPast && ((coldCost+cost > budgets.coldTail && coldCount >= 2) || coldCount >= coldResumeTailMessages) {
				droppedInSlice = true
				foldedByCold = true
				break
			}
			if opts.autoEscalated && userTurnsPicked >= 1 && totalCost+cost > budgets.lowWater {
				droppedInSlice = true
				break
			}
			if !inColdPast && userTurnsPicked >= minProtectedUserTurns && totalCost+cost > budgets.activeCap {
				droppedInSlice = true
				break
			}
		}

		totalCost += cost
		if inColdPast {
			coldCost += cost
			coldCount++
		}
		if m.Role == "user" {
			userTurnsPicked++
		}
		picked = append(picked, pickedItem{lm: lm, id: m.ID, cost: cost})
	}
	for len(picked) > 0 && picked[len(picked)-1].lm.Role == "assistant" {
		totalCost -= picked[len(picked)-1].cost
		picked = picked[:len(picked)-1]
	}
	if len(picked) == 0 {
		return chatRequest{}, ErrEmptyMessage
	}

	foldedEarlier := hasOlderInDB || droppedInSlice
	foldedBeforeID := ""
	if foldedEarlier {
		foldedBeforeID = picked[len(picked)-1].id
		if summaryText == "" && !conv.Private {
			if eps, eErr := s.Store.ConversationEpisodes(ctx, conv.ID, user.ID); eErr == nil && len(eps) > 0 {
				summaryText = strings.Join(eps, "\n- ")
				if len(eps) > 1 {
					summaryText = "- " + summaryText
				}
			}
		}
		if ts.allowTools && !ts.canReadEarlier {
			ts.canReadEarlier = true
			ts.tools = append(append([]llm.Tool(nil), ts.tools...), readEarlierTool)
		}
		if updatedSection := formatSummarySection(summaryText, true, ts.canReadEarlier); updatedSection != summarySection {
			sys = s.systemPromptWithTools(ctx, user, conv, ts, mem.Core, updatedSection)
			newHard := rawWindow - reserve - estimateTokens(sys)
			for len(picked) > 1 && totalCost > newHard {
				totalCost -= picked[len(picked)-1].cost
				picked = picked[:len(picked)-1]
				for len(picked) > 0 && picked[len(picked)-1].lm.Role == "assistant" {
					totalCost -= picked[len(picked)-1].cost
					picked = picked[:len(picked)-1]
				}
			}
			if len(picked) == 0 {
				return chatRequest{}, ErrEmptyMessage
			}
			foldedBeforeID = picked[len(picked)-1].id
		}
	}

	msgs := make([]llm.Message, 0, len(picked)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: sys})
	for i := len(picked) - 1; i >= 0; i-- {
		msgs = append(msgs, picked[i].lm)
	}
	req := chatRequest{
		Request:           llm.Request{Model: model.UpstreamModel, Messages: msgs, MaxTokens: model.MaxOutput},
		tools:             ts.tools,
		core:              mem.Core,
		summaryText:       summaryText,
		foldedBeforeID:    foldedBeforeID,
		recalled:          mem.Recall != "",
		uncompactedTokens: uncompactedTokens,
		compactAfterTurn:  foldedByCold || (summaryCursor == "" && hasOlderInDB),
		budgets:           budgets,
	}
	if conv.AssistantID != nil {
		if a, err := s.Store.GetAssistant(ctx, *conv.AssistantID, user.ID); err == nil && a.Temperature != nil {
			req.Temperature = a.Temperature
		}
	}
	return req, nil
}

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
