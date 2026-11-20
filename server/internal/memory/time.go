package memory

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"nuan/internal/store"
)

const (
	dayMs                 = int64(24 * 3600 * 1000)
	RecallTouchCooldownMs = dayMs // at most 1 recall count increment per memory per 24h
	DefaultOngoingDays    = 21
	MaxOngoingDays        = 180
)

// episodeDate matches leading YYYY-MM-DD or YYYY-MM prefixes on episode and digest summaries.
var episodeDate = regexp.MustCompile(`^(\d{4}-\d{2}(?:-\d{2})?[ ：:]+)+`)

// retrospectiveCues indicate the user is explicitly referring back to a past chat or event.
var retrospectiveCues = []string{
	"上次", "之前", "以前", "上回", "前阵子", "前段时间", "上个月", "去年", "早些时候",
	"记得", "记不记得", "聊过", "说过", "提过", "讲过", "问过", "讨论过",
	"后来", "还在继续", "老毛病", "又犯了",
}

func hasRetrospectiveIntent(messages []string) bool {
	if len(messages) == 0 {
		return false
	}
	// Check the latest user turn (and immediate preceding turn if latest is very short).
	latest := strings.TrimSpace(messages[len(messages)-1])
	for _, cue := range retrospectiveCues {
		if strings.Contains(latest, cue) {
			return true
		}
	}
	if len([]rune(latest)) <= 10 && len(messages) >= 2 {
		prev := strings.TrimSpace(messages[len(messages)-2])
		for _, cue := range retrospectiveCues {
			if strings.Contains(prev, cue) {
				return true
			}
		}
	}
	return false
}

// memoryEventAt returns the timestamp when the remembered event/fact occurred,
// unaffected by later keyword backfills, embedding updates, or sleep compression.
func memoryEventAt(m *store.Memory) int64 {
	if m.EventAt > 0 {
		return m.EventAt
	}
	return m.CreatedAt
}

// calendarAgeDays returns the calendar-day distance in China time (CST) between eventMs and nowMs.
func calendarAgeDays(eventMs, nowMs int64) int {
	if eventMs <= 0 || nowMs <= eventMs {
		return 0
	}
	nowT := time.UnixMilli(nowMs).In(zone)
	evT := time.UnixMilli(eventMs).In(zone)
	nowDay := time.Date(nowT.Year(), nowT.Month(), nowT.Day(), 0, 0, 0, 0, zone)
	evDay := time.Date(evT.Year(), evT.Month(), evT.Day(), 0, 0, 0, 0, zone)
	days := int(nowDay.Sub(evDay) / (24 * time.Hour))
	if days < 0 {
		return 0
	}
	return days
}

func relativeSpan(days int) string {
	switch {
	case days <= 0:
		return "今天"
	case days == 1:
		return "昨天"
	case days <= 7:
		return fmt.Sprintf("%d天前", days)
	case days <= 29:
		return fmt.Sprintf("%d周前", max(1, days/7))
	case days < 365:
		return fmt.Sprintf("%d个月前", max(1, days/30))
	default:
		return fmt.Sprintf("%d年前", max(1, days/365))
	}
}

// sanitizeStaleTemporalWords rewrites unanchored present-tense markers ("近期", "最近", "正在")
// on expired ongoing memories so models never mistake a past temporary state for happening today.
func sanitizeStaleTemporalWords(s string) string {
	s = strings.TrimSpace(s)
	for _, w := range []string{"近期正在", "最近正在", "这几天正在", "这阵子正在"} {
		s = strings.ReplaceAll(s, w, "当时正在")
	}
	if !strings.Contains(s, "当时正在") {
		s = strings.ReplaceAll(s, "正在", "当时正在")
	}
	for _, w := range []string{"近期", "最近", "这几天", "这阵子"} {
		s = strings.ReplaceAll(s, w, "当时")
	}
	return s
}

// formatCoreEntry formats a core memory line for the system prompt. Ongoing items
// include their recorded calendar date (stable across minutes to preserve prefix caching).
func formatCoreEntry(m *store.Memory) string {
	content := strings.TrimSpace(m.Content)
	if m.Category != "ongoing" {
		return content
	}
	evMs := memoryEventAt(m)
	if evMs <= 0 {
		return content
	}
	rec := time.UnixMilli(evMs).In(zone).Format("2006-01-02")
	if m.ValidUntil != nil && *m.ValidUntil > 0 {
		until := time.UnixMilli(*m.ValidUntil).In(zone).Format("2006-01-02")
		return fmt.Sprintf("%s（记于 %s，预计至 %s）", content, rec, until)
	}
	return fmt.Sprintf("%s（记于 %s）", content, rec)
}

// formatRecallEntry formats a retrieval-layer memory with an explicit relative-time
// and lifecycle tag so the chat model always understands how old the item is.
func formatRecallEntry(m *store.Memory, nowMs int64, currentConvID string) string {
	evMs := memoryEventAt(m)
	days := calendarAgeDays(evMs, nowMs)
	tag := relativeSpan(days)
	content := strings.TrimSpace(m.Content)
	hasLeadingDate := episodeDate.MatchString(content)
	if !hasLeadingDate && evMs > 0 {
		if days >= 30 {
			tag += "·" + time.UnixMilli(evMs).In(zone).Format("2006-01-02")
		} else if days >= 8 {
			tag += "·" + time.UnixMilli(evMs).In(zone).Format("01-02")
		}
	}
	isExpiredOngoing := m.Category == "ongoing" && m.ValidUntil != nil && *m.ValidUntil < nowMs
	if isExpiredOngoing {
		tag += "·已过时效"
		content = sanitizeStaleTemporalWords(content)
	} else if m.Compressed {
		tag += "·历史汇总"
	}
	entry := "[" + tag + "] " + content
	if m.SourceConvID != nil && *m.SourceConvID != "" && *m.SourceConvID != currentConvID {
		entry += " [conv:" + *m.SourceConvID + "]"
	}
	return entry
}

// formatOngoingAsEpisode converts an expired ongoing memory's text into a dated
// historical episode entry during nightly sleep consolidation.
func formatOngoingAsEpisode(m *store.Memory) string {
	evMs := memoryEventAt(m)
	prefix := ""
	if evMs > 0 {
		prefix = time.UnixMilli(evMs).In(zone).Format("2006-01-02") + " "
	}
	body := strings.TrimSpace(episodeDate.ReplaceAllString(strings.TrimSpace(m.Content), ""))
	body = sanitizeStaleTemporalWords(body)
	return strings.TrimSpace(prefix + body)
}
