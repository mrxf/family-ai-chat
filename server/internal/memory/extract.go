package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const extractionPrompt = `你是「小暖」的记忆整理员。小暖是家人用的 AI 助手。你的工作是从一段对话里，找出值得长期记住的、关于用户本人和用户家人的事实，并维护一份记忆清单。

输出严格的 JSON，不要任何解释：
{"actions":[...],"episodes":[{"content":"...","keywords":"..."}]}

actions 里每一项是下面三种之一：
- {"op":"add","category":"…","content":"…","keywords":"…","valid_until":"YYYY-MM-DD 或省略"}
- {"op":"update","ref":编号,"content":"…","keywords":"…","category":"可省略","valid_until":"可省略"} —— 已有记忆变了（换药、搬家、改主意、病情变化），用新内容替换编号那条
- {"op":"remove","ref":编号} —— 已有记忆不再成立（事情办完了、病好了、用户说不对、用户要求忘掉）

category 只能是：
- profile：用户的基本情况（称呼、年龄段、职业或退休、住在哪个城市、生活习惯）
- health：健康与用药，用户本人和家人的都算（疾病、过敏、正在吃的药和剂量、手术、体检的关键异常），例如「用户的父亲有高血压，每天早上吃氨氯地平 5mg」
- family：家人与宠物的基本情况（家里有谁、年龄、生日、在哪工作上学、住在哪、养了什么宠物及年龄品种），人的健康和用药放 health
- preference：喜好和习惯（口味、忌口、爱好、希望小暖怎么回答，比如「说简单点」「字少点」）
- ongoing：正在进行或近期持续未解决的事（准备旅行、装修、找工作、备考，以及用户/家人近期的病痛恢复、家里宠物当前的生病/瘙痒/护理困扰、近期正在纠结处理的问题）。能估计结束日期的写 valid_until；拿不准何时结束的短期困扰写距今天约 21 天后的 YYYY-MM-DD

写 content 和 keywords 的规则：
- 一条只写一件事，用第三人称「用户……」，不超过 60 字，脱离上下文也能看懂（写全名称，不写「他」「这个药」「它」）。
- 写上具体年月，严禁裸写「最近」「近期」「这几天」「正在」等模糊当下词（否则一个月后再看会误当成眼下刚发生的事）：对话里说「今天」「上周」「最近」时，必须按消息日期换成具体年月（例如写成「2026年11月用户准备看房」，不要写「用户最近在看房」）。
- keywords 写 4–8 个检索词，空格分隔：必须包含日常口语说法、俗称、同义词、上下位词（例如写「柯基」配「小狗 狗狗 宠物 犬」；写「瘙痒」配「痒 痒痒 抓挠 皮肤病」；写「氨氯地平」配「降压药 高血压 血压」）。

最重要的规则：宁可漏记，不要记错。
- 只记用户明确说出的、关于自己或自己家人/宠物的事实。小暖说的话、小暖的建议都不是事实。
- 不记：替别人打听的（「帮邻居问问糖尿病怎么吃」）、假设的、开玩笑的、单纯问知识的、一次性的小事（「今天中午吃了面」）。
- 不记任何密码、验证码、身份证号、银行卡号、详细门牌地址。
- 拿不准是不是用户本人的事，就不记。
- 和已有记忆说的是同一件事：内容没变就什么都不做，变了就 update，不要重复 add。
- 用户说「忘掉」「别记」某事：对相关编号 remove，并且不要把这件事再写进任何记忆或 episodes。

` + privateTopics + `
整段对话都是这类内容时，输出 {"actions":[],"episodes":[]}；只有一部分是时，只记其余部分，episodes 里也不要提到这些内容。

episodes：只概括「未来几周可能会再次提起或后续追问」的个人/家庭事件、健康咨询、决策纠结或具体计划。不要每聊一次都记流水账！
- 下面这些对话一律输出空数组 "episodes":[]（不生成 episode）：
  1. 闲聊寒暄、讲笑话、讲故事、单纯讨论公共新闻或历史科普常识；
  2. 一次性的工具型/常识型问答（如查菜谱做法、翻译润色、改文案、写代码/查报错、问软件怎么操作、算账换算、单纯画一张与个人近况无关的图）；
  3. 只是问小暖记不记得某事、复述或核对已有记忆。
- 确实值得留档时通常写 1 条；只有同一段对话里聊了完全不相关的多个独立个人话题时，才拆成 2–3 条（最多 3 条），避免把无关话题塞进同一条。
- 每条 content 不超过 110 字，只写聊的内容、用户顾虑和结论，不要写日期前缀，不要写「最近」「近期」等模糊时间词，也不要写「已记录」「未记录」「此前已记下」这类关于记忆整理本身的话。
- 每条必须配 keywords（4–10 个空格分隔的口语词、俗称、同义词、上下位词，例如「小狗 狗狗 柯基 宠物 痒 痒痒 抓挠 皮肤 雨后 药浴 宠物医院」）。
- 如果下面给了这个对话已有的概括，在它的基础上增补更新，不要丢掉原来的要点。

没有要改的事实就输出 {"actions":[],"episodes":[...]}。`

type Action struct {
	Op         string  `json:"op"`
	Ref        int     `json:"ref"`
	Category   string  `json:"category"`
	Content    string  `json:"content"`
	Keywords   string  `json:"keywords"`
	ValidUntil *string `json:"valid_until"`
}

type EpisodeDraft struct {
	Content  string `json:"content"`
	Keywords string `json:"keywords"`
}

type Extraction struct {
	Actions  []Action
	Episode  string         // first episode summary (kept for compatibility)
	Episodes []EpisodeDraft // 0..3 topic-pure episode summaries with keywords
}

// privateTopics are never remembered automatically, whatever the user says.
// Memories are injected into every chat and listed on a phone the family
// shares, so these would surface at the wrong moment or to the wrong person;
// much of it is also idle curiosity or bragging rather than fact. Someone who
// really wants one kept can still add it by hand.
const privateTopics = `下面这些内容一律不记（用户说「记住」也不记）：
- 性和亲密关系：性行为、性偏好和性幻想、约炮、炮友、暧昧、婚外情、前任之间的私密往事、色情内容。
- 性病、避孕、流产等性与生殖方面的隐私。正常的怀孕、生育等家庭喜讯可以记。
- 用户说了不想让家人知道的任何事（隐瞒的行踪、私房钱、秘密）。
- 家庭矛盾：对家人的抱怨和负面评价、婚姻危机、离婚打算。
- 债务和网贷、赌博、违法行为、官司纠纷。
- 吹牛、口嗨、夸张或猎奇的说法：像在说大话、找刺激或满足好奇心的，一律当成没说过。`

var ErrParse = errors.New("invalid extraction JSON")

func Parse(text string, log *slog.Logger) (Extraction, error) {
	first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if first < 0 || last < first {
		return Extraction{}, ErrParse
	}
	var raw struct {
		Actions         []json.RawMessage `json:"actions"`
		Episode         *string           `json:"episode"`
		EpisodeKeywords string            `json:"episode_keywords"`
		Episodes        []json.RawMessage `json:"episodes"`
	}
	if json.Unmarshal([]byte(text[first:last+1]), &raw) != nil || raw.Actions == nil || (raw.Episode == nil && raw.Episodes == nil) {
		return Extraction{}, ErrParse
	}
	out := Extraction{}
	for _, epRaw := range raw.Episodes {
		var d EpisodeDraft
		if json.Unmarshal(epRaw, &d) != nil {
			var s string
			if json.Unmarshal(epRaw, &s) == nil {
				d.Content = s
			}
		}
		d.Content = clip(strings.TrimSpace(d.Content), 120)
		d.Keywords = clip(strings.TrimSpace(d.Keywords), 100)
		if d.Content != "" && len(out.Episodes) < 3 {
			out.Episodes = append(out.Episodes, d)
		}
	}
	if len(out.Episodes) == 0 && raw.Episode != nil {
		ep := clip(strings.TrimSpace(*raw.Episode), 120)
		if ep != "" {
			out.Episodes = []EpisodeDraft{{Content: ep, Keywords: clip(strings.TrimSpace(raw.EpisodeKeywords), 100)}}
		}
	}
	if len(out.Episodes) > 0 {
		out.Episode = out.Episodes[0].Content
	}
	for _, item := range raw.Actions {
		var a Action
		err := json.Unmarshal(item, &a)
		a.Content = clip(strings.TrimSpace(a.Content), 80)
		a.Keywords = clip(strings.TrimSpace(a.Keywords), 100)
		valid := err == nil && ((a.Op == "add" && store.ValidMemoryCategory(a.Category, false) && a.Content != "") || (a.Op == "update" && a.Ref > 0 && a.Content != "" && (a.Category == "" || store.ValidMemoryCategory(a.Category, false))) || (a.Op == "remove" && a.Ref > 0 && a.Category != "episode"))
		if a.ValidUntil != nil && *a.ValidUntil != "" {
			_, err := time.ParseInLocation("2006-01-02", *a.ValidUntil, zone)
			valid = valid && err == nil
		}
		if !valid {
			log.Warn("discard invalid memory action", "op", a.Op)
			continue
		}
		out.Actions = append(out.Actions, a)
	}
	return out, nil
}

func (s *Service) extract(ctx context.Context, u *store.User, conv *store.Conversation, snap store.MemorySnapshot) (Extraction, []*store.Memory, error) {
	ms, err := s.Store.ListMemories(ctx, u.ID)
	if err != nil {
		return Extraction{}, nil, err
	}
	var query strings.Builder
	for _, m := range snap.Messages {
		if m.Role == "user" {
			query.WriteString(m.Content)
			query.WriteByte('\n')
		}
	}
	existing := []*store.Memory{}
	retrieval := []*store.Memory{}
	var priorEpisodes []string
	for _, m := range ms {
		if m.AssistantID != nil {
			continue
		}
		if m.Category == "episode" && m.SourceConvID != nil && *m.SourceConvID == conv.ID {
			// The stored summary starts with its date; apply() adds a fresh one.
			priorEpisodes = append(priorEpisodes, strings.TrimSpace(episodeDate.ReplaceAllString(m.Content, "")))
			continue
		}
		if isCore(m, store.Now()) {
			existing = append(existing, m)
		} else {
			retrieval = append(retrieval, m)
		}
	}
	episode := strings.Join(priorEpisodes, "\n- ")
	if len(priorEpisodes) > 1 {
		episode = "- " + episode
	}
	vector, model := s.queryVector(ctx, query.String(), backgroundTimeout)
	existing = append(existing, rank(query.String(), vector, model, retrieval, 15, false)...)
	contextMsgs := []*store.Message{}
	if snap.Cursor != "" {
		contextMsgs, err = s.Store.ListMessages(ctx, conv.ID, snap.Messages[0].ID, 2)
		if err != nil {
			return Extraction{}, nil, err
		}
	}

	if len(snap.Messages) == 1 && snap.Messages[0].Role == "user" && utf8.RuneCountInString(snap.Messages[0].Content) > 8000 {
		out, err := s.extractLong(ctx, u, existing, episode, contextMsgs, snap.Messages[0])
		return out, existing, err
	}
	contextMsgs = append(contextMsgs, snap.Messages...)
	out, err := s.requestExtraction(ctx, u, existing, episode, contextMsgs)
	return out, existing, err
}

func (s *Service) requestExtraction(ctx context.Context, u *store.User, existing []*store.Memory, episode string, msgs []*store.Message) (Extraction, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "今天日期：%s\n用户称呼：%s\n已有记忆：\n", time.Now().In(zone).Format("2006-01-02"), u.DisplayName)
	for i, m := range existing {
		fmt.Fprintf(&b, "#%d [%s] %s\n", i+1, m.Category, m.Content)
	}
	if episode == "" {
		episode = "（还没有）"
	}
	fmt.Fprintf(&b, "这个对话已有的概括：\n%s\n新对话（开头最多两条为已处理的上下文）：\n", episode)
	for _, m := range msgs {
		who, content := "用户", m.Content
		if m.Role == "assistant" {
			who = "小暖"
			content = clip(content, 500)
		}
		if len(m.Attachments) > 0 {
			content += "（发了一张图片）"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", time.UnixMilli(m.CreatedAt).In(zone).Format("2006-01-02 15:04"), who, content)
	}
	m, c, err := s.Resolve(ctx, "memory")
	if err != nil {
		return Extraction{}, err
	}
	text, _, err := c.Complete(ctx, llm.Request{Model: m.UpstreamModel, ReasoningEffort: m.DefaultEffort, Messages: []llm.Message{{Role: "system", Content: extractionPrompt}, {Role: "user", Content: b.String()}}})
	if err != nil {
		return Extraction{}, err
	}
	return Parse(text, s.Log)
}

// A single pasted message can exceed a segment. Maintain a draft between its
// chunks, then commit all changes against the original, unmodified snapshot.
func (s *Service) extractLong(ctx context.Context, u *store.User, existing []*store.Memory, episode string, prior []*store.Message, msg *store.Message) (Extraction, error) {
	type draft struct {
		m               store.Memory
		original        int
		active, changed bool
		until           *string
	}
	drafts := []*draft{}
	for i, m := range existing {
		drafts = append(drafts, &draft{m: *m, original: i + 1, active: true})
	}
	result := Extraction{}
	rs := []rune(msg.Content)
	for start := 0; start < len(rs); start += 7000 {
		refs := []*draft{}
		mems := []*store.Memory{}
		for _, d := range drafts {
			if d.active {
				refs = append(refs, d)
				mems = append(mems, &d.m)
			}
		}
		part := *msg
		part.Content = string(rs[start:min(start+7000, len(rs))])
		input := append([]*store.Message{}, prior...)
		input = append(input, &part)
		out, err := s.requestExtraction(ctx, u, mems, episode, input)
		if err != nil {
			return Extraction{}, err
		}
		for _, a := range out.Actions {
			if a.Op == "add" {
				drafts = append(drafts, &draft{m: store.Memory{Category: a.Category, Content: a.Content, Keywords: a.Keywords}, active: true, changed: true, until: a.ValidUntil})
				continue
			}
			if a.Ref < 1 || a.Ref > len(refs) {
				s.Log.Warn("discard unknown memory ref", "ref", a.Ref)
				continue
			}
			d := refs[a.Ref-1]
			if !d.active || (d.m.Category == "episode" && a.Op != "remove") {
				continue
			}
			d.changed = true
			if a.Op == "remove" {
				d.active = false
				continue
			}
			d.m.Content, d.m.Keywords = a.Content, a.Keywords
			if a.Category != "" {
				d.m.Category = a.Category
			}
			if a.ValidUntil != nil {
				d.until = a.ValidUntil
			}
		}
		if len(out.Episodes) > 0 {
			result.Episodes = out.Episodes
			result.Episode = out.Episodes[0].Content
			parts := make([]string, 0, len(out.Episodes))
			for _, ep := range out.Episodes {
				parts = append(parts, ep.Content)
			}
			episode = strings.Join(parts, "\n- ")
		} else if out.Episode != "" {
			episode = out.Episode
			result.Episode = out.Episode
			result.Episodes = []EpisodeDraft{{Content: out.Episode}}
		}
		// The preceding chunk is context only; keep it compact for the next request.
		prev := part
		prev.Content = clip(prev.Content, 500)
		prior = []*store.Message{&prev}
	}
	for _, d := range drafts {
		if !d.changed {
			continue
		}
		if !d.active {
			if d.original > 0 {
				result.Actions = append(result.Actions, Action{Op: "remove", Ref: d.original})
			}
			continue
		}
		op := "add"
		if d.original > 0 {
			op = "update"
		}
		result.Actions = append(result.Actions, Action{Op: op, Ref: d.original, Category: d.m.Category, Content: d.m.Content, Keywords: d.m.Keywords, ValidUntil: d.until})
	}
	return result, nil
}

func (s *Service) apply(ctx context.Context, snap store.MemorySnapshot, out Extraction, existing []*store.Memory) error {
	eventMs := store.Now()
	if len(snap.Messages) > 0 && snap.Messages[len(snap.Messages)-1].CreatedAt > 0 {
		eventMs = snap.Messages[len(snap.Messages)-1].CreatedAt
	}
	evDay := time.UnixMilli(eventMs).In(zone)
	evDayStart := time.Date(evDay.Year(), evDay.Month(), evDay.Day(), 0, 0, 0, 0, zone)
	defaultUntil := evDayStart.AddDate(0, 0, DefaultOngoingDays+1).UnixMilli() - 1
	maxUntil := evDayStart.AddDate(0, 0, MaxOngoingDays+1).UnixMilli() - 1

	changes := []store.MemoryChange{}
	for _, a := range out.Actions {
		var old *store.Memory
		if a.Op != "add" {
			if a.Ref < 1 || a.Ref > len(existing) {
				s.Log.Warn("discard unknown memory ref", "ref", a.Ref)
				continue
			}
			old = existing[a.Ref-1]
			if old.Category == "episode" && a.Op != "remove" {
				continue
			}
		}
		ch := store.MemoryChange{Op: a.Op}
		if old != nil {
			ch.RefID = old.ID
		}
		if a.Op != "remove" {
			m := &store.Memory{
				UserID:          snap.UserID,
				Category:        a.Category,
				Content:         a.Content,
				Keywords:        a.Keywords,
				KeywordsChecked: true,
				Source:          "auto",
				SourceConvID:    &snap.ConversationID,
				Pinned:          true,
				EventAt:         eventMs,
			}
			if old != nil {
				m.Pinned = old.Pinned
				m.ValidUntil = old.ValidUntil
				if m.Category == "" {
					m.Category = old.Category
				}
			}
			if a.ValidUntil != nil {
				m.ValidUntil = nil
				if *a.ValidUntil != "" {
					d, _ := time.ParseInLocation("2006-01-02", *a.ValidUntil, zone)
					v := min(d.AddDate(0, 0, 1).UnixMilli()-1, maxUntil)
					m.ValidUntil = &v
				}
			}
			if m.Category == "ongoing" {
				if m.ValidUntil == nil || (a.ValidUntil == nil && *m.ValidUntil < eventMs) {
					v := defaultUntil
					m.ValidUntil = &v
				} else if *m.ValidUntil > maxUntil {
					v := maxUntil
					m.ValidUntil = &v
				}
			} else {
				m.ValidUntil = nil
			}
			ch.Memory = m
		}
		changes = append(changes, ch)
	}

	drafts := out.Episodes
	if len(drafts) == 0 && strings.TrimSpace(out.Episode) != "" {
		drafts = []EpisodeDraft{{Content: out.Episode}}
	}
	// Preserve original date & event_at for unchanged episodes when an old thread is reopened.
	priorByKey := map[string]*store.Memory{}
	if len(drafts) > 0 {
		if allMems, err := s.Store.ListMemories(ctx, snap.UserID); err == nil {
			for _, pm := range allMems {
				if pm.Category == "episode" && pm.SourceConvID != nil && *pm.SourceConvID == snap.ConversationID {
					body := strings.TrimSpace(episodeDate.ReplaceAllString(strings.TrimSpace(pm.Content), ""))
					if k := store.MemoryKey(body); k != "" {
						priorByKey[k] = pm
					}
				}
			}
		}
	}
	datePrefix := evDay.Format("2006-01-02")
	var specs []store.EpisodeSpec
	for _, d := range drafts {
		// The model sometimes copies the message dates into its summary.
		ep := strings.TrimSpace(episodeDate.ReplaceAllString(strings.TrimSpace(d.Content), ""))
		if ep == "" {
			continue
		}
		if prev, ok := priorByKey[store.MemoryKey(ep)]; ok {
			specs = append(specs, store.EpisodeSpec{
				Content:  prev.Content,
				Keywords: strings.TrimSpace(d.Keywords),
				EventAt:  memoryEventAt(prev),
			})
			continue
		}
		specs = append(specs, store.EpisodeSpec{
			Content:  datePrefix + " " + ep,
			Keywords: strings.TrimSpace(d.Keywords),
			EventAt:  eventMs,
		})
	}
	return s.Store.ApplyMemoryExtractionEpisodes(ctx, snap, changes, specs)
}

// segment preserves message boundaries; extractLong splits an oversized message.
func segment(ms []*store.Message) []*store.Message {
	n := 0
	for i, m := range ms {
		size := utf8.RuneCountInString(m.Content)
		if m.Role == "assistant" {
			size = min(size, 500)
		}
		if i > 0 && n+size > 8000 {
			return ms[:i]
		}
		n += size
	}
	return ms
}
