package llm

import (
	"strings"
	"testing"
)

func TestModelProfileHierarchyAndVersions(t *testing.T) {
	// 1. Anthropic: Opus > Sonnet > Haiku, and Opus 5.5 > 5.0 > 4.7 > 4.6.
	opus46 := ProfileModel("anthropic/claude-opus-4.6", "Claude Opus 4.6")
	opus47 := ProfileModel("claude-opus-4-7", "Opus 4.7")
	opus50 := ProfileModel("opus-5.0", "Opus 5.0")
	opus55 := ProfileModel("claude-opus-5-5-20261001", "Claude Opus 5.5")
	sonnet55 := ProfileModel("claude-sonnet-5.5", "Claude Sonnet 5.5")
	haiku45 := ProfileModel("claude-haiku-4-5", "Claude Haiku 4.5")

	if !(opus55.RankScore() > opus50.RankScore() && opus50.RankScore() > opus47.RankScore() && opus47.RankScore() > opus46.RankScore()) {
		t.Fatalf("expected Opus 5.5 > 5.0 > 4.7 > 4.6, got %d > %d > %d > %d",
			opus55.RankScore(), opus50.RankScore(), opus47.RankScore(), opus46.RankScore())
	}
	if !(opus46.RankScore() > sonnet55.RankScore() && sonnet55.RankScore() > haiku45.RankScore()) {
		t.Fatalf("expected Opus > Sonnet > Haiku, got opus=%d sonnet=%d haiku=%d",
			opus46.RankScore(), sonnet55.RankScore(), haiku45.RankScore())
	}
	if opus55.Version.String() != "5.5" || opus46.Version.String() != "4.6" {
		t.Fatalf("opus versions: 5.5=%q 4.6=%q", opus55.Version.String(), opus46.Version.String())
	}

	// 2. Google Gemini: Carbon > Pro > Flash > Hydrogen.
	carbon := ProfileModel("gemini-4-carbon", "Gemini 4 Carbon")
	pro := ProfileModel("gemini-3-pro", "Gemini 3 Pro")
	flash := ProfileModel("gemini-3-flash", "Gemini 3 Flash")
	hydrogen := ProfileModel("gemini-4-hydrogen", "Gemini 氢气模型")

	if !(carbon.RankScore() > pro.RankScore() && pro.RankScore() > flash.RankScore() && flash.RankScore() > hydrogen.RankScore()) {
		t.Fatalf("expected Carbon > Pro > Flash > Hydrogen, got %d > %d > %d > %d",
			carbon.RankScore(), pro.RankScore(), flash.RankScore(), hydrogen.RankScore())
	}
	if !carbon.HasDomain(DomainMedical) || !carbon.HasDomain(DomainResearch) {
		t.Fatalf("carbon should cover medical & research: %+v", carbon)
	}
	if hydrogen.Tier != TierLite || !hydrogen.HasDomain(DomainHomework) {
		t.Fatalf("hydrogen should be TierLite with homework/daily: %+v", hydrogen)
	}

	// 3. OpenAI: Fable 5.2 > Fable 5.0, Astra / Sol > Terra > Mini > Nano.
	fable50 := ProfileModel("fable-5.0", "Fable 5.0")
	fable52 := ProfileModel("openai/fable-5.2", "Fable 5.2")
	astra := ProfileModel("gpt-6-astra", "GPT-6 Astra")
	terra := ProfileModel("gpt-6.1-terra", "GPT-6.1 Terra")
	mini := ProfileModel("gpt-5-mini", "GPT-5 Mini")
	nano := ProfileModel("gpt-5-nano", "GPT-5 Nano")

	if !(fable52.RankScore() > fable50.RankScore()) {
		t.Fatalf("expected Fable 5.2 > 5.0, got %d vs %d", fable52.RankScore(), fable50.RankScore())
	}
	if !(astra.RankScore() > terra.RankScore() && terra.RankScore() > mini.RankScore() && mini.RankScore() > nano.RankScore()) {
		t.Fatalf("expected Astra > Terra > Mini > Nano, got %d > %d > %d > %d",
			astra.RankScore(), terra.RankScore(), mini.RankScore(), nano.RankScore())
	}

	// 4. Open-source & domestic flagship models: DeepSeek, Kimi, Qwen, Bailu, GLM, Llama.
	dsR1 := ProfileModel("deepseek-ai/DeepSeek-R1", "DeepSeek R1")
	dsDistill := ProfileModel("deepseek-ai/DeepSeek-R1-Distill-Qwen-7B", "DeepSeek R1 7B")
	dsV3 := ProfileModel("deepseek-chat", "DeepSeek V3")
	dsFlash := ProfileModel("deepseek-flash", "DeepSeek Flash")

	if !(dsR1.RankScore() > dsV3.RankScore() && dsV3.RankScore() > dsFlash.RankScore()) {
		t.Fatalf("expected DeepSeek R1 > V3 > Flash, got %d > %d > %d", dsR1.RankScore(), dsV3.RankScore(), dsFlash.RankScore())
	}
	if dsDistill.Tier >= dsR1.Tier {
		t.Fatalf("expected 7B distill to be lower tier than full R1: distill=%+v r1=%+v", dsDistill, dsR1)
	}

	kimiK3 := ProfileModel("moonshotai/kimi-k3", "Kimi K3")
	kimiK25 := ProfileModel("kimi-k2.5", "Kimi K2.5")
	kimiK2 := ProfileModel("kimi-k2", "Kimi K2")
	if !(kimiK3.RankScore() > kimiK25.RankScore() && kimiK25.RankScore() > kimiK2.RankScore()) {
		t.Fatalf("expected Kimi K3 > K2.5 > K2, got %d > %d > %d", kimiK3.RankScore(), kimiK25.RankScore(), kimiK2.RankScore())
	}

	qwen235 := ProfileModel("Qwen/Qwen3.5-235B-A22B", "Qwen 3.5 235B")
	qwen8b := ProfileModel("Qwen/Qwen3-8B", "Qwen 3 8B")
	if qwen235.Version.String() != "3.5" || qwen235.ParamBillions != 235 {
		t.Fatalf("qwen235 misparsed: ver=%s params=%d", qwen235.Version.String(), qwen235.ParamBillions)
	}
	if !(qwen235.RankScore() > qwen8b.RankScore()) {
		t.Fatalf("expected Qwen 235B > Qwen 8B")
	}

	bailu := ProfileModel("bailu-2", "Bailu 2")
	if bailu.Vendor != "bailu" || !strings.Contains(bailu.Summary(), "Bailu") {
		t.Fatalf("bailu profile=%+v", bailu)
	}
}
