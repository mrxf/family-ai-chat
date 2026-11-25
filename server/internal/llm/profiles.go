package llm

import (
	"fmt"
	"strconv"
	"strings"
)

// Model capability tiers for Auto mode routing.
// Maintained centrally on the server so admins do not have to configure every
// model's relative strength by hand, while admin routing_note always takes precedence.
const (
	TierLite     = 1 // 极速轻量：Gemini Helium/Lithium / GPT Luna·Nano / 7B-8B，秒回极省钱，适合寒暄、基础算术、简单翻译
	TierFast     = 2 // 快速日常：Gemini Hydrogen / Claude Haiku / GPT Terra·Mini / DeepSeek-Flash / Turbo，适合日常聊天、中小学作业、普通问答
	TierBalanced = 3 // 均衡主力：DeepSeek-V3/Chat / Bailu / Kimi-K2 / Qwen-Plus / GLM-4.6 / GPT-4o，综合性价比高
	TierStrong   = 4 // 强力进阶：Gemini Neon·Argon / Claude Sonnet / GPT Sol / DeepSeek-R1 / Kimi-K3 / Qwen3-Max，适合复杂分析与长文
	TierFlagship = 5 // 顶级旗舰：Gemini Carbon / Claude Fable·Opus / GPT Astra / o3，适合严肃医疗、科研物理、硬核推理
)

// Domain tags describing what kinds of family questions a model excels at.
const (
	DomainMedical     = "medical"
	DomainResearch    = "research"
	DomainReasoning   = "reasoning"
	DomainWriting     = "writing"
	DomainLongContext = "long_context"
	DomainCoding      = "coding"
	DomainHomework    = "homework"
	DomainDaily       = "daily"
)

// ModelVersion represents a parsed generation/release number (e.g. Opus 5.5 -> 5.5).
type ModelVersion struct {
	Major    int
	Minor    int
	HasMinor bool
}

// Known reports whether a version number was detected in the model identifier.
func (v ModelVersion) Known() bool { return v.Major > 0 }

// Score converts Major.Minor into a monotonic integer (e.g. 4.6 -> 406, 4.10 -> 410, 5.5 -> 505).
func (v ModelVersion) Score() int {
	if v.Major <= 0 {
		return 0
	}
	return v.Major*100 + v.Minor
}

func (v ModelVersion) String() string {
	if v.Major <= 0 {
		return ""
	}
	if v.HasMinor || v.Minor > 0 {
		return fmt.Sprintf("%d.%d", v.Major, v.Minor)
	}
	return strconv.Itoa(v.Major)
}

// ModelProfile is the server's built-in knowledge about a model's family,
// hierarchy rank, version, and best-suited scenarios.
type ModelProfile struct {
	Vendor        string       `json:"vendor"`
	FamilyKey     string       `json:"family_key"`     // groups versions of the same line, e.g. "claude:opus", "openai:fable"
	SeriesLabel   string       `json:"series_label"`   // human-readable family name
	HierarchyHint string       `json:"hierarchy_hint"` // relative standing within vendor, e.g. "强于同代 Sonnet / Haiku"
	Tier          int          `json:"tier"`           // TierLite(1) .. TierFlagship(5)
	SubRank       int          `json:"sub_rank"`       // 1..100 fine ordering across tiers
	Version       ModelVersion `json:"version"`
	ParamBillions int          `json:"param_billions,omitempty"`
	Strengths     string       `json:"strengths"` // concise scenario guide
	Domains       []string     `json:"domains"`
}

// TierLabel returns a family-friendly tier badge in Chinese.
func TierLabel(tier int) string {
	switch tier {
	case TierFlagship:
		return "顶级旗舰"
	case TierStrong:
		return "强力进阶"
	case TierBalanced:
		return "均衡主力"
	case TierFast:
		return "快速日常"
	case TierLite:
		return "极速轻量"
	default:
		return "通用对话"
	}
}

// VendorPriority defines the default preference among same-tier models:
// Google Gemini > Anthropic Claude > OpenAI GPT > other vendors.
func VendorPriority(vendor string) int {
	switch strings.ToLower(strings.TrimSpace(vendor)) {
	case "google":
		return 3
	case "anthropic":
		return 2
	case "openai":
		return 1
	default:
		return 0
	}
}

// RankScore produces a composite capability score combining tier, sub-rank and version,
// so Opus > Sonnet > Haiku, Opus 5.5 > 5.0 > 4.7 > 4.6, and same-tier Gemini > Anthropic > OpenAI hold deterministically.
func (p ModelProfile) RankScore() int {
	return p.Tier*1_000_000 + p.SubRank*10_000 + min(p.Version.Score(), 9999)
}

// Summary formats the server's built-in cognition for admin display and router prompts.
func (p ModelProfile) Summary() string {
	var parts []string
	head := p.SeriesLabel
	if v := p.Version.String(); v != "" {
		head += " v" + v
	}
	head += "（" + TierLabel(p.Tier)
	if p.HierarchyHint != "" {
		head += "，" + p.HierarchyHint
	}
	head += "）"
	parts = append(parts, head)
	if p.Strengths != "" {
		parts = append(parts, "适合："+p.Strengths)
	}
	return strings.Join(parts, " · ")
}

// HasDomain checks whether the profile includes a domain tag.
func (p ModelProfile) HasDomain(d string) bool {
	for _, x := range p.Domains {
		if x == d {
			return true
		}
	}
	return false
}

// ProfileModel infers the built-in ModelProfile from an upstream model ID and optional display name.
func ProfileModel(upstreamID, displayName string) ModelProfile {
	norm := normalizeModelKey(upstreamID)
	disp := normalizeModelKey(displayName)
	combined := norm
	if disp != "" && disp != norm {
		combined = norm + " " + disp
	}
	params := parseParamBillions(norm)
	if params == 0 && disp != "" {
		params = parseParamBillions(disp)
	}

	// 1. Anthropic / Claude family (御三家之一): Fable/Mythos > Opus > Sonnet > Haiku, higher version > lower version.
	if hasWord(combined, "claude", "anthropic", "fable", "mythos", "opus", "sonnet", "haiku") {
		ver := extractFamilyVersion(norm, "claude", "fable", "mythos", "opus", "sonnet", "haiku")
		if !ver.Known() && disp != "" {
			ver = extractFamilyVersion(disp, "claude", "fable", "mythos", "opus", "sonnet", "haiku")
		}
		switch {
		case strings.Contains(combined, "fable") || strings.Contains(combined, "mythos"):
			label := "Claude Fable"
			fk := "claude:fable"
			if strings.Contains(combined, "mythos") {
				label = "Claude Mythos"
				fk = "claude:mythos"
			}
			return ModelProfile{
				Vendor: "anthropic", FamilyKey: fk, SeriesLabel: label,
				HierarchyHint: "Anthropic 超旗舰深度推理模型，强于 Opus / Sonnet / Haiku",
				Tier:          TierFlagship, SubRank: 95, Version: ver,
				Strengths: "超长链路深度推理、硬核科研与物理数理推导、疑难问题拆解与医疗/法律专业分析",
				Domains:   []string{DomainReasoning, DomainResearch, DomainMedical, DomainWriting, DomainCoding},
			}
		case strings.Contains(combined, "opus"):
			return ModelProfile{
				Vendor: "anthropic", FamilyKey: "claude:opus", SeriesLabel: "Claude Opus",
				HierarchyHint: "强于 Sonnet 与 Haiku，同系版本越高越强",
				Tier:          TierFlagship, SubRank: 93, Version: ver,
				Strengths: "复杂深度推理、疑难问题拆解、严肃长文写作与润色、医疗/法律/技术方案分析",
				Domains:   []string{DomainReasoning, DomainWriting, DomainResearch, DomainMedical, DomainCoding},
			}
		case strings.Contains(combined, "haiku"):
			return ModelProfile{
				Vendor: "anthropic", FamilyKey: "claude:haiku", SeriesLabel: "Claude Haiku",
				HierarchyHint: "轻量高速版，成本低速度快",
				Tier:          TierFast, SubRank: 54, Version: ver,
				Strengths: "日常简短问答、快速翻译、文字整理与简单闲聊",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		default: // sonnet or generic claude
			return ModelProfile{
				Vendor: "anthropic", FamilyKey: "claude:sonnet", SeriesLabel: "Claude Sonnet",
				HierarchyHint: "主力均衡版，强于 Haiku，次于 Opus",
				Tier:          TierStrong, SubRank: 82, Version: ver,
				Strengths: "自然细腻的中文写作、日常复杂问答、文章总结、工作邮件与逻辑分析",
				Domains:   []string{DomainWriting, DomainReasoning, DomainCoding, DomainDaily},
			}
		}
	}

	// 2. Google / Gemini & Gemma family (御三家之一 + 开源):
	// Gemini 4 元素周期表: Carbon > Neon > Argon > Hydrogen > Helium/Lithium (兼容旧代 Ultra > Pro > Flash > Lite).
	if hasWord(combined, "gemini", "carbon", "neon", "argon", "hydrogen", "helium", "lithium") {
		ver := extractFamilyVersion(norm, "gemini", "carbon", "neon", "argon", "hydrogen", "helium", "lithium")
		if !ver.Known() && disp != "" {
			ver = extractFamilyVersion(disp, "gemini", "carbon", "neon", "argon", "hydrogen", "helium", "lithium")
		}
		switch {
		case strings.Contains(combined, "carbon"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:carbon", SeriesLabel: "Gemini Carbon",
				HierarchyHint: "Google 顶级旗舰模型，强于 Neon / Argon / Hydrogen",
				Tier:          TierFlagship, SubRank: 97, Version: ver,
				Strengths: "医疗健康问题、体检报告与药品解读、深度科学研究、物理数理推导与复杂图片分析",
				Domains:   []string{DomainMedical, DomainResearch, DomainReasoning, DomainLongContext, DomainCoding},
			}
		case strings.Contains(combined, "ultra"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:ultra", SeriesLabel: "Gemini Ultra",
				HierarchyHint: "Google 旗舰模型（旧代命名）",
				Tier:          TierFlagship, SubRank: 96, Version: ver,
				Strengths: "高难度多模态理解、科学研究与复杂推理",
				Domains:   []string{DomainResearch, DomainReasoning, DomainMedical},
			}
		case strings.Contains(combined, "neon"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:neon", SeriesLabel: "Gemini Neon",
				HierarchyHint: "Google 强力进阶主力模型，兼顾深度推理与速度，次于 Carbon，强于 Hydrogen",
				Tier:          TierStrong, SubRank: 86, Version: ver,
				Strengths: "复杂知识问答、深度分析、长文档解读、多模态看图、写作润色与编程推理",
				Domains:   []string{DomainReasoning, DomainResearch, DomainWriting, DomainLongContext, DomainMedical, DomainCoding},
			}
		case strings.Contains(combined, "argon"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:argon", SeriesLabel: "Gemini Argon",
				HierarchyHint: "Google 高效推理模型，次于 Carbon / Neon，强于 Hydrogen",
				Tier:          TierStrong, SubRank: 84, Version: ver,
				Strengths: "高性价比深度推理、长资料解读与复杂综合问答",
				Domains:   []string{DomainReasoning, DomainResearch, DomainLongContext},
			}
		case strings.Contains(combined, "hydrogen") || strings.Contains(combined, "氢气"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:hydrogen", SeriesLabel: "Gemini Hydrogen",
				HierarchyHint: "Google 高速高性价比日常模型，次于 Carbon / Neon，适合高频日常使用",
				Tier:          TierFast, SubRank: 57, Version: ver,
				Strengths: "日常问答、拍照识图、中小学数学题与作业辅导、生活常识、菜谱与快速翻译",
				Domains:   []string{DomainDaily, DomainHomework, DomainLongContext},
			}
		case strings.Contains(combined, "helium") || strings.Contains(combined, "lithium"):
			label := "Gemini Helium"
			fk := "gemini:helium"
			if strings.Contains(combined, "lithium") {
				label = "Gemini Lithium"
				fk = "gemini:lithium"
			}
			return ModelProfile{
				Vendor: "google", FamilyKey: fk, SeriesLabel: label,
				HierarchyHint: "Google 极速超轻量元素模型，响应极快成本极低",
				Tier:          TierLite, SubRank: 35, Version: ver,
				Strengths: "快问快答、日常闲聊、基础翻译与简单常识",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		case strings.Contains(combined, "flash-lite") || strings.Contains(combined, "flash lite") ||
			strings.Contains(combined, "-lite") || strings.Contains(combined, "-nano"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:lite", SeriesLabel: "Gemini Lite",
				HierarchyHint: "超轻量极速版（旧代命名）",
				Tier:          TierLite, SubRank: 32, Version: ver,
				Strengths: "快问快答、日常闲聊、基础翻译与简单常识",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		case strings.Contains(combined, "flash"):
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:flash", SeriesLabel: "Gemini Flash",
				HierarchyHint: "高速高性价比多模态版（旧代命名）",
				Tier:          TierFast, SubRank: 56, Version: ver,
				Strengths: "拍照识图、日常问答、孩子中小学作业辅导、旅游菜谱与长文档速读",
				Domains:   []string{DomainDaily, DomainHomework, DomainLongContext},
			}
		default: // pro or standard gemini
			return ModelProfile{
				Vendor: "google", FamilyKey: "gemini:pro", SeriesLabel: "Gemini Pro",
				HierarchyHint: "Google 强力全能模型（旧代命名）",
				Tier:          TierStrong, SubRank: 83, Version: ver,
				Strengths: "复杂知识问答、多模态看图分析、长资料解读、数理与健康科普",
				Domains:   []string{DomainResearch, DomainReasoning, DomainLongContext, DomainMedical},
			}
		}
	}

	if strings.Contains(combined, "gemma") {
		ver := extractFamilyVersion(norm, "gemma")
		tier, sub := TierLite, 31
		if params >= 20 {
			tier, sub = TierFast, 55
		}
		return ModelProfile{
			Vendor: "google", FamilyKey: "google:gemma", SeriesLabel: "Gemma",
			HierarchyHint: "Google 开源轻量模型",
			Tier:          tier, SubRank: sub, Version: ver, ParamBillions: params,
			Strengths: "日常轻量对话、简单问答与基础翻译",
			Domains:   []string{DomainDaily, DomainHomework},
		}
	}

	// 3. OpenAI / GPT-6 (Astra > Sol > Terra > Luna) & legacy o-series family (御三家之一).
	if reOSeriesVersion.MatchString(norm) {
		m := reOSeriesVersion.FindStringSubmatch(norm)
		maj, _ := strconv.Atoi(m[1])
		minV, _ := strconv.Atoi(m[2])
		ver := ModelVersion{Major: maj, Minor: minV, HasMinor: m[2] != ""}
		if strings.Contains(combined, "mini") {
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:o-mini", SeriesLabel: "OpenAI o-Mini",
				HierarchyHint: "轻量快速推理模型",
				Tier:          TierBalanced, SubRank: 64, Version: ver,
				Strengths: "数学题解答、逻辑思考、理科作业分步辅导与编程问题",
				Domains:   []string{DomainReasoning, DomainHomework, DomainCoding},
			}
		}
		tier, sub := TierStrong, 79
		if strings.Contains(combined, "pro") || maj >= 3 {
			tier, sub = TierFlagship, 89
		}
		return ModelProfile{
			Vendor: "openai", FamilyKey: "openai:o", SeriesLabel: "OpenAI o 系列",
			HierarchyHint: "强推理旗舰，强于 mini 版",
			Tier:          tier, SubRank: sub, Version: ver,
			Strengths: "高难度数理逻辑、复杂问题推演、科研与专业技术分析",
			Domains:   []string{DomainReasoning, DomainResearch, DomainCoding},
		}
	}

	if strings.Contains(combined, "gpt-oss") {
		tier, sub := TierFast, 49
		if params >= 70 {
			tier, sub = TierBalanced, 65
		}
		return ModelProfile{
			Vendor: "openai", FamilyKey: "openai:gpt-oss", SeriesLabel: "GPT-OSS",
			HierarchyHint: "OpenAI 开源权重模型",
			Tier:          tier, SubRank: sub, ParamBillions: params,
			Strengths: "日常综合问答、理科辅导与通用写作",
			Domains:   []string{DomainDaily, DomainHomework, DomainReasoning},
		}
	}

	if hasWord(combined, "gpt", "chatgpt", "astra") ||
		strings.Contains(norm, "-sol") || strings.Contains(norm, "-terra") || strings.Contains(norm, "-luna") {
		ver := extractFamilyVersion(norm, "chatgpt", "gpt", "astra", "sol", "terra", "luna")
		if !ver.Known() && disp != "" {
			ver = extractFamilyVersion(disp, "chatgpt", "gpt", "astra", "sol", "terra", "luna")
		}
		switch {
		case strings.Contains(combined, "astra"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:astra", SeriesLabel: "GPT Astra",
				HierarchyHint: "OpenAI 顶级全能旗舰，强于 Sol / Terra / Luna",
				Tier:          TierFlagship, SubRank: 91, Version: ver,
				Strengths: "高难度综合推理、跨学科疑难解答、深度研究分析与高质量创作",
				Domains:   []string{DomainReasoning, DomainResearch, DomainWriting, DomainMedical, DomainCoding},
			}
		case strings.Contains(combined, "-pro") || strings.HasSuffix(combined, " pro"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:pro", SeriesLabel: "GPT Pro",
				HierarchyHint: "OpenAI 深度思考旗舰",
				Tier:          TierFlagship, SubRank: 90, Version: ver,
				Strengths: "复杂推理、专业难题、严谨方案规划与深度研究（稍慢但最准）",
				Domains:   []string{DomainReasoning, DomainResearch, DomainMedical, DomainCoding},
			}
		case strings.Contains(combined, "-sol") || strings.HasSuffix(combined, " sol") || strings.Contains(combined, " sol "):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:sol", SeriesLabel: "GPT Sol",
				HierarchyHint: "OpenAI 强力主力模型，次于 Astra，强于 Terra / Luna",
				Tier:          TierStrong, SubRank: 80, Version: ver,
				Strengths: "复杂文档分析、专业问答、日常编程与高质量写作（性价比高）",
				Domains:   []string{DomainReasoning, DomainWriting, DomainResearch, DomainCoding, DomainDaily},
			}
		case strings.Contains(combined, "terra"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:terra", SeriesLabel: "GPT Terra",
				HierarchyHint: "OpenAI 高速视觉与日常实用版，次于 Sol，强于 Luna",
				Tier:          TierFast, SubRank: 52, Version: ver,
				Strengths: "日常快速问答、看图识物、作业辅导、生活建议与高频操作任务",
				Domains:   []string{DomainDaily, DomainHomework, DomainWriting},
			}
		case strings.Contains(combined, "luna"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:luna", SeriesLabel: "GPT Luna",
				HierarchyHint: "OpenAI 极速超轻量经济版，速度最快成本最低",
				Tier:          TierLite, SubRank: 30, Version: ver,
				Strengths: "简单闲聊、基础常识、快问快答与轻量文本整理",
				Domains:   []string{DomainDaily},
			}
		case strings.Contains(combined, "nano"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:nano", SeriesLabel: "GPT Nano",
				HierarchyHint: "超轻量极速版（旧代命名）",
				Tier:          TierLite, SubRank: 29, Version: ver,
				Strengths: "简单闲聊、基础常识、简短翻译与快问快答",
				Domains:   []string{DomainDaily},
			}
		case strings.Contains(combined, "mini"):
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:mini", SeriesLabel: "GPT Mini",
				HierarchyHint: "轻量高性价比版（旧代命名）",
				Tier:          TierFast, SubRank: 50, Version: ver,
				Strengths: "日常问答、中小学题目辅导、菜谱生活建议与翻译",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		default:
			tier, sub := TierBalanced, 66
			if ver.Major >= 5 || (ver.Major == 4 && ver.Minor >= 5) {
				tier, sub = TierStrong, 77
			} else if ver.Major > 0 && ver.Major < 4 {
				tier, sub = TierFast, 44
			}
			return ModelProfile{
				Vendor: "openai", FamilyKey: "openai:gpt", SeriesLabel: "GPT",
				HierarchyHint: "OpenAI 通用主力模型，版本越高越强",
				Tier:          tier, SubRank: sub, Version: ver,
				Strengths: "综合知识问答、看图理解、实用写作与日常家庭顾问",
				Domains:   []string{DomainDaily, DomainWriting, DomainReasoning, DomainHomework},
			}
		}
	}

	// 4. DeepSeek family (开源顶级模型): R-series > V-series/Chat > Flash/Distill-small.
	if strings.Contains(combined, "deepseek") {
		ver := ModelVersion{}
		series := "v"
		if m := reDeepSeekSeries.FindStringSubmatch(combined); m != nil {
			series = strings.ToLower(m[1])
			maj, _ := strconv.Atoi(m[2])
			minV, _ := strconv.Atoi(m[3])
			ver = ModelVersion{Major: maj, Minor: minV, HasMinor: m[3] != ""}
		}
		isReasoner := series == "r" || strings.Contains(combined, "reasoner") || strings.Contains(combined, "-r1") || strings.Contains(combined, "-r2")
		isDistillSmall := strings.Contains(combined, "distill") && params > 0 && params <= 32
		switch {
		case strings.Contains(combined, "flash") || strings.Contains(combined, "lite"):
			return ModelProfile{
				Vendor: "deepseek", FamilyKey: "deepseek:flash", SeriesLabel: "DeepSeek Flash",
				HierarchyHint: "极速轻量版，响应快成本极低",
				Tier:          TierFast, SubRank: 50, Version: ver,
				Strengths: "日常闲聊、生活琐事问答、孩子中小学数学与作业题、快速翻译",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		case isReasoner && isDistillSmall:
			return ModelProfile{
				Vendor: "deepseek", FamilyKey: "deepseek:distill", SeriesLabel: "DeepSeek 蒸馏推理版",
				HierarchyHint: "小参数蒸馏推理版，弱于满血 R 系列",
				Tier:          TierFast, SubRank: 54, Version: ver, ParamBillions: params,
				Strengths: "中小学数学题分步解答、基础逻辑题",
				Domains:   []string{DomainHomework, DomainReasoning},
			}
		case isReasoner:
			return ModelProfile{
				Vendor: "deepseek", FamilyKey: "deepseek:r", SeriesLabel: "DeepSeek R 系列",
				HierarchyHint: "开源旗舰深度推理模型，逻辑强于 V/Chat 版",
				Tier:          TierStrong, SubRank: 82, Version: ver, ParamBillions: params,
				Strengths: "数学与物理推导、复杂逻辑分析、深度思考问题与中文长文剖析",
				Domains:   []string{DomainReasoning, DomainResearch, DomainWriting, DomainHomework},
			}
		default:
			return ModelProfile{
				Vendor: "deepseek", FamilyKey: "deepseek:v", SeriesLabel: "DeepSeek",
				HierarchyHint: "开源顶级中文主力模型，高性价比",
				Tier:          TierBalanced, SubRank: 68, Version: ver, ParamBillions: params,
				Strengths: "日常聊天、生活百科、地道中文写作（祝福语/通知/文章）、孩子作业辅导",
				Domains:   []string{DomainDaily, DomainWriting, DomainHomework},
			}
		}
	}

	// 5. Kimi / Moonshot family (开源顶级 / 国产长文本旗舰): K3 > K2.5 > K2 > v1.
	if hasWord(combined, "kimi", "moonshot") {
		ver := ModelVersion{}
		if m := reKimiVersion.FindStringSubmatch(combined); m != nil {
			maj, _ := strconv.Atoi(m[1])
			minV, _ := strconv.Atoi(m[2])
			ver = ModelVersion{Major: maj, Minor: minV, HasMinor: m[2] != ""}
		} else {
			ver = extractFamilyVersion(norm, "kimi", "moonshot")
		}
		tier, sub := TierBalanced, 67
		if ver.Major >= 3 || strings.Contains(combined, "thinking") || strings.Contains(combined, "pro") {
			tier, sub = TierStrong, 80
		} else if strings.Contains(combined, "moonshot-v1") {
			tier, sub = TierFast, 52
		}
		return ModelProfile{
			Vendor: "moonshot", FamilyKey: "kimi:k", SeriesLabel: "Kimi",
			HierarchyHint: "同系列代数越高越强（如 K3 > K2.5 > K2）",
			Tier:          tier, SubRank: sub, Version: ver,
			Strengths: "超长文章与合同/说明书阅读总结、地道中文写作、资料整理与复杂中文问答",
			Domains:   []string{DomainLongContext, DomainWriting, DomainDaily, DomainReasoning},
		}
	}

	// 6. Bailu (白鹭).
	if hasWord(combined, "bailu", "白鹭") {
		ver := extractFamilyVersion(norm, "bailu")
		tier, sub := TierBalanced, 63
		if hasAny(combined, []string{"flash", "lite", "mini", "air"}) {
			tier, sub = TierFast, 49
		} else if hasAny(combined, []string{"pro", "max", "plus"}) {
			tier, sub = TierStrong, 75
		}
		return ModelProfile{
			Vendor: "bailu", FamilyKey: "bailu:chat", SeriesLabel: "Bailu",
			HierarchyHint: "中文亲和对话模型",
			Tier:          tier, SubRank: sub, Version: ver,
			Strengths: "中文日常陪伴聊天、家庭生活咨询、节日祝福与通俗解答",
			Domains:   []string{DomainDaily, DomainWriting, DomainHomework},
		}
	}

	// 7. Qwen / 通义千问 family (开源顶级模型家族).
	if hasWord(combined, "qwen", "qwq", "qvq") {
		ver := extractFamilyVersion(norm, "qwen", "qwq", "qvq")
		switch {
		case strings.Contains(combined, "qwq") || strings.Contains(combined, "qvq"):
			return ModelProfile{
				Vendor: "qwen", FamilyKey: "qwen:qwq", SeriesLabel: "Qwen 推理模型",
				HierarchyHint: "通义千问深度思考版",
				Tier:          TierStrong, SubRank: 79, Version: ver, ParamBillions: params,
				Strengths: "数学逻辑解题、理科推导与分步思考",
				Domains:   []string{DomainReasoning, DomainHomework, DomainCoding},
			}
		case strings.Contains(combined, "max") || params >= 70:
			return ModelProfile{
				Vendor: "qwen", FamilyKey: "qwen:max", SeriesLabel: "Qwen Max / 旗舰开源",
				HierarchyHint: "通义千问大参数旗舰，强于 Plus / Turbo",
				Tier:          TierStrong, SubRank: 78, Version: ver, ParamBillions: params,
				Strengths: "复杂中文问答、数理辅导、多语种翻译与实用写作",
				Domains:   []string{DomainReasoning, DomainWriting, DomainDaily, DomainHomework},
			}
		case hasAny(combined, []string{"turbo", "flash", "lite"}) || (params > 0 && params <= 14):
			tier := TierFast
			if params > 0 && params <= 8 {
				tier = TierLite
			}
			return ModelProfile{
				Vendor: "qwen", FamilyKey: "qwen:turbo", SeriesLabel: "Qwen 轻量高速",
				HierarchyHint: "通义千问极速版，响应快成本低",
				Tier:          tier, SubRank: 46, Version: ver, ParamBillions: params,
				Strengths: "日常闲聊、快问快答、简单作业与翻译",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		default: // plus / 30b / 32b / default qwen
			return ModelProfile{
				Vendor: "qwen", FamilyKey: "qwen:plus", SeriesLabel: "Qwen Plus",
				HierarchyHint: "通义千问均衡主力，介于 Max 与 Turbo 之间",
				Tier:          TierBalanced, SubRank: 65, Version: ver, ParamBillions: params,
				Strengths: "日常综合问答、家庭生活百科、孩子作业讲解与中文文案",
				Domains:   []string{DomainDaily, DomainHomework, DomainWriting},
			}
		}
	}

	// 8. Zhipu GLM (智谱开源/旗舰模型).
	if strings.Contains(combined, "glm") || strings.Contains(combined, "chatglm") {
		ver := extractFamilyVersion(norm, "chatglm", "glm")
		switch {
		case strings.Contains(combined, "flash"):
			return ModelProfile{
				Vendor: "zhipu", FamilyKey: "glm:flash", SeriesLabel: "GLM Flash",
				HierarchyHint: "智谱极速免费/低价轻量版",
				Tier:          TierLite, SubRank: 33, Version: ver,
				Strengths: "日常闲聊、简单常识问答与基础翻译",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		case strings.Contains(combined, "-air"):
			return ModelProfile{
				Vendor: "zhipu", FamilyKey: "glm:air", SeriesLabel: "GLM Air",
				HierarchyHint: "智谱高性价比轻量版",
				Tier:          TierFast, SubRank: 50, Version: ver,
				Strengths: "日常聊天、摘要整理与作业辅导",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		case ver.Major >= 5 || hasAny(combined, []string{"zero", "-z1", "plus"}):
			return ModelProfile{
				Vendor: "zhipu", FamilyKey: "glm:flagship", SeriesLabel: "GLM 旗舰",
				HierarchyHint: "智谱主力旗舰模型，强于 Air / Flash",
				Tier:          TierStrong, SubRank: 76, Version: ver,
				Strengths: "中文深度理解、通知公文与书信写作、逻辑分析与综合问答",
				Domains:   []string{DomainWriting, DomainReasoning, DomainDaily},
			}
		default:
			return ModelProfile{
				Vendor: "zhipu", FamilyKey: "glm:standard", SeriesLabel: "GLM",
				HierarchyHint: "智谱均衡中文模型",
				Tier:          TierBalanced, SubRank: 64, Version: ver,
				Strengths: "中文日常问答、实用写作与总结归纳",
				Domains:   []string{DomainDaily, DomainWriting, DomainHomework},
			}
		}
	}

	// 9. Meta Llama (海外顶级开源模型).
	if strings.Contains(combined, "llama") {
		ver := extractFamilyVersion(norm, "llama")
		switch {
		case strings.Contains(combined, "behemoth") || params >= 200:
			return ModelProfile{
				Vendor: "meta", FamilyKey: "llama:flagship", SeriesLabel: "Llama 旗舰",
				HierarchyHint: "Meta 超大参数开源旗舰",
				Tier:          TierStrong, SubRank: 77, Version: ver, ParamBillions: params,
				Strengths: "复杂推理、外语交流、知识问答与技术分析",
				Domains:   []string{DomainReasoning, DomainCoding, DomainDaily},
			}
		case strings.Contains(combined, "maverick") || params >= 60:
			return ModelProfile{
				Vendor: "meta", FamilyKey: "llama:main", SeriesLabel: "Llama 主力版",
				HierarchyHint: "Meta 开源主力大模型，强于 Scout / 8B",
				Tier:          TierBalanced, SubRank: 63, Version: ver, ParamBillions: params,
				Strengths: "通用知识问答、外语学习与日常交流",
				Domains:   []string{DomainDaily, DomainHomework},
			}
		default: // scout / 8b / 11b
			return ModelProfile{
				Vendor: "meta", FamilyKey: "llama:lite", SeriesLabel: "Llama 轻量版",
				HierarchyHint: "Meta 开源高速轻量模型",
				Tier:          TierFast, SubRank: 45, Version: ver, ParamBillions: params,
				Strengths: "快速简单问答、外语练习与日常闲聊",
				Domains:   []string{DomainDaily},
			}
		}
	}

	// 10. Mistral / Mixtral / Pixtral (欧洲顶级开源模型).
	if hasWord(combined, "mistral", "mixtral", "pixtral", "ministral") {
		ver := extractFamilyVersion(norm, "mistral", "mixtral", "pixtral", "ministral")
		tier, sub := TierBalanced, 60
		if strings.Contains(combined, "large") {
			tier, sub = TierStrong, 74
		} else if hasAny(combined, []string{"small", "ministral", "nemo", "7b", "8b"}) {
			tier, sub = TierFast, 46
		}
		return ModelProfile{
			Vendor: "mistral", FamilyKey: "mistral:chat", SeriesLabel: "Mistral",
			HierarchyHint: "欧洲主流开源/多语言模型",
			Tier:          tier, SubRank: sub, Version: ver, ParamBillions: params,
			Strengths: "多语种外文翻译、通用问答与文档摘要",
			Domains:   []string{DomainDaily, DomainWriting},
		}
	}

	// 11. xAI Grok.
	if strings.Contains(combined, "grok") {
		ver := extractFamilyVersion(norm, "grok")
		if strings.Contains(combined, "mini") {
			return ModelProfile{
				Vendor: "xai", FamilyKey: "grok:mini", SeriesLabel: "Grok Mini",
				HierarchyHint: "轻量快速推理版",
				Tier:          TierFast, SubRank: 54, Version: ver,
				Strengths: "日常问答、数学逻辑题与快速解答",
				Domains:   []string{DomainDaily, DomainHomework, DomainReasoning},
			}
		}
		tier, sub := TierStrong, 81
		if strings.Contains(combined, "heavy") || ver.Major >= 4 {
			tier, sub = TierFlagship, 88
		}
		return ModelProfile{
			Vendor: "xai", FamilyKey: "grok:main", SeriesLabel: "Grok",
			HierarchyHint: "xAI 旗舰大模型",
			Tier:          tier, SubRank: sub, Version: ver,
			Strengths: "复杂问题分析、科学常识、深度讨论与推理",
			Domains:   []string{DomainReasoning, DomainResearch, DomainDaily},
		}
	}

	// 12. Doubao (豆包) / MiniMax / Step (阶跃) / Hunyuan (混元) / Yi / Ernie.
	if strings.Contains(combined, "doubao") {
		ver := extractFamilyVersion(norm, "doubao")
		tier, sub := TierBalanced, 64
		if strings.Contains(combined, "lite") || strings.Contains(combined, "flash") {
			tier, sub = TierFast, 48
		} else if strings.Contains(combined, "thinking") || strings.Contains(combined, "pro") {
			tier, sub = TierStrong, 74
		}
		return ModelProfile{
			Vendor: "bytedance", FamilyKey: "doubao:chat", SeriesLabel: "豆包 Doubao",
			HierarchyHint: "家用亲切口语对话模型",
			Tier:          tier, SubRank: sub, Version: ver,
			Strengths: "长辈日常陪聊、生活百科、家常菜谱与通俗答疑",
			Domains:   []string{DomainDaily, DomainHomework, DomainWriting},
		}
	}

	if hasWord(combined, "minimax", "abab") {
		ver := extractFamilyVersion(norm, "minimax", "abab")
		return ModelProfile{
			Vendor: "minimax", FamilyKey: "minimax:chat", SeriesLabel: "MiniMax",
			HierarchyHint: "长上下文自然对话模型",
			Tier:          TierBalanced, SubRank: 64, Version: ver,
			Strengths: "自然拟人聊天、中文写作与长文本阅读",
			Domains:   []string{DomainDaily, DomainWriting, DomainLongContext},
		}
	}

	// 13. Generic fallback for any future / unlisted models.
	ver := extractGenericVersion(norm)
	switch {
	case hasAny(combined, []string{"ultra", "flagship", "reasoner", "thinking", "-pro", "-max"}):
		return ModelProfile{
			Vendor: "other", FamilyKey: "other:" + baseFamilyPrefix(norm), SeriesLabel: PrettyName(upstreamID),
			Tier: TierStrong, SubRank: 75, Version: ver, ParamBillions: params,
			Strengths: "复杂问题分析与深度解答",
			Domains:   []string{DomainReasoning, DomainResearch},
		}
	case hasAny(combined, []string{"nano", "micro", "flash-lite"}) || (params > 0 && params <= 8):
		return ModelProfile{
			Vendor: "other", FamilyKey: "other:" + baseFamilyPrefix(norm), SeriesLabel: PrettyName(upstreamID),
			Tier: TierLite, SubRank: 32, Version: ver, ParamBillions: params,
			Strengths: "极速日常简单问答与闲聊",
			Domains:   []string{DomainDaily},
		}
	case hasAny(combined, []string{"mini", "flash", "lite", "turbo", "-air"}) || (params > 0 && params <= 16):
		return ModelProfile{
			Vendor: "other", FamilyKey: "other:" + baseFamilyPrefix(norm), SeriesLabel: PrettyName(upstreamID),
			Tier: TierFast, SubRank: 48, Version: ver, ParamBillions: params,
			Strengths: "日常快速问答、闲聊与基础辅导",
			Domains:   []string{DomainDaily, DomainHomework},
		}
	default:
		return ModelProfile{
			Vendor: "other", FamilyKey: "other:" + baseFamilyPrefix(norm), SeriesLabel: PrettyName(upstreamID),
			Tier: TierBalanced, SubRank: 60, Version: ver, ParamBillions: params,
			Strengths: "日常综合对话与百科问答",
			Domains:   []string{DomainDaily, DomainWriting},
		}
	}
}
