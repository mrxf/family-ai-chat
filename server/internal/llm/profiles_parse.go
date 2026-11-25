package llm

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reStripParamTokens = regexp.MustCompile(`(?i)(?:^|[-_])(?:\d+x\d+b|a\d+b|\d+(?:\.\d+)?b|\d+k|\d+m)(?:$|[-_])`)
	reParamSize        = regexp.MustCompile(`(?i)(?:^|[-_/])(\d+)b(?:$|[-_/:])`)
	reMoETotalParam    = regexp.MustCompile(`(?i)(?:^|[-_/])(\d+)x(\d+)b(?:$|[-_/:])`)
	reVersionDotOrDash = regexp.MustCompile(`(?:^|[-_a-z])(\d{1,2})(?:[.\-](\d{1,2}))?(?:$|[-_a-z])`)
	reKimiVersion      = regexp.MustCompile(`(?i)kimi[-_]?k(\d{1,2})(?:[.\-](\d{1,2}))?`)
	reDeepSeekSeries   = regexp.MustCompile(`(?i)deepseek[-_]?([vr])(\d{1,2})(?:[.\-](\d{1,2}))?`)
	reOSeriesVersion   = regexp.MustCompile(`(?i)^o(\d{1,2})(?:[.\-](\d{1,2}))?`)
	reLeadVer          = regexp.MustCompile(`^v?(\d{1,2})(?:[.\-](\d{1,2}))?(?:$|[^0-9])`)
	reTrailVer         = regexp.MustCompile(`(?:^|[^0-9])v?(\d{1,2})(?:[.\-](\d{1,2}))?$`)
)

func normalizeModelKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s, _, _ = strings.Cut(s, ":")
	if loc := reDated.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	for _, suf := range []string{"-low", "-medium", "-high", "-xhigh", "-latest", "-preview"} {
		s = strings.TrimSuffix(s, suf)
	}
	return s
}

func hasWord(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func parseParamBillions(norm string) int {
	if m := reMoETotalParam.FindStringSubmatch(norm); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return a * b
	}
	if m := reParamSize.FindStringSubmatch(norm); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// cleanForVersion strips parameter counts (70b, 235b, a22b, 8x7b) and context tokens (128k)
// so numbers like 235B or 128K are never misread as model release versions.
func cleanForVersion(norm string) string {
	cleaned := norm
	for i := 0; i < 3; i++ {
		next := reStripParamTokens.ReplaceAllString(cleaned, "-")
		if next == cleaned {
			break
		}
		cleaned = next
	}
	return strings.Trim(cleaned, "-_")
}

// extractFamilyVersion extracts Major[.Minor] near known family keywords.
// Handles both "claude-opus-5-5", "claude-opus-4.6", "claude-3-5-sonnet", "fable-5.2", "gpt-6.1-terra".
func extractFamilyVersion(norm string, anchors ...string) ModelVersion {
	cleaned := cleanForVersion(norm)
	for _, anchor := range anchors {
		idx := strings.Index(cleaned, anchor)
		if idx < 0 {
			continue
		}
		after := strings.TrimLeft(cleaned[idx+len(anchor):], "-_. ")
		if v := leadingVersion(after); v.Known() {
			return v
		}
		// Also check right before the anchor (e.g. "claude-3-5-sonnet" when anchor="sonnet", or "5.5-opus").
		before := strings.TrimRight(cleaned[:idx], "-_. ")
		if v := trailingVersion(before); v.Known() {
			return v
		}
	}
	return extractGenericVersion(cleaned)
}

func leadingVersion(s string) ModelVersion {
	m := reLeadVer.FindStringSubmatch(s)
	if m == nil {
		return ModelVersion{}
	}
	maj, _ := strconv.Atoi(m[1])
	if maj == 0 || maj > 20 {
		return ModelVersion{}
	}
	minV, _ := strconv.Atoi(m[2])
	return ModelVersion{Major: maj, Minor: minV, HasMinor: m[2] != ""}
}

func trailingVersion(s string) ModelVersion {
	m := reTrailVer.FindStringSubmatch(s)
	if m == nil {
		return ModelVersion{}
	}
	maj, _ := strconv.Atoi(m[1])
	if maj == 0 || maj > 20 {
		return ModelVersion{}
	}
	minV, _ := strconv.Atoi(m[2])
	return ModelVersion{Major: maj, Minor: minV, HasMinor: m[2] != ""}
}

func extractGenericVersion(cleaned string) ModelVersion {
	m := reVersionDotOrDash.FindStringSubmatch(cleaned)
	if m == nil {
		return ModelVersion{}
	}
	maj, _ := strconv.Atoi(m[1])
	if maj == 0 || maj > 20 {
		return ModelVersion{}
	}
	minV, _ := strconv.Atoi(m[2])
	return ModelVersion{Major: maj, Minor: minV, HasMinor: m[2] != ""}
}

func baseFamilyPrefix(norm string) string {
	parts := strings.FieldsFunc(norm, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	if len(parts) == 0 {
		return "unknown"
	}
	return parts[0]
}
