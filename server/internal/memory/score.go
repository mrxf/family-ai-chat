package memory

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"nuan/internal/store"
)

const (
	SemanticThreshold = 0.42
	VectorThreshold   = SemanticThreshold
	KeywordThreshold  = 0.85 // calibrated BM25 gate for standalone lexical matches
	RecallLimit       = 6
	RecallRunes       = 780
	CoreSoftLimit     = 1500
	CoreHardLimit     = 3000
)

// Pure grammatical/clause particles split Han runs so cross-boundary noise
// bigrams like "的柯" or "基吗" are never generated.
var clauseParticles = runeSet("的了吗呢吧啊呀哦嘛哈呐地得着")

// High-frequency function characters excluded from standalone 1-gram matching
// and pure-stopword bigrams.
var stopRunes = runeSet("的了吗呢吧啊呀哦嘛哈呐地得着了是在有和与及或而但因所就都也还又很太更最只才把被给让对向从到于过我你他她它们这那哪谁啥什怎几多少个位条次件点些上下左右前后里外能会要可以想说问聊讲看做没不别非未已正再先去来")

// Generic modifiers, quantifiers and calendar units may participate in 2-grams
// (e.g. "高血压", "5岁") but never trigger standalone 1-gram hits on their own
// (preventing "高兴" from matching "高血压" on "高").
var weakUnigrams = runeSet("大小好坏高低新老多少长短快慢早晚真假全半次天年月日时分秒岁号个人家事话询问讨论用户小暖")

// Conversational meta-prefixes stripped from user search queries so polite
// framing never dilutes lexical matching.
var queryFillerPhrases = []string{
	"你还记得", "还记得", "记不记得", "你记得", "想起来",
	"上次聊的", "上次说的", "之前聊的", "之前说的", "以前聊的", "以前说的",
	"聊过的事", "是怎么回事", "怎么回事", "还在继续", "怎么样了",
	"为什么", "为啥", "怎么办", "告诉我", "帮我看看", "请问",
}

// Colloquial <-> clinical/breed synonym clusters common in family & pet chats.
// Used for lightweight local expansion even when no embedding model is configured.
var synonymClusters = [][]string{
	{"小狗", "狗狗", "毛孩子", "汪星人", "宠物狗", "犬", "柯基", "金毛", "泰迪", "柴犬", "边牧", "萨摩耶", "哈士奇", "拉布拉多", "比熊", "法斗", "博美", "贵宾", "雪纳瑞"},
	{"小猫", "猫咪", "喵星人", "宠物猫", "英短", "美短", "布偶", "橘猫", "暹罗", "狸花", "蓝猫"},
	{"痒", "痒痒", "发痒", "抓挠", "瘙痒", "皮炎", "湿疹", "红疹", "皮肤病"},
	{"看病", "去医院", "看医生", "就医", "复诊", "复查", "门诊", "宠物医院"},
	{"药澡", "洗药澡", "药浴"},
	{"拉肚子", "窜稀", "软便", "腹泻", "肠胃炎"},
	{"想吐", "恶心", "呕吐", "反酸"},
	{"发烧", "退烧", "发热", "体温"},
	{"头疼", "头痛", "偏头痛"},
	{"血压高", "降压", "降压药", "高血压", "血压"},
	{"血糖高", "降糖", "降糖药", "糖尿病", "血糖"},
	{"血脂高", "降脂", "高血脂", "他汀"},
}

var synonymLookup = func() map[string][]string {
	m := map[string][]string{}
	for _, cluster := range synonymClusters {
		for _, term := range cluster {
			m[term] = cluster
		}
	}
	return m
}()

func runeSet(s string) map[rune]bool {
	m := make(map[rune]bool, len(s))
	for _, r := range s {
		m[r] = true
	}
	return m
}

type Token struct {
	Text   string
	Weight float64
	Bigram bool // 2-gram or whole Latin/digit token (higher precision than 1-gram)
	Strong bool // domain-bearing token eligible to anchor a lexical match
}

type WeightedQuery struct {
	Text   string
	Weight float64
}

// cleanDocContent strips leading episodic date prefixes and repetitive "用户..."
// boilerplate so neither BM25 length normalization nor embeddings waste weight on them.
func cleanDocContent(s string) string {
	s = strings.TrimSpace(episodeDate.ReplaceAllString(strings.TrimSpace(s), ""))
	for _, p := range []string{"用户询问", "用户请小暖", "用户向小暖", "用户和家人", "用户的", "用户家", "用户"} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimSpace(strings.TrimPrefix(s, p))
			break
		}
	}
	return s
}

// EmbedText builds the text sent to the embedding model for a memory row,
// stripping date boilerplate and appending synonym keywords when available.
func EmbedText(m *store.Memory) string {
	body := cleanDocContent(m.Content)
	if body == "" {
		body = strings.TrimSpace(m.Content)
	}
	kw := strings.TrimSpace(m.Keywords)
	if kw == "" {
		return body
	}
	return body + " (" + kw + ")"
}

func cleanQueryText(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	for _, f := range queryFillerPhrases {
		q = strings.ReplaceAll(q, f, " ")
	}
	return q
}

// AnalyzeCJK extracts dual-granularity tokens (filtered Han 2-grams, strong Han
// 1-grams with reduplication boost, and Latin/number words).
func AnalyzeCJK(text string, isQuery bool) []Token {
	raw := strings.ToLower(strings.TrimSpace(text))
	if isQuery {
		raw = cleanQueryText(raw)
	}
	var out []Token
	idx := map[string]int{}
	emit := func(tok Token) {
		if tok.Text == "" {
			return
		}
		if pos, ok := idx[tok.Text]; ok {
			if tok.Weight > out[pos].Weight {
				out[pos].Weight = tok.Weight
			}
			out[pos].Bigram = out[pos].Bigram || tok.Bigram
			out[pos].Strong = out[pos].Strong || tok.Strong
			return
		}
		idx[tok.Text] = len(out)
		out = append(out, tok)
	}

	rs := []rune(raw)
	for i := 0; i < len(rs); {
		if unicode.Is(unicode.Han, rs[i]) {
			start := i
			for i < len(rs) && unicode.Is(unicode.Han, rs[i]) {
				i++
			}
			analyzeHanRun(rs[start:i], emit)
		} else if unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i]) {
			start := i
			allDigits := true
			for i < len(rs) && !unicode.Is(unicode.Han, rs[i]) && (unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i])) {
				if !unicode.IsDigit(rs[i]) {
					allDigits = false
				}
				i++
			}
			word := string(rs[start:i])
			// 4-digit calendar years appear in almost every dated memory; downweight them.
			if allDigits && len(word) == 4 && strings.HasPrefix(word, "20") {
				emit(Token{Text: word, Weight: 0.2, Bigram: true, Strong: false})
			} else {
				emit(Token{Text: word, Weight: 1.0, Bigram: true, Strong: true})
			}
		} else {
			i++
		}
	}
	return out
}

func analyzeHanRun(run []rune, emit func(Token)) {
	// Split on pure clause particles so "说的柯基吗" yields sub-runs ["说", "柯基"].
	subStart := 0
	for i := 0; i <= len(run); i++ {
		if i == len(run) || clauseParticles[run[i]] {
			sub := run[subStart:i]
			if len(sub) == 1 {
				r := sub[0]
				if !stopRunes[r] && !weakUnigrams[r] {
					emit(Token{Text: string(r), Weight: 0.55, Strong: true})
				}
			} else if len(sub) > 1 {
				for j := 0; j < len(sub)-1; j++ {
					a, b := sub[j], sub[j+1]
					if a == b && !stopRunes[a] && !weakUnigrams[a] {
						// Colloquial reduplication ("痒痒", "狗狗") strongly signals a core concept.
						emit(Token{Text: string(a), Weight: 0.72, Strong: true})
						emit(Token{Text: string(sub[j : j+2]), Weight: 1.0, Bigram: true, Strong: true})
						continue
					}
					if stopRunes[a] && stopRunes[b] {
						continue
					}
					strong := !stopRunes[a] || !stopRunes[b]
					w := 1.0
					if stopRunes[a] || stopRunes[b] {
						w = 0.6
						strong = false
					}
					emit(Token{Text: string(sub[j : j+2]), Weight: w, Bigram: true, Strong: strong})
				}
				for _, r := range sub {
					if !stopRunes[r] && !weakUnigrams[r] {
						emit(Token{Text: string(r), Weight: 0.45, Strong: true})
					}
				}
			}
			subStart = i + 1
		}
	}
}

// Terms returns deduplicated lexical terms for general inspection and tests.
func Terms(text string) []string {
	toks := AnalyzeCJK(text, true)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if t.Strong {
			out = append(out, t.Text)
		}
	}
	return out
}

// SearchTerms extracts high-signal substrings suitable for SQL LIKE pre-filtering
// on raw conversation messages (including local colloquial synonym expansions).
func SearchTerms(queries ...string) []string {
	seen := map[string]bool{}
	var bigrams, unigrams, expanded []string
	add := func(dst *[]string, s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			*dst = append(*dst, s)
		}
	}
	for _, q := range queries {
		for _, tok := range AnalyzeCJK(q, true) {
			if !tok.Strong {
				continue
			}
			if tok.Bigram {
				add(&bigrams, tok.Text)
			} else {
				add(&unigrams, tok.Text)
			}
			if syn, ok := synonymLookup[tok.Text]; ok {
				for _, s := range syn {
					add(&expanded, s)
				}
			}
		}
	}
	out := append(bigrams, unigrams...)
	out = append(out, expanded...)
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

type docIndex struct {
	tf     map[string]float64
	length float64
}

func buildDocIndex(content, keywords string) docIndex {
	tf := map[string]float64{}
	var length float64
	addField := func(text string, fieldBoost float64) {
		for _, tok := range AnalyzeCJK(text, false) {
			v := fieldBoost * tok.Weight
			tf[tok.Text] += v
			if tok.Bigram {
				length += 1.0
			} else {
				length += 0.35
			}
		}
	}
	addField(cleanDocContent(content), 1.0)
	if strings.TrimSpace(keywords) != "" {
		addField(keywords, 1.5)
	}
	if length < 4 {
		length = 4
	}
	return docIndex{tf: tf, length: length}
}

// expandQueryTokens enriches query tokens with matching local synonym clusters
// whose targets exist in corpusVocab (avoiding phantom terms).
func expandQueryTokens(queries []WeightedQuery, corpusVocab map[string]int) map[string]Token {
	merged := map[string]Token{}
	put := func(tok Token) {
		if prev, ok := merged[tok.Text]; ok {
			if tok.Weight > prev.Weight {
				prev.Weight = tok.Weight
			}
			prev.Bigram = prev.Bigram || tok.Bigram
			prev.Strong = prev.Strong || tok.Strong
			merged[tok.Text] = prev
			return
		}
		merged[tok.Text] = tok
	}
	for _, q := range queries {
		if q.Weight <= 0 || strings.TrimSpace(q.Text) == "" {
			continue
		}
		toks := AnalyzeCJK(q.Text, true)
		for _, tok := range toks {
			scaled := tok
			scaled.Weight *= q.Weight
			put(scaled)
			if !tok.Strong {
				continue
			}
			if cluster, ok := synonymLookup[tok.Text]; ok {
				for _, alt := range cluster {
					if alt == tok.Text {
						continue
					}
					for _, st := range AnalyzeCJK(alt, false) {
						if !st.Strong {
							continue
						}
						if corpusVocab != nil && corpusVocab[st.Text] == 0 {
							continue
						}
						st.Weight *= q.Weight * 0.78
						put(st)
					}
				}
			}
		}
	}
	return merged
}

// RankTextDocs scores plain text documents (such as historical messages in the same conversation)
// against unweighted query strings using the CJK bigram + colloquial synonym BM25 engine.
func RankTextDocs(queries []string, texts []string) ([]float64, []bool) {
	if len(queries) == 0 || len(texts) == 0 {
		return make([]float64, len(texts)), make([]bool, len(texts))
	}
	wqs := make([]WeightedQuery, 0, len(queries))
	for _, q := range queries {
		if q = strings.TrimSpace(q); q != "" {
			wqs = append(wqs, WeightedQuery{Text: q, Weight: 1.0})
		}
	}
	docs := make([]docIndex, len(texts))
	for i, t := range texts {
		docs[i] = buildDocIndex(t, "")
	}
	return bm25Corpus(wqs, docs)
}

// bm25Corpus scores a slice of documents against weighted queries using dynamic
// smoothed IDF (virtual corpus floor N_eff >= 12 so 1-memory accounts stay calibrated).
func bm25Corpus(queries []WeightedQuery, docs []docIndex) ([]float64, []bool) {
	scores, passed, _ := bm25Detailed(queries, docs)
	return scores, passed
}

func bm25Detailed(queries []WeightedQuery, docs []docIndex) ([]float64, []bool, []int) {
	scores := make([]float64, len(docs))
	passed := make([]bool, len(docs))
	strongBigrams := make([]int, len(docs))
	if len(docs) == 0 || len(queries) == 0 {
		return scores, passed, strongBigrams
	}
	df := map[string]int{}
	totalLen := 0.0
	for _, d := range docs {
		totalLen += d.length
		for term := range d.tf {
			df[term]++
		}
	}
	avgLen := totalLen / float64(len(docs))
	if avgLen < 6 {
		avgLen = 6
	}
	qTokens := expandQueryTokens(queries, df)
	if len(qTokens) == 0 {
		return scores, passed, strongBigrams
	}
	nEff := math.Max(float64(len(docs)), 12.0)
	const (
		k1 = 1.2
		b  = 0.45
	)
	for i, d := range docs {
		score := 0.0
		bigramHits := 0
		strongHits := 0
		for term, qt := range qTokens {
			f := d.tf[term]
			if f <= 0 {
				continue
			}
			docFreq := float64(df[term])
			idf := math.Log(1.0 + (nEff-docFreq+0.5)/(docFreq+0.5))
			tfNorm := (f * (k1 + 1.0)) / (f + k1*(1.0-b+b*(d.length/avgLen)))
			contrib := qt.Weight * idf * tfNorm
			score += contrib
			if qt.Strong && idf >= 0.85 {
				strongHits++
				if qt.Bigram && qt.Weight >= 0.45 {
					bigramHits++
				}
			}
		}
		scores[i] = score
		strongBigrams[i] = bigramHits
		passed[i] = bigramHits > 0 || (strongHits > 0 && score >= KeywordThreshold)
	}
	return scores, passed, strongBigrams
}

// KeywordScore evaluates a single text against query using the calibrated BM25 scorer.
func KeywordScore(query, text string) float64 {
	s, _ := bm25Corpus([]WeightedQuery{{Text: query, Weight: 1.0}}, []docIndex{buildDocIndex(text, "")})
	if len(s) == 0 {
		return 0
	}
	return s[0]
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

// Score evaluates a single memory against a query and optional query vector.
// Semantic and lexical channels each have an independent gate so synonyms (k=0)
// are never penalized by lexical absence and rare drug names are never dropped.
func Score(query string, vector []float32, model string, m *store.Memory) (float64, bool) {
	ranked := rankScored([]WeightedQuery{{Text: query, Weight: 1.0}}, vector, model, []*store.Memory{m}, 1, false)
	if len(ranked) == 0 {
		return 0, false
	}
	return ranked[0].score, ranked[0].ok
}

type scoredMemory struct {
	m     *store.Memory
	score float64
	ok    bool
}

func rank(query string, vector []float32, model string, ms []*store.Memory, limit int, threshold bool) []*store.Memory {
	return rankWeighted([]WeightedQuery{{Text: query, Weight: 1.0}}, vector, model, ms, limit, threshold)
}

func rankWeighted(queries []WeightedQuery, vector []float32, model string, ms []*store.Memory, limit int, threshold bool) []*store.Memory {
	items := rankScored(queries, vector, model, ms, limit, threshold)
	out := make([]*store.Memory, 0, len(items))
	for _, it := range items {
		out = append(out, it.m)
	}
	return out
}

type rankOptions struct {
	threshold     bool
	ageGate       bool
	nowMs         int64
	retrospective bool
}

func rankScored(queries []WeightedQuery, vector []float32, model string, ms []*store.Memory, limit int, threshold bool) []scoredMemory {
	return rankWithOptions(queries, vector, model, ms, limit, rankOptions{
		threshold: threshold,
		nowMs:     time.Now().UnixMilli(),
	})
}

// rankForInject applies Ebbinghaus age-gated thresholds for silent per-turn
// auto-injection so >30-day episodic chatter is not resurfaced on casual word collisions.
func rankForInject(queries []WeightedQuery, vector []float32, model string, ms []*store.Memory, limit int, nowMs int64, retrospective bool) []*store.Memory {
	items := rankWithOptions(queries, vector, model, ms, limit, rankOptions{
		threshold:     true,
		ageGate:       true,
		nowMs:         nowMs,
		retrospective: retrospective,
	})
	out := make([]*store.Memory, 0, len(items))
	distantCount := 0
	for _, it := range items {
		isTimeBound := it.m.Category == "episode" || it.m.Category == "ongoing"
		if isTimeBound && !retrospective && calendarAgeDays(memoryEventAt(it.m), nowMs) > 30 {
			if distantCount >= 2 {
				continue
			}
			distantCount++
		}
		out = append(out, it.m)
	}
	return out
}

func rankWithOptions(queries []WeightedQuery, vector []float32, model string, ms []*store.Memory, limit int, opts rankOptions) []scoredMemory {
	if len(ms) == 0 || limit <= 0 {
		return nil
	}
	docs := make([]docIndex, len(ms))
	for i, m := range ms {
		docs[i] = buildDocIndex(m.Content, m.Keywords)
	}
	lexScores, lexOK, strongBigrams := bm25Detailed(queries, docs)

	cosScores := make([]float64, len(ms))
	vecOK := make([]bool, len(ms))
	hasVec := make([]bool, len(ms))
	if len(vector) > 0 {
		for i, m := range ms {
			if m.EmbeddingModel == model && len(m.Embedding) == len(vector) {
				hasVec[i] = true
				c := Cosine(vector, m.Embedding)
				cosScores[i] = c
				vecOK[i] = c >= SemanticThreshold
			}
		}
	}

	// Compute 1-based ranks per channel for Reciprocal Rank Fusion (RRF).
	lexRank := make([]int, len(ms))
	var lexIdx []int
	for i, s := range lexScores {
		if s > 0 {
			lexIdx = append(lexIdx, i)
		}
	}
	sort.SliceStable(lexIdx, func(a, b int) bool { return lexScores[lexIdx[a]] > lexScores[lexIdx[b]] })
	for r, idx := range lexIdx {
		lexRank[idx] = r + 1
	}

	vecRank := make([]int, len(ms))
	var vecIdx []int
	for i, ok := range hasVec {
		if ok && cosScores[i] > 0 {
			vecIdx = append(vecIdx, i)
		}
	}
	sort.SliceStable(vecIdx, func(a, b int) bool { return cosScores[vecIdx[a]] > cosScores[vecIdx[b]] })
	for r, idx := range vecIdx {
		vecRank[idx] = r + 1
	}

	nowMs := opts.nowMs
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	const dayFloat = 24 * 3600 * 1000.0

	var items []scoredMemory
	for i, m := range ms {
		ok := vecOK[i] || lexOK[i]
		isTimeBound := m.Category == "episode" || m.Category == "ongoing"
		evMs := memoryEventAt(m)
		calAgeDays := calendarAgeDays(evMs, nowMs)

		if opts.ageGate && isTimeBound && calAgeDays > 7 {
			if calAgeDays <= 30 || opts.retrospective {
				// Cooling window (8-30d) or explicit retrospective query ("上次/之前"):
				// reject single accidental 2-char word collisions; require multi-bigram or solid semantic match.
				vecPass := hasVec[i] && cosScores[i] >= 0.50
				lexPass := strongBigrams[i] >= 2 || lexScores[i] >= 4.0
				ok = vecPass || lexPass
			} else {
				// Distant past (>30d) in ordinary forward-looking chat (no retrospective cue):
				// stay silent by default so 30+ day old episodes never hijack fresh topics;
				// only allow near-identical semantic + multi-bigram matches when vectors exist.
				ok = hasVec[i] && cosScores[i] >= 0.75 && strongBigrams[i] >= 2
			}
		}

		if opts.threshold && !ok {
			continue
		}
		rrf := 0.0
		if vecRank[i] > 0 {
			rrf += 1.0/float64(20+vecRank[i]) + 0.15*cosScores[i]
		}
		if lexRank[i] > 0 {
			rrf += 0.95/float64(20+lexRank[i]) + 0.02*math.Min(lexScores[i], 12.0)
		}
		if rrf > 0 && evMs > 0 {
			ageDays := math.Max(0, float64(nowMs-evMs)/dayFloat)
			rrf += 0.03 * math.Exp(-ageDays/45.0)
			if m.RecallCount > 0 {
				rrf += 0.008 * math.Min(math.Log1p(float64(m.RecallCount)), 2.5)
			}
			if opts.ageGate && isTimeBound && !opts.retrospective && ageDays > 14 {
				rrf *= 0.65 + 0.35*math.Exp(-(ageDays-14)/30.0)
			}
		}
		items = append(items, scoredMemory{m: m, score: rrf, ok: ok})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
