package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/skills"
	"nuan/internal/store"
	"strings"
	"sync"
	"time"
)

// runTools executes the calls of one round (pictures are drawn in parallel)
// and returns the text handed back to the model for each call, in order, plus
// whether the round only drew pictures (so the wrap-up round needs no tools).
func (s *Service) runTools(ctx context.Context, t *toolTurn, calls []llm.ToolCall) ([]string, bool) {
	results := make([]string, len(calls))
	var jobs []drawJob
	im, ip := s.imageModel(ctx)
	var refs []store.Attachment
	refsLoaded := false
	usedReadTool := false
	askedUser := false

	for i, call := range calls {
		switch call.Name {
		case recallMemoryTool.Name:
			usedReadTool = true
			if t.private || s.Memory == nil {
				results[i] = "当前是隐私对话或未开启记忆功能，无法查阅记忆。"
				continue
			}
			if t.memCalls >= maxMemoryCalls {
				results[i] = "本轮对话查阅记忆次数已达上限，请根据已有信息回答。"
				continue
			}
			t.memCalls++
			var args memory.RecallRequest
			if strings.TrimSpace(call.Arguments) != "" && json.Unmarshal([]byte(call.Arguments), &args) != nil {
				// Tolerate models sending {"query":"..."} as a single string.
				var alt struct {
					Query    string `json:"query"`
					Category string `json:"category"`
					Mode     string `json:"mode"`
					Limit    int    `json:"limit"`
				}
				if json.Unmarshal([]byte(call.Arguments), &alt) == nil {
					if strings.TrimSpace(alt.Query) != "" {
						args.Queries = []string{alt.Query}
					}
					args.Category, args.Mode, args.Limit = alt.Category, alt.Mode, alt.Limit
				}
			} else if len(args.Queries) == 0 && strings.TrimSpace(call.Arguments) != "" {
				var alt struct {
					Query string `json:"query"`
				}
				if json.Unmarshal([]byte(call.Arguments), &alt) == nil && strings.TrimSpace(alt.Query) != "" {
					args.Queries = []string{alt.Query}
				}
			}
			memLabel := "近期记忆"
			if len(args.Queries) > 0 && !strings.EqualFold(strings.TrimSpace(args.Mode), "recent") {
				memLabel = joinShortQueries(args.Queries, 28)
			}
			stepIdx := t.run.startStep("memory", "翻看记忆："+memLabel, "")
			t.run.setActivity("正在翻看记忆…")
			t.postActivity = "正在结合记忆整理回答…"
			results[i] = s.Memory.Recall(ctx, t.user, t.assistantID, t.convID, args)
			t.run.finishStep(stepIdx, "done", "已翻看记忆："+memLabel, "")

		case readConversationTool.Name:
			usedReadTool = true
			if t.private || s.Memory == nil {
				results[i] = "当前是隐私对话或未开启记忆功能，无法读取历史对话。"
				continue
			}
			if t.memCalls >= maxMemoryCalls {
				results[i] = "本轮对话查阅次数已达上限，请根据已有信息回答。"
				continue
			}
			t.memCalls++
			var args struct {
				ConversationID string `json:"conversation_id"`
				ConvID         string `json:"conv_id"`
				Focus          string `json:"focus"`
			}
			_ = json.Unmarshal([]byte(call.Arguments), &args)
			cid := strings.TrimSpace(args.ConversationID)
			if cid == "" {
				cid = strings.TrimSpace(args.ConvID)
			}
			stepIdx := t.run.startStep("memory", "查阅历史对话", "")
			t.run.setActivity("正在查看之前的对话…")
			t.postActivity = "正在结合历史对话整理回答…"
			results[i] = s.Memory.ReadConversation(ctx, t.user, t.assistantID, cid, args.Focus)
			t.run.finishStep(stepIdx, "done", "已查阅历史对话详情", "")

		case readEarlierTool.Name:
			usedReadTool = true
			results[i] = s.execReadEarlierCall(ctx, t, call.Arguments)

		case webSearchTool.Name:
			usedReadTool = true
			results[i] = s.execWebSearchCall(ctx, t, call.Arguments)

		case webFetchTool.Name:
			usedReadTool = true
			results[i] = s.execWebFetchCall(ctx, t, call.Arguments)

		case skills.ReadSkillToolName:
			usedReadTool = true
			out, unlocked := s.execReadSkillCall(t, call.Arguments)
			results[i] = out
			if len(unlocked) > 0 {
				t.usedReadSkill = true
				t.unlockedTools = appendUniqueTools(t.unlockedTools, unlocked...)
				if key := parseReadSkillKey(call.Arguments); key != "" {
					if t.activatedSkills == nil {
						t.activatedSkills = map[string]bool{}
					}
					t.activatedSkills[strings.ToLower(key)] = true
				}
			}

		case imageTool.Name:
			var args struct {
				Prompt string `json:"prompt"`
				Image  string `json:"image"`
				Aspect string `json:"aspect"`
			}
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || strings.TrimSpace(args.Prompt) == "" {
				results[i] = "参数不对：需要 prompt（画面描述）。"
				continue
			}
			if im == nil {
				results[i] = "画图功能现在不可用（管理员关闭了画图模型）。请向用户说明。"
				continue
			}
			if t.images+len(jobs) >= maxImagesPerTurn {
				results[i] = fmt.Sprintf("这条回复最多画 %d 张，这张没有画。", maxImagesPerTurn)
				continue
			}
			j := drawJob{i: i, prompt: clip(strings.TrimSpace(args.Prompt), 4000), size: imageSize(im.UpstreamModel, args.Aspect)}
			if ref := strings.TrimSpace(args.Image); ref != "" {
				if !refsLoaded {
					refs, refsLoaded = s.imageRefs(ctx, t.convID, t.asstID), true
				}
				n := refNumber(ref)
				if n < 1 || n > len(refs) {
					results[i] = fmt.Sprintf("没有找到「%s」：这段对话里的图片编号是 图1 到 图%d。", ref, len(refs))
					if len(refs) == 0 {
						results[i] = "这段对话里还没有图片，没法修改；不填 image 就是画一张新图。"
					}
					continue
				}
				j.source = &refs[n-1]
				j.sourceNo = n
			}
			jobs = append(jobs, j)

		default:
			if s.Skills != nil {
				if owner, ok := s.Skills.OwnerSkill(call.Name, skills.Context{UserID: t.user.ID, ConversationID: t.convID, Private: t.private}); ok {
					if t.private {
						results[i] = "当前是隐私对话模式，为保护隐私不会启用「" + owner.Title + "」相关工具。"
						continue
					}
					for _, candidate := range s.Skills.List(skills.Context{UserID: t.user.ID, ConversationID: t.convID, Private: t.private}) {
						if !t.activeSkills[candidate.Key] {
							continue
						}
						for _, tool := range candidate.Tools {
							if tool.Name == call.Name {
								cp := candidate
								owner = &cp
								break
							}
						}
					}
					if !t.activeSkills[owner.Key] {
						usedReadTool = true
						results[i] = fmt.Sprintf(
							"工具 %q 属于「%s (%s)」技能，当前尚未激活。请先调用 read_skill({\"skill_key\": %q}) 阅读技能指南并解锁对应工具后再操作。",
							call.Name, owner.Title, owner.Key, owner.Key,
						)
						continue
					}
					if t.usedSkillOwners == nil {
						t.usedSkillOwners = map[string]bool{}
					}
					t.usedSkillOwners[owner.Key] = true
					if isAppControlTool(call.Name) {
						if t.appControlCalls >= 6 {
							results[i] = "本轮应用管理操作已达 6 次上限，请根据已有结果回复。"
							continue
						}
						t.appControlCalls++
					}
					if out, handled := s.execAppControlTool(ctx, t, call.Name, call.Arguments); handled {
						usedReadTool = true
						results[i] = out
						continue
					}
					if out, handled := s.execScheduleTool(ctx, t, call.Name, call.Arguments); handled {
						usedReadTool = true
						results[i] = out
						continue
					}
					if out, handled := s.execAutomationTool(ctx, t, call.Name, call.Arguments); handled {
						usedReadTool = true
						results[i] = out
						continue
					}
					if out, handled, asked := s.execComponentOrAskTool(ctx, t, call.Name, call.Arguments); handled {
						if asked {
							askedUser = true
						} else {
							usedReadTool = true
						}
						results[i] = out
						continue
					}
				}
			}
			results[i] = "没有这个工具：" + call.Name
		}
	}

	if len(jobs) == 0 {
		return results, askedUser && !usedReadTool
	}

	// Daily limit: refuse what does not fit, draw the rest.
	allowed, limit := s.reserveImages(ctx, t.user, len(jobs))
	if limit > 0 {
		defer s.releaseImages(t.user.ID, allowed)
	}
	for _, j := range jobs[allowed:] {
		if allowed == 0 {
			results[j.i] = fmt.Sprintf("今天的画图次数已经用完了（每人每天 %d 张），明天再来。请温和地告诉用户，不要假装画好了。", limit)
		} else {
			results[j.i] = fmt.Sprintf("今天的画图次数只够再画 %d 张（每人每天 %d 张），这张没有画。请告诉用户。", allowed, limit)
		}
	}
	jobs = jobs[:allowed]
	if len(jobs) == 0 {
		return results, !usedReadTool
	}

	label := "正在画图…"
	edits := 0
	for _, j := range jobs {
		if j.source != nil {
			edits++
		}
	}
	switch {
	case len(jobs) > 1:
		label = fmt.Sprintf("正在画 %d 张图…", len(jobs))
	case edits == 1:
		label = "正在改图…"
	}
	t.run.setActivity(label)
	defer t.run.setActivity("")

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			att, cost, redrawn, err := s.drawImage(ctx, t.user.ID, t.private, im, ip, j)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					results[j.i] = "用户停止了画图。"
					return
				}
				s.Log.Warn("image generation failed", "message_id", t.asstID, "model", im.UpstreamModel, "edit", j.source != nil, "err", err)
				results[j.i] = "画图失败：" + friendlyImageError(err) + "。请用一两句话告诉用户没画成和原因，可以建议换个说法再试；不要假装已经画好。"
				return
			}
			t.images++
			t.cost += cost
			all := t.run.addAttachment(att)
			pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.Store.SetAttachments(pctx, t.asstID, all); err != nil {
				s.Log.Error("save attachments", "message_id", t.asstID, "err", err)
			}
			if err := s.Store.LogImage(pctx, t.user.ID, im.ID, cost); err != nil {
				s.Log.Error("log image", "message_id", t.asstID, "err", err)
			}
			cancel()
			s.Log.Info("image generated", "message_id", t.asstID, "model", im.UpstreamModel, "edit", j.source != nil, "redrawn", redrawn,
				"ms", time.Since(started).Milliseconds(), "cost", cost)
			switch {
			case redrawn:
				results[j.i] = fmt.Sprintf("画图模型不支持在原图上直接修改，已按图%d 的提示词加上修改要求重新画了一张，并显示给用户了；新图和原图会有些不同，请顺带说明。不要复述提示词，不要输出 JSON 或链接。", j.sourceNo)
			case j.source != nil:
				results[j.i] = "已经在原图基础上改好，新图片显示在聊天界面里给用户看了。请用一两句温暖自然的话告诉用户改好了，可以问要不要再调整；不要复述提示词，不要输出 JSON 或链接。"
			default:
				results[j.i] = "图片已经画好，并已显示在聊天界面里给用户看了。请用一两句温暖自然的话告诉用户画好了，可以简单说说画面或问要不要调整；不要复述提示词，不要输出 JSON 或链接。"
			}
		}()
	}
	wg.Wait()
	return results, !usedReadTool
}
