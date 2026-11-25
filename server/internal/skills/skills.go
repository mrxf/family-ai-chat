// Package skills implements on-demand, skill-scoped tool loading for chat models.
//
// High-frequency capabilities (image generation, web search, memory recall, and read_skill
// itself) are mounted as common tools on every turn. Lower-frequency domain capabilities
// (such as schedule management, maps, ledgers, etc.) are grouped into Skills:
//   - Only a compact catalog summary (with brief tool hints) sits in the system prompt,
//     keeping daily chats lightweight and 100% friendly to upstream prefix caches.
//   - Calling read_skill(skill_key) returns the skill's full operational guide and
//     dynamically unlocks its bound tools for the rest of the turn (and a short
//     multi-turn conversation lease).
package skills

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nuan/internal/llm"
)

type ToolScope string

const (
	ScopeCommon ToolScope = "common"
	ScopeSkill  ToolScope = "skill"

	ReadSkillToolName = "read_skill"
)

// ToolHint discloses a skill-scoped tool's name and one-line capability inside the
// skill catalog so a model looking for a specific tool knows which skill to read first.
type ToolHint struct {
	Name  string
	Brief string
}

// Context carries per-turn metadata for skill visibility and dynamic content rendering.
type Context struct {
	UserID         string
	ConversationID string
	AssistantID    *string
	ClientContext  any // transient device snapshot, rendered only by the skill Dynamic callback
	Private        bool
	Now            time.Time
}

// Definition describes one domain skill and the tools it unlocks.
type Definition struct {
	Key          string
	Title        string
	Description  string
	WhenToUse    string
	LeaseSummary string // compact static reminder for leased follow-up turns (no changing counters)
	ToolHints    []ToolHint
	Tools        []llm.Tool
	Content      string
	Dynamic      func(ctx Context) string
	Available    func(ctx Context) bool
}

func (d *Definition) AllowedToolNames() []string {
	names := make([]string, 0, len(d.Tools))
	for _, t := range d.Tools {
		names = append(names, t.Name)
	}
	return names
}

func (d *Definition) RenderContent(ctx Context) string {
	body := strings.TrimSpace(d.Content)
	if d.Dynamic != nil {
		if dyn := strings.TrimSpace(d.Dynamic(ctx)); dyn != "" {
			if body != "" {
				body += "\n\n" + dyn
			} else {
				body = dyn
			}
		}
	}
	return body
}

// Provider supplies available skills for a conversation turn.
type Provider interface {
	List(ctx Context) []Definition
	Get(key string, ctx Context) (*Definition, bool)
	OwnerSkill(toolName string, contexts ...Context) (*Definition, bool)
}

// Registry is a concurrency-safe in-memory SkillProvider.
type Registry struct {
	mu     sync.RWMutex
	order  []string
	byKey  map[string]Definition
	byTool map[string][]string // tool name -> ordered owning skill keys
}

func NewRegistry(defs ...Definition) *Registry {
	r := &Registry{
		byKey:  map[string]Definition{},
		byTool: map[string][]string{},
	}
	for _, d := range defs {
		r.Register(d)
	}
	return r
}

func (r *Registry) Register(d Definition) {
	key := normalizeKey(d.Key)
	if key == "" {
		return
	}
	d.Key = key
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byKey[key]; !exists {
		r.order = append(r.order, key)
	} else {
		for name, owners := range r.byTool {
			kept := []string{}
			for _, owner := range owners {
				if owner != key {
					kept = append(kept, owner)
				}
			}
			r.byTool[name] = kept
		}
	}
	r.byKey[key] = d
	for _, t := range d.Tools {
		r.byTool[t.Name] = append(r.byTool[t.Name], key)
	}
}

func (r *Registry) List(ctx Context) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, 0, len(r.order))
	for _, k := range r.order {
		d := r.byKey[k]
		if d.Available != nil && !d.Available(ctx) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (r *Registry) Get(key string, ctx Context) (*Definition, bool) {
	norm := normalizeKey(key)
	if norm == "" {
		return nil, false
	}
	r.mu.RLock()
	d, ok := r.byKey[norm]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if d.Available != nil && !d.Available(ctx) {
		return nil, false
	}
	cp := d
	return &cp, true
}

func (r *Registry) OwnerSkill(toolName string, contexts ...Context) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range r.byTool[strings.TrimSpace(toolName)] {
		d := r.byKey[key]
		if len(contexts) > 0 && d.Available != nil && !d.Available(contexts[0]) {
			continue
		}
		cp := d
		return &cp, true
	}
	return nil, false
}

func normalizeKey(k string) string {
	return strings.ToLower(strings.TrimSpace(k))
}

// ReadSkillTool is the builtin common tool that reads a skill's guide and mounts its tools.
var ReadSkillTool = llm.Tool{
	Name: ReadSkillToolName,
	Description: "Read and activate a domain skill's complete operational guide, rules, and bound tools. " +
		"Call this BEFORE handling a domain task listed in the skill catalog or whenever you need any of the skill-scoped tools disclosed under that skill.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_key": map[string]any{
				"type":        "string",
				"description": "The exact skill key from the skill catalog (e.g. \"schedule\").",
			},
		},
		"required": []string{"skill_key"},
	},
}

// FormatCatalog renders the static skill catalog section for the system prompt.
// It contains no timestamps or per-turn counters so upstream prefix caching stays intact.
func FormatCatalog(defs []Definition) string {
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n# 技能与扩展工具目录\n")
	b.WriteString("除常驻工具外，你还拥有以下按需加载的领域技能。默认状态下，技能专属工具尚未挂载到当前可用工具列表中。\n")
	b.WriteString("- 当用户的话题涉及某项技能的场景，或你需要使用该技能下披露的专属工具时，请先调用 `read_skill`（传入对应的 `skill_key`）。\n")
	b.WriteString("- **主动择优呈现**：若回答内容天然契合目录中的原生交互组件，无需等用户开口要求，主动优先调用对应技能用组件呈现（避免用 Markdown 表格或冗长列表硬凑），普通聊天与纯文字说明则不要硬用组件；若同时需要联网查数据，可与 `web_search` 同轮并发调用 `read_skill`。\n")
	b.WriteString("- 成功调用 `read_skill` 后，系统会返回该技能的完整操作指南与边界规范，并在下一步自动为你解锁对应的专属工具；未激活前请勿直接盲调未解锁的工具。\n\n")
	b.WriteString("可用技能目录：\n")
	for _, d := range defs {
		label := d.Key
		if d.Title != "" && d.Title != d.Key {
			label = fmt.Sprintf("`%s`（%s）", d.Key, d.Title)
		} else {
			label = fmt.Sprintf("`%s`", d.Key)
		}
		fmt.Fprintf(&b, "- %s：%s", label, strings.TrimSpace(d.Description))
		if wt := strings.TrimSpace(d.WhenToUse); wt != "" {
			fmt.Fprintf(&b, "\n  - 适用场景：%s", wt)
		}
		hints := formatToolHints(d)
		if hints != "" {
			fmt.Fprintf(&b, "\n  - `read_skill` 后解锁工具：%s", hints)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatToolHints(d Definition) string {
	if len(d.ToolHints) > 0 {
		parts := make([]string, 0, len(d.ToolHints))
		for _, h := range d.ToolHints {
			if strings.TrimSpace(h.Brief) != "" {
				parts = append(parts, fmt.Sprintf("`%s`（%s）", h.Name, h.Brief))
			} else {
				parts = append(parts, fmt.Sprintf("`%s`", h.Name))
			}
		}
		return strings.Join(parts, "、")
	}
	names := d.AllowedToolNames()
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, "、")
}
