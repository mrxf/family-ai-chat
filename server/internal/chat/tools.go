package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const (
	// Up to 4 tool rounds (e.g. recall_memory -> web_search -> web_fetch -> generate_image)
	// followed by a final words-only round.
	maxToolRounds    = 5
	maxImagesPerTurn = 3
	maxMemoryCalls   = 6
	imageTimeout     = 3 * time.Minute
)

// drawSlots bounds concurrent image generations server-wide (memory for decoding, upstream load).
var drawSlots = make(chan struct{}, 4)

// generate_image: the chat model writes the prompt, the server draws with the
// admin's image model and shows the picture in the reply.
var imageTool = llm.Tool{
	Name: "generate_image",
	Description: "Draw a picture and show it to the user. Call this whenever the user wants an image: draw, paint, " +
		"generate, design a poster / avatar / wallpaper / card / illustration, etc. One call draws one picture.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type": "string",
				"description": "Detailed description for the image model, in English: subject, style, composition, lighting, colors, mood. " +
					"Keep any text that must appear in the picture verbatim in its original language, in quotes.",
			},
			"image": map[string]any{
				"type": "string",
				"description": "Only when modifying an existing picture (one drawn earlier or a photo the user sent): its number shown in the conversation, " +
					"e.g. \"图2\". The prompt then says what to change and what to keep. Omit for a new picture.",
			},
			"aspect": map[string]any{
				"type": "string",
				"enum": []string{"square", "portrait", "landscape"},
				"description": "square by default; portrait for phone wallpapers, posters and full-body people; landscape for scenery and banners. " +
					"When modifying a picture, omit it to keep the original shape.",
			},
		},
		"required": []string{"prompt"},
	},
}

var recallMemoryTool = llm.Tool{
	Name: "recall_memory",
	Description: "Search or browse the user's saved memories AND past non-private chat messages. " +
		"Call this proactively whenever the user mentions past events/topics/pets/medications ('上次', '之前', '还记得吗', '还在继续'), " +
		"asks what or how many things you remember, or when core facts mention an entity whose past context is missing.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queries": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
				"description": "1 to 4 search keyword groups resolving pronouns and expanding colloquialisms/synonyms " +
					"(e.g. [\"柯基 瘙痒 皮肤\", \"小狗 痒 宠物医院\"]). Omit or pass [] when mode=\"recent\".",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"search", "recent"},
				"description": "\"search\" (default) ranks matching memories and raw chat excerpts; \"recent\" lists chronological memories with total counts (use when the user asks what/how many memories exist or what was discussed recently).",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "episode", "health", "family", "profile", "preference", "ongoing"},
				"description": "Optional category filter; \"all\" by default, or \"episode\" for past conversation summaries ('聊过的事').",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max memories to return (default 12 for search, 30 for recent).",
			},
		},
	},
}

var readConversationTool = llm.Tool{
	Name: "read_past_conversation",
	Description: "Read the original messages of a past conversation by its conv_id (shown in memory entries or recall_memory results). " +
		"Call this when a summary is too brief and you need exact details, numbers, medications, or conclusions from that chat.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"conversation_id": map[string]any{
				"type":        "string",
				"description": "The conv_id of the past conversation to inspect.",
			},
			"focus": map[string]any{
				"type":        "string",
				"description": "Optional keywords to focus on when the past conversation is very long.",
			},
		},
		"required": []string{"conversation_id"},
	},
}

// imageSize maps the tool's aspect to a size the model family accepts ("" = model default).
func imageSize(upstream, aspect string) string {
	m := strings.ToLower(upstream)
	switch {
	case strings.Contains(m, "dall-e-2"):
		return "" // square only
	case strings.Contains(m, "dall-e-3"):
		return map[string]string{"square": "1024x1024", "portrait": "1024x1792", "landscape": "1792x1024"}[aspect]
	}
	return map[string]string{"square": "1024x1024", "portrait": "1024x1536", "landscape": "1536x1024"}[aspect]
}

const toolPrompt = `

# 画图
你可以调用 generate_image 工具画图，画好的图会直接显示在你的回复里。
- 用户想要图片时（画、生成、做一张海报/头像/壁纸/贺卡/表情等）直接调用，不要先追问细节；只有完全不知道要画什么时才问一句。
- prompt 用详细的英文写（主体、风格、构图、光线、色彩、氛围）；图里要出现的中文文字原样保留并加引号。
- 一次调用画一张；用户要几张不同的图时，在同一次回复里同时发起几次调用（最多 3 张）。用户没要求多张时只画一张。
- 对话里的每张图都有编号（如「图2」），用户发的照片和你画的图都有。用户想修改某张图（换颜色、加东西、换背景、改成卡通风格、修照片等）时，把它的编号填进 image 参数，prompt 用英文写清要改什么、哪些保持不变；用户说"这张""刚才那张"时一般指最近的一张。
- 想要全新的图时不要填 image。
- 画好后用一两句话自然地回应，不要复述提示词，不要输出链接或 JSON。
- 不要说自己不能画图。`

const noImagePrompt = `

你目前不能画图。如果用户想要你画图或生成图片，温和地说明管理员还没有开通画图功能；可以先用文字描述画面，或帮他写一段画图提示词。`

const otherModelDrawsPrompt = `

当前这个模型不能画图（别的模型可以）。如果用户想要你画图或生成图片，温和地告诉他：点屏幕顶部的模型名称，换一个模型再说一遍就能画。`

const memoryToolPrompt = `

# 记忆与历史检索
你可以调用 recall_memory 和 read_past_conversation 工具查阅用户的记忆库和以往对话记录。
- 系统提示词里的「关于用户的事」只是常驻画像，消息附注里的「可能相关的往事」只是自动粗筛的少量片段，绝不代表用户的全部记忆。
- 遇到以下情况时，必须先调用 recall_memory 查找，不要未经检索就说「不记得」「看不到」或盲目猜测：
  1. 用户提到过去聊过的事、家人、宠物、药物、症状或安排（如「上次」「之前」「还记得吗」「还在继续」「为啥又…」）；
  2. 用户询问你记得哪些事、界面里有多少条记忆、最近聊过什么（设置 mode="recent" 浏览全量记忆与统计）；
  3. 常驻画像里提到了某个宠物、家人或健康问题，但当前缺少前因后果。
- 调用 recall_memory 时，主动把代词和口语换算成多组同义词/专名并发查询（例如用户说「小狗痒痒」、画像里有「五岁柯基」，传 queries: ["柯基 瘙痒 皮肤", "小狗 痒 医院 药浴"]）。
- 当记忆条目或检索结果带有 conv_id，而用户追问当时的具体细节（原因、费用、检查项、药名用法、原话结论）时，继续调用 read_past_conversation 读取原对话全文。`

// imageModel returns the configured image model when it is usable.
func (s *Service) imageModel(ctx context.Context) (*store.Model, *store.Provider) {
	id, _ := s.Store.GetSetting(ctx, "image_model_id")
	if id == "" {
		return nil, nil
	}
	m, p, ok := s.usableAny(ctx, id)
	if !ok || m.Kind != store.KindImage {
		return nil, nil
	}
	return m, p
}

// ImageAvailable reports whether conversations can draw pictures.
func (s *Service) ImageAvailable(ctx context.Context) bool {
	m, _ := s.imageModel(ctx)
	return m != nil
}

type toolSet struct {
	tools          []llm.Tool
	allowTools     bool
	canDraw        bool
	canRecall      bool
	canReadEarlier bool
	canSearch      bool
	canFetch       bool
	canUseSkills   bool
	skillCatalog   string
	leasedSkills   map[string]bool
	leaseNote      string
}

// toolsFor returns the tools a chat model may use in this reply.
func (s *Service) toolsFor(ctx context.Context, user *store.User, conv *store.Conversation, m *store.Model) toolSet {
	if !m.Tools || s.toolsRejected(m) {
		return toolSet{}
	}
	ts := toolSet{allowTools: true}
	if im, _ := s.imageModel(ctx); im != nil {
		ts.canDraw = true
		ts.tools = append(ts.tools, imageTool)
	}
	if !conv.Private && s.Memory != nil && s.Memory.Allowed(ctx, user, conv.AssistantID) {
		ts.canRecall = true
		ts.tools = append(ts.tools, recallMemoryTool, readConversationTool)
	}
	if s.Search != nil {
		if en, canFetch, _ := s.Search.ActiveStatus(ctx); en {
			ts.canSearch = true
			ts.tools = append(ts.tools, webSearchTool)
			if canFetch {
				ts.canFetch = true
				ts.tools = append(ts.tools, webFetchTool)
			}
		}
	}
	if !conv.Private && s.Skills != nil {
		sctx := s.skillContext(user, conv)
		if defs := s.Skills.List(sctx); len(defs) > 0 {
			ts.canUseSkills = true
			ts.skillCatalog = skills.FormatCatalog(defs)
			ts.tools = appendUniqueTools(ts.tools, skills.ReadSkillTool)
			if leasedKeys := s.activeLeasedSkillKeys(conv.ID); len(leasedKeys) > 0 {
				ts.leasedSkills = map[string]bool{}
				var summaries []string
				for _, k := range leasedKeys {
					if def, ok := s.Skills.Get(k, sctx); ok {
						ts.leasedSkills[def.Key] = true
						ts.tools = appendUniqueTools(ts.tools, def.Tools...)
						if strings.TrimSpace(def.LeaseSummary) != "" {
							summaries = append(summaries, strings.TrimSpace(def.LeaseSummary))
						}
					}
				}
				if len(summaries) > 0 {
					ts.leaseNote = strings.Join(summaries, " ") + " "
				}
			}
		}
	}
	return ts
}

// drawPrompt is the system prompt section about drawing for this reply.
func (s *Service) drawPrompt(ctx context.Context, canDraw bool) string {
	switch {
	case canDraw:
		return toolPrompt
	case s.ImageAvailable(ctx):
		return otherModelDrawsPrompt
	}
	return noImagePrompt
}

func toolKey(m *store.Model) string { return fmt.Sprintf("%s@%d", m.ID, m.UpdatedAt) }

// toolsRejected reports whether the upstream refused tools for this model
// (remembered until the model is edited, so nobody waits for the retry again).
func (s *Service) toolsRejected(m *store.Model) bool {
	_, ok := s.noTools.Load(toolKey(m))
	return ok
}

func (s *Service) markToolsRejected(m *store.Model) { s.noTools.Store(toolKey(m), true) }

// toolTurn carries the state of one reply's tool use.
type toolTurn struct {
	clientCtx            *ClientContext
	quotedComponent      *ComponentQuoteRef
	appControlCalls      int
	automation           bool
	user                 *store.User
	convID               string
	assistantID          *string
	asstID               string
	userQuestion         string
	foldedBeforeID       string
	run                  *Run
	private              bool
	images               int
	memCalls             int
	historyCalls         int
	searchCalls          int
	fetchCalls           int
	scheduleCalls        int
	componentCalls       int
	componentManageCalls int
	automationCalls      int
	updatedComponents    int
	lastCreatedMsgID     string
	lastCreatedCompID    string
	lastPinnedMsgID      string
	lastPinnedCompID     string
	lastUpdatedMsgID     string
	lastUpdatedCompID    string
	activeSkills         map[string]bool
	activatedSkills      map[string]bool
	usedSkillOwners      map[string]bool
	unlockedTools        []llm.Tool
	usedReadSkill        bool
	sources              []store.WebSource
	seenSourceURL        map[string]int
	postActivity         string
	cost                 float64
}
