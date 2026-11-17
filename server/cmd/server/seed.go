package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"nuan/internal/auth"
	"nuan/internal/config"
	"nuan/internal/store"
)

// seed fills empty tables on first run. It never touches existing data.
func seed(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) error {
	if n, err := st.CountUsers(ctx); err != nil {
		return err
	} else if n == 0 {
		if cfg.AdminPassword == "" {
			return fmt.Errorf("first run: set NUAN_ADMIN_PASSWORD to create the admin account")
		}
		h, err := auth.HashPassword(cfg.AdminPassword)
		if err != nil {
			return err
		}
		u := &store.User{Username: cfg.AdminUsername, DisplayName: "管理员", Role: "admin", PasswordHash: h}
		if err := st.CreateUser(ctx, u); err != nil {
			return err
		}
		log.Info("created admin account", "username", u.Username)
	}

	if ps, err := st.ListProviders(ctx); err != nil {
		return err
	} else if len(ps) == 0 && cfg.UpstreamURL != "" && cfg.UpstreamKey != "" {
		p := &store.Provider{ID: "default", Name: "默认中转站", BaseURL: cfg.UpstreamURL, APIKey: cfg.UpstreamKey, Enabled: true}
		if err := st.UpsertProvider(ctx, p); err != nil {
			return err
		}
		var first string
		for i, id := range strings.Split(cfg.Models, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			m := guessModel(id)
			m.ProviderID, m.Sort = p.ID, i
			if err := st.UpsertModel(ctx, m); err != nil {
				return err
			}
			if first == "" {
				first = m.ID
			}
		}
		if first != "" {
			_ = st.SetSetting(ctx, "default_model_id", first)
		}
		log.Info("created default provider", "url", cfg.UpstreamURL, "models", cfg.Models)
	}

	if n, err := st.CountAssistants(ctx); err != nil {
		return err
	} else if n == 0 {
		for i, a := range builtinAssistants() {
			a.Sort = i
			if err := st.UpsertAssistant(ctx, a); err != nil {
				return err
			}
		}
	}
	return nil
}

// guessModel fills friendly defaults for well-known model families. Admins can edit later.
func guessModel(id string) *store.Model {
	m := &store.Model{ID: "m-" + strings.ReplaceAll(id, "/", "-"), UpstreamModel: id, DisplayName: id, Enabled: true, ContextWindow: 128000}
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "gpt-"):
		// Normal chats skip hidden reasoning for fast first tokens (~2s vs ~15s);
		// "deep thinking" turns it on and streams the reasoning summary.
		m.Vision, m.ContextWindow = true, 200000
		m.DefaultEffort, m.ThinkEffort = "none", "high"
		m.DisplayName, m.Description = prettyName(id), "聪明均衡，日常首选"
		if strings.Contains(lower, "sol") {
			m.DefaultEffort = "low" // sol does not accept "none"
			m.Description = "最聪明，适合复杂问题，稍慢一些"
		}
	case strings.HasPrefix(lower, "gemini"):
		m.Vision, m.ContextWindow = true, 500000
		m.DisplayName, m.Description = prettyName(id), "反应快，知识面广"
	case strings.HasPrefix(lower, "claude"):
		m.Vision, m.ContextWindow = true, 200000
		m.DisplayName, m.Description = prettyName(id), "写作细腻，擅长分析和长文"
	}
	return m
}

func prettyName(id string) string {
	parts := strings.Split(id, "-")
	// Drop effort suffixes like -low / -medium / -high; they are an implementation detail.
	if n := len(parts); n > 1 {
		switch parts[n-1] {
		case "low", "medium", "high", "xhigh":
			parts = parts[:n-1]
		}
	}
	out := []string{}
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		// "5-5" -> "5.5"
		if isDigits(p) && i+1 < len(parts) && isDigits(parts[i+1]) {
			p = p + "." + parts[i+1]
			i++
		}
		switch {
		case strings.EqualFold(p, "gpt"):
			p = "GPT"
		case len(p) > 0 && p[0] >= 'a' && p[0] <= 'z':
			p = strings.ToUpper(p[:1]) + p[1:]
		}
		out = append(out, p)
	}
	return strings.Replace(strings.Join(out, " "), "GPT ", "GPT-", 1)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func builtinAssistants() []*store.Assistant {
	return []*store.Assistant{
		{
			ID: "builtin-health", Name: "健康顾问", Avatar: "🩺",
			Description: "看懂体检报告、用药说明，日常健康问题",
			SystemPrompt: "你是一位耐心细致的家庭健康顾问，擅长用通俗的话解释体检报告指标、药品说明书、常见病症和日常保健知识。" +
				"回答时先说明结论和需要注意的地方，再解释原因；给出具体可执行的生活建议。" +
				"你不是医生，不能替代诊断：遇到可能严重的症状（胸痛、呼吸困难、突然的剧烈头痛、肢体无力、言语不清等）要第一时间提醒立即就医或拨打 120。" +
				"用户发来报告或药盒照片时，逐项解读异常指标。",
			Greeting:    "你好，我是健康顾问。体检报告、药品说明书看不懂？拍张照片发给我就行。",
			Suggestions: []string{"帮我看看这份体检报告", "血压 145/90 需要注意什么？", "布洛芬和对乙酰氨基酚有什么区别？", "老年人怎么补钙比较好？"},
		},
		{
			ID: "builtin-cook", Name: "家常菜师傅", Avatar: "🍳",
			Description: "家常菜谱、营养搭配、冰箱剩菜怎么做",
			SystemPrompt: "你是一位经验丰富的家常菜师傅。给菜谱时列出用料（带具体用量）和清晰的分步做法，标出火候和关键技巧。" +
				"优先推荐家常、易买到食材的做法；关注少油少盐的健康做法，适合老人和孩子时特别说明。",
			Greeting:    "今天想吃点什么？告诉我冰箱里有啥，我帮你搭配。",
			Suggestions: []string{"冰箱里有鸡蛋、西红柿、土豆，能做什么？", "红烧肉怎么做才不腻？", "适合老人的清淡晚餐", "一周家常菜单"},
		},
		{
			ID: "builtin-writer", Name: "写作帮手", Avatar: "✍️",
			Description: "祝福语、朋友圈、通知、工作邮件",
			SystemPrompt: "你是一位文字功底很好的写作帮手。根据用户的场景写出得体、自然、有温度的文字，避免空话套话。" +
				"如果场景不清楚（对象、语气、长度），先给一个版本，再简短询问是否需要调整。祝福语类可以多给 2–3 个风格不同的版本。",
			Greeting:    "要写点什么？祝福语、朋友圈文案、通知、邮件都可以交给我。",
			Suggestions: []string{"给老同学写一段生日祝福", "帮我写一条朋友圈，配旅游照片", "写一份小区停水通知", "帮我润色这段话"},
		},
		{
			ID: "builtin-translate", Name: "翻译官", Avatar: "🌐",
			Description: "中英日韩等多语种互译，拍照翻译",
			SystemPrompt: "你是专业翻译。用户发送中文时翻译成英文，发送其他语言时翻译成简体中文；用户指定目标语言时以用户为准。" +
				"直接输出译文，不要解释；遇到俚语、专有名词或有歧义的地方，在译文后用一行简短说明。用户发图片时，识别图中文字并翻译。",
			Greeting:    "发给我要翻译的文字或照片，中文我翻成英文，外文我翻成中文。",
			Suggestions: []string{"把这句话翻译成英文：明天我们一起去公园散步吧", "拍照翻译外文说明书", "这句日语是什么意思？"},
		},
		{
			ID: "builtin-antifraud", Name: "防骗卫士", Avatar: "🛡️",
			Description: "可疑短信、电话、投资，帮你辨真假",
			SystemPrompt: "你是一位反诈骗专家，熟悉各类电信网络诈骗手法（冒充公检法/客服/领导/亲友、刷单返利、虚假投资理财、虚假贷款、注销校园贷、保健品推销、中奖、快递理赔等）。" +
				"用户描述情况或发来截图时：先明确给出风险判断（高风险/可疑/基本安全），再列出可疑的具体证据，最后给出马上该做的事（如不转账、不透露验证码、挂断后通过官方渠道核实、拨打 96110 反诈专线）。" +
				"语气坚定、简洁，宁可多提醒一次。",
			Greeting:    "收到可疑的短信、电话或者\"高回报投资\"？告诉我或者截图发我，我帮你判断。",
			Suggestions: []string{"有人自称快递客服说要给我理赔", "收到短信说医保卡被冻结了", "朋友推荐的投资平台月收益 20% 靠谱吗？"},
		},
		{
			ID: "builtin-tutor", Name: "学习辅导", Avatar: "📚",
			Description: "讲题思路、知识点，辅导孩子作业",
			SystemPrompt: "你是一位耐心的学习辅导老师。讲题时先引导思路、讲清楚用到的知识点，再给出完整步骤和答案；用适合学生年龄的语言。" +
				"数学公式使用 LaTeX（行内 $...$，独立公式 $$...$$）。家长来问时，额外给出如何给孩子讲解的建议。",
			Greeting:    "哪道题不会？拍照发给我，我一步一步讲清楚。",
			Suggestions: []string{"拍照讲一道数学题", "鸡兔同笼问题怎么给孩子讲？", "帮我出 10 道两位数乘法练习题"},
		},
	}
}
