package llm

// Provider kinds. Everything speaks the OpenAI-compatible chat completions API;
// the kind only tunes defaults (base URL, model list parsing, request dialect).
const (
	KindOpenAI      = "openai"
	KindOpenRouter  = "openrouter"
	KindGemini      = "gemini"
	KindDeepSeek    = "deepseek"
	KindDashScope   = "dashscope"
	KindSiliconFlow = "siliconflow"
	KindCustom      = "custom"
)

// Preset describes a well-known provider for the "add provider" screen.
type Preset struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"` // empty for custom
	Description string `json:"description"`
	KeyURL      string `json:"key_url"`    // where to create an API key
	KeyPrefix   string `json:"key_prefix"` // typical key prefix, a hint for the input field
}

// Presets are shown in this order.
var Presets = []Preset{
	{KindOpenRouter, "OpenRouter", "https://openrouter.ai/api/v1",
		"一个 Key 就能用 GPT、Claude、Gemini 等几百个模型，按量付费，并返回每次的实际花费",
		"https://openrouter.ai/settings/keys", "sk-or-"},
	{KindOpenAI, "OpenAI", "https://api.openai.com/v1",
		"GPT 系列官方接口",
		"https://platform.openai.com/api-keys", "sk-"},
	{KindGemini, "Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai",
		"Gemini 官方接口，Google AI Studio 申请 Key，有免费额度",
		"https://aistudio.google.com/apikey", "AIza"},
	{KindDeepSeek, "DeepSeek", "https://api.deepseek.com/v1",
		"国产模型，中文好、价格低，国内可直连",
		"https://platform.deepseek.com/api_keys", "sk-"},
	{KindDashScope, "阿里云百炼", "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"通义千问等模型，国内可直连",
		"https://bailian.console.aliyun.com/?apiKey=1", "sk-"},
	{KindSiliconFlow, "硅基流动", "https://api.siliconflow.cn/v1",
		"DeepSeek、Qwen 等开源模型合集，国内可直连",
		"https://cloud.siliconflow.cn/account/ak", "sk-"},
	{KindCustom, "自定义", "",
		"其他兼容 OpenAI 接口的服务，或者自己的中转站",
		"", ""},
}

// PresetFor returns the preset of a kind (custom for unknown kinds).
func PresetFor(kind string) Preset {
	for _, p := range Presets {
		if p.Kind == kind {
			return p
		}
	}
	return Presets[len(Presets)-1]
}

// ValidKind reports whether kind is a known preset.
func ValidKind(kind string) bool {
	for _, p := range Presets {
		if p.Kind == kind {
			return true
		}
	}
	return false
}
