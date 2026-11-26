// Package guide embeds modular, code-verified user manuals for 小暖 App so the
// chat model can load only the relevant topic on demand via the app_guide skill.
package guide

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed docs/*.md
var docsFS embed.FS

// MaxTopicsPerRead bounds a single read_app_guide call so context stays compact.
const MaxTopicsPerRead = 3

// Topic describes one modular help document embedded in docs/.
type Topic struct {
	Key     string
	Title   string
	Summary string
	File    string
}

// Topics is the ordered catalog of available user-guide documents.
var Topics = []Topic{
	{
		Key:     "overview",
		Title:   "能力总览与界面导航",
		Summary: "小暖整体能做什么、手机端抽屉与电脑端侧栏布局差异、搜索/新对话/设置等常用入口",
		File:    "docs/overview.md",
	},
	{
		Key:     "memories",
		Title:   "小暖记住的事与记忆导入",
		Summary: "查看/开关/增删改长期记忆、从 ChatGPT/Gemini/Claude/豆包导入步骤、3万字上限与不支持直接传 Word/TXT 的应对方法",
		File:    "docs/memories.md",
	},
	{
		Key:     "chat",
		Title:   "聊天、模型、图片与语音",
		Summary: "发消息与换行、切换模型/自动选模型/深度思考、拍照发图（最多9张）、画图改图与存图、联网搜索、语音输入与朗读、复制/选字/修改重发/换模型重答/删消息",
		File:    "docs/chat.md",
	},
	{
		Key:     "privacy_and_share",
		Title:   "隐私对话、分享对话与外部应用分享",
		Summary: "开启/退出隐私对话及限制、共享给家人多人协作、分享短口令（30天有效）、生成长图海报、共享与分享管理、Android 从外部 App 分享进来",
		File:    "docs/privacy_and_share.md",
	},
	{
		Key:     "schedules",
		Title:   "我的日程与 AI 定时任务",
		Summary: "口头或手动建日程提醒、周期重复、手机本地通知与电脑端差异；后台 AI 定时任务创建、启停、立即运行、查看记录与自动更新首页卡片",
		File:    "docs/schedules.md",
	},
	{
		Key:     "home_and_cards",
		Title:   "首页与交互卡片",
		Summary: "天气/待办清单/资讯列表/图表/指标看板/互动问答/询问卡片、固定到首页、编辑与历史版本、引用卡片到新对话、口头管理首页卡片与猜你想问",
		File:    "docs/home_and_cards.md",
	},
	{
		Key:     "assistants",
		Title:   "找专门的助手",
		Summary: "首页切换角色助手、快捷提问建议、与普通/隐私对话的关系",
		File:    "docs/assistants.md",
	},
	{
		Key:     "settings_and_shortcuts",
		Title:   "设置、快捷键、账号安全与管理员边界",
		Summary: "手动或口头调本机字号与深浅色、朗读语速、电脑端键盘快捷键、修改密码、免输登录口令 #xn...xn#、自托管服务器、Android 检查更新、管理员菜单与安全边界",
		File:    "docs/settings_and_shortcuts.md",
	},
}

var topicByKey = func() map[string]Topic {
	m := make(map[string]Topic, len(Topics))
	for _, t := range Topics {
		m[t.Key] = t
	}
	return m
}()

// TopicKeys returns all valid topic keys in catalog order.
func TopicKeys() []string {
	out := make([]string, len(Topics))
	for i, t := range Topics {
		out[i] = t.Key
	}
	return out
}

// Lookup finds a topic by normalized key.
func Lookup(key string) (Topic, bool) {
	t, ok := topicByKey[strings.ToLower(strings.TrimSpace(key))]
	return t, ok
}

// CatalogMarkdown renders the compact directory returned by read_skill("app_guide").
func CatalogMarkdown() string {
	var b strings.Builder
	b.WriteString("可用指南主题（通过 `read_app_guide` 按需读取，每次最多选 3 个）：\n")
	for _, t := range Topics {
		fmt.Fprintf(&b, "- `%s`（%s）：%s\n", t.Key, t.Title, t.Summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Read loads up to MaxTopicsPerRead deduplicated topics and returns their combined
// markdown content, loaded titles, and any unrecognized keys.
func Read(keys []string) (content string, titles []string, unknown []string) {
	seen := make(map[string]bool, len(keys))
	var docs []string
	for _, raw := range keys {
		k := strings.ToLower(strings.TrimSpace(raw))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		t, ok := topicByKey[k]
		if !ok {
			unknown = append(unknown, raw)
			continue
		}
		if len(docs) >= MaxTopicsPerRead {
			continue
		}
		data, err := docsFS.ReadFile(t.File)
		if err != nil {
			continue
		}
		docs = append(docs, strings.TrimSpace(string(data)))
		titles = append(titles, t.Title)
	}
	return strings.Join(docs, "\n\n---\n\n"), titles, unknown
}
