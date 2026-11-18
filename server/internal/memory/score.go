package memory

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"nuan/internal/store"
)

const (
	// Semantic matches need moderate cosine agreement; lexical coverage rescues
	// exact drug names. Keyword-only recall requires at least a third of query terms.
	VectorThreshold  = 0.48
	KeywordThreshold = 0.34
	VectorWeight     = 0.7
	RecallLimit      = 6
	RecallRunes      = 600
	CoreSoftLimit    = 1500
	CoreHardLimit    = 3000
)

// Terms uses overlapping Han bigrams and whole Latin/number words, deduplicated.
func Terms(text string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	rs := []rune(strings.ToLower(text))
	for i := 0; i < len(rs); {
		start := i
		if unicode.Is(unicode.Han, rs[i]) {
			for i < len(rs) && unicode.Is(unicode.Han, rs[i]) {
				i++
			}
			if i-start == 1 {
				add(string(rs[start]))
			} else {
				for j := start; j < i-1; j++ {
					add(string(rs[j : j+2]))
				}
			}
		} else if unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i]) {
			for i < len(rs) && !unicode.Is(unicode.Han, rs[i]) && (unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i])) {
				i++
			}
			add(string(rs[start:i]))
		} else {
			i++
		}
	}
	return out
}
func KeywordScore(query, text string) float64 {
	terms := Terms(query)
	if len(terms) == 0 {
		return 0
	}
	text = strings.ToLower(text)
	n := 0
	for _, t := range terms {
		if strings.Contains(text, t) {
			n++
		}
	}
	return float64(n) / float64(len(terms))
}
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, aa, bb float64
	for i, x := range a {
		y := float64(b[i])
		v := float64(x)
		dot += v * y
		aa += v * v
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	v := dot / math.Sqrt(aa*bb)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
func Score(query string, vector []float32, model string, m *store.Memory) (float64, bool) {
	k := KeywordScore(query, m.Content+" "+m.Keywords)
	if len(vector) > 0 && m.EmbeddingModel == model && len(m.Embedding) == len(vector) {
		v := VectorWeight*Cosine(vector, m.Embedding) + (1-VectorWeight)*k
		return v, v >= VectorThreshold
	}
	return k, k >= KeywordThreshold
}
func rank(query string, vector []float32, model string, ms []*store.Memory, limit int, threshold bool) []*store.Memory {
	type scored struct {
		m *store.Memory
		s float64
	}
	items := []scored{}
	for _, m := range ms {
		s, ok := Score(query, vector, model, m)
		if ok || !threshold {
			items = append(items, scored{m, s})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].s > items[j].s })
	out := []*store.Memory{}
	for _, v := range items {
		if len(out) >= limit {
			break
		}
		out = append(out, v.m)
	}
	return out
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
