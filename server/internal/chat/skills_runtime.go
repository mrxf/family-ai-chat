package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/skills"
	"nuan/internal/store"
)

const (
	defaultSkillLeaseTurns = 3
	maxSkillBonusRounds    = 2
	maxTrackedLeaseConvs   = 1024
)

type skillLease struct {
	skillKey       string
	remainingTurns int
	updatedAt      int64
}

func (s *Service) skillContext(user *store.User, conv *store.Conversation) skills.Context {
	var uid, cid string
	var aid *string
	priv := false
	if user != nil {
		uid = user.ID
	}
	if conv != nil {
		cid = conv.ID
		aid = conv.AssistantID
		priv = conv.Private
	}
	return skills.Context{
		UserID:         uid,
		ConversationID: cid,
		AssistantID:    aid,
		Private:        priv,
		Now:            time.Now().In(shanghai),
	}
}

func (s *Service) activeLeasedSkillKeys(convID string) []string {
	if convID == "" {
		return nil
	}
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	leases := s.skillLeases[convID]
	if len(leases) == 0 {
		return nil
	}
	keys := make([]string, 0, len(leases))
	for k, l := range leases {
		if l.remainingTurns > 0 {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Service) activateSkillLease(convID, skillKey string) {
	if convID == "" || skillKey == "" {
		return
	}
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	if s.skillLeases == nil {
		s.skillLeases = map[string]map[string]*skillLease{}
	}
	if len(s.skillLeases) >= maxTrackedLeaseConvs && s.skillLeases[convID] == nil {
		// Evict the oldest conversation entry to bound memory.
		var oldestConv string
		var oldestTs int64
		for cid, m := range s.skillLeases {
			var maxTs int64
			for _, l := range m {
				if l.updatedAt > maxTs {
					maxTs = l.updatedAt
				}
			}
			if oldestConv == "" || maxTs < oldestTs {
				oldestConv = cid
				oldestTs = maxTs
			}
		}
		if oldestConv != "" {
			delete(s.skillLeases, oldestConv)
		}
	}
	m := s.skillLeases[convID]
	if m == nil {
		m = map[string]*skillLease{}
		s.skillLeases[convID] = m
	}
	m[skillKey] = &skillLease{
		skillKey:       skillKey,
		remainingTurns: defaultSkillLeaseTurns,
		updatedAt:      time.Now().UnixMilli(),
	}
}

// tickSkillLeases advances conversation skill leases once a turn finishes:
// skills activated or whose tools were invoked this turn refresh to defaultSkillLeaseTurns;
// idle leases decrement and expire cleanly at 0.
func (s *Service) tickSkillLeases(convID string, activated, used map[string]bool) {
	if convID == "" {
		return
	}
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	m := s.skillLeases[convID]
	if len(m) == 0 {
		return
	}
	now := time.Now().UnixMilli()
	for k, l := range m {
		if activated[k] || used[k] {
			l.remainingTurns = defaultSkillLeaseTurns
			l.updatedAt = now
			continue
		}
		l.remainingTurns--
		if l.remainingTurns <= 0 {
			delete(m, k)
		}
	}
	if len(m) == 0 {
		delete(s.skillLeases, convID)
	}
}

func appendUniqueTools(dst []llm.Tool, extras ...llm.Tool) []llm.Tool {
	seen := make(map[string]bool, len(dst)+len(extras))
	for _, t := range dst {
		seen[t.Name] = true
	}
	for _, t := range extras {
		if !seen[t.Name] {
			seen[t.Name] = true
			dst = append(dst, t)
		}
	}
	return dst
}

func parseReadSkillKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var args struct {
		SkillKey string `json:"skill_key"`
		AltKey   string `json:"skillKey"`
		Key      string `json:"key"`
		Name     string `json:"name"`
	}
	if json.Unmarshal([]byte(raw), &args) == nil {
		for _, v := range []string{args.SkillKey, args.AltKey, args.Key, args.Name} {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func (s *Service) execReadSkillCall(t *toolTurn, rawArgs string) (string, []llm.Tool) {
	if t.private || s.Skills == nil {
		return "当前是隐私对话或未启用扩展技能，无法加载该技能。", nil
	}
	key := parseReadSkillKey(rawArgs)
	sctx := skills.Context{
		ClientContext:  t.clientCtx,
		UserID:         t.user.ID,
		ConversationID: t.convID,
		AssistantID:    t.assistantID,
		Private:        t.private,
		Now:            time.Now().In(shanghai),
	}
	if key == "" {
		defs := s.Skills.List(sctx)
		var keys []string
		for _, d := range defs {
			keys = append(keys, d.Key)
		}
		return fmt.Sprintf("参数错误：请提供要读取的 skill_key（当前可用：%s）。", strings.Join(keys, ", ")), nil
	}
	def, ok := s.Skills.Get(key, sctx)
	if !ok {
		defs := s.Skills.List(sctx)
		var keys []string
		for _, d := range defs {
			keys = append(keys, d.Key)
		}
		return fmt.Sprintf("未找到技能 %q。当前可用的 skill_key：%s。", key, strings.Join(keys, ", ")), nil
	}

	if t.activeSkills == nil {
		t.activeSkills = map[string]bool{}
	}
	t.activeSkills[def.Key] = true
	s.activateSkillLease(t.convID, def.Key)

	t.run.addDoneStep("skill", "已加载技能："+def.Title, "")
	t.run.setActivity("正在读取「" + def.Title + "」指南…")
	t.postActivity = "正在按「" + def.Title + "」处理…"

	if def.Key == "app_control" {
		cp := *def
		if t.automation {
			cp.Tools = homeControlTools()
		}
		def = &cp
	}
	toolNames := def.AllowedToolNames()
	var b strings.Builder
	fmt.Fprintf(&b, "技能已激活：%s（%s）\n", def.Title, def.Key)
	if len(toolNames) > 0 {
		fmt.Fprintf(&b, "现已解锁并可直接调用以下工具：%s\n\n", strings.Join(toolNames, ", "))
	}
	b.WriteString(def.RenderContent(sctx))
	return strings.TrimSpace(b.String()), def.Tools
}
