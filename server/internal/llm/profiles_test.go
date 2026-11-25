package llm

import (
	"strings"
	"testing"
)

func TestModelProfileHierarchyAndVersions(t *testing.T) {
	// 1. Anthropic: Fable > Opus > Sonnet > Haiku, and Opus 5.5 > 5.0 > 4.7 > 4.6, Fable 5.2 > 5.0.
	fable50 := ProfileModel("claude-fable-5.0", "Claude Fable 5.0")
	fable52 := ProfileModel("anthropic/claude-fable-5.2", "Claude Fable 5.2")
	opus46 := ProfileModel("anthropic/claude-opus-4.6", "Claude Opus 4.6")
	opus47 := ProfileModel("claude-opus-4-7", "Opus 4.7")
	opus50 := ProfileModel("opus-5.0", "Opus 5.0")
	opus55 := ProfileModel("claude-opus-5-5-20261001", "Claude Opus 5.5")
	sonnet55 := ProfileModel("claude-sonnet-5.5", "Claude Sonnet 5.5")
	haiku55 := ProfileModel("claude-haiku-5.5", "Claude Haiku 5.5")

	if fable52.Vendor != "anthropic" || !(fable52.RankScore() > fable50.RankScore() && fable50.RankScore() > opus55.RankScore()) {
		t.Fatalf("expected Anthropic Fable 5.2 > Fable 5.0 > Opus 5.5, got vendor=%s %d > %d > %d",
			fable52.Vendor, fable52.RankScore(), fable50.RankScore(), opus55.RankScore())
	}
	if !(opus55.RankScore() > opus50.RankScore() && opus50.RankScore() > opus47.RankScore() && opus47.RankScore() > opus46.RankScore()) {
		t.Fatalf("expected Opus 5.5 > 5.0 > 4.7 > 4.6, got %d > %d > %d > %d",
			opus55.RankScore(), opus50.RankScore(), opus47.RankScore(), opus46.RankScore())
	}
	if !(opus46.RankScore() > sonnet55.RankScore() && sonnet55.RankScore() > haiku55.RankScore()) {
		t.Fatalf("expected Opus > Sonnet > Haiku, got opus=%d sonnet=%d haiku=%d",
			opus46.RankScore(), sonnet55.RankScore(), haiku55.RankScore())
	}
	if opus55.Version.String() != "5.5" || opus46.Version.String() != "4.6" {
		t.Fatalf("opus versions: 5.5=%q 4.6=%q", opus55.Version.String(), opus46.Version.String())
	}

	// 2. Google Gemini 4 (Periodic Table): Carbon > Neon > Argon > Hydrogen > Helium.
	carbon := ProfileModel("gemini-4-carbon", "Gemini 4 Carbon")
	neon := ProfileModel("gemini-4-neon", "Gemini 4 Neon")
	argon := ProfileModel("gemini-4-argon", "Gemini 4 Argon")
	hydrogen := ProfileModel("gemini-4-hydrogen", "Gemini 4 Hydrogen")
	helium := ProfileModel("gemini-4-helium", "Gemini 4 Helium")

	if !(carbon.RankScore() > neon.RankScore() && neon.RankScore() > argon.RankScore() && argon.RankScore() > hydrogen.RankScore() && hydrogen.RankScore() > helium.RankScore()) {
		t.Fatalf("expected Carbon > Neon > Argon > Hydrogen > Helium, got %d > %d > %d > %d > %d",
			carbon.RankScore(), neon.RankScore(), argon.RankScore(), hydrogen.RankScore(), helium.RankScore())
	}
	if !carbon.HasDomain(DomainMedical) || !carbon.HasDomain(DomainResearch) {
		t.Fatalf("carbon should cover medical & research: %+v", carbon)
	}
	if hydrogen.Tier != TierFast || !hydrogen.HasDomain(DomainHomework) {
		t.Fatalf("hydrogen should be TierFast with homework/daily: %+v", hydrogen)
	}

	// 3. OpenAI GPT-6: Astra > Sol > Terra > Luna.
	astra62 := ProfileModel("gpt-6.2-astra", "GPT-6.2 Astra")
	sol62 := ProfileModel("gpt-6.2-sol", "GPT-6.2 Sol")
	terra61 := ProfileModel("gpt-6.1-terra", "GPT-6.1 Terra")
	luna62 := ProfileModel("gpt-6.2-luna", "GPT-6.2 Luna")

	if !(astra62.RankScore() > sol62.RankScore() && sol62.RankScore() > terra61.RankScore() && terra61.RankScore() > luna62.RankScore()) {
		t.Fatalf("expected Astra > Sol > Terra > Luna, got %d > %d > %d > %d",
			astra62.RankScore(), sol62.RankScore(), terra61.RankScore(), luna62.RankScore())
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

	// 5. Same-tier vendor preference: Google Gemini > Anthropic Claude > OpenAI GPT.
	if !(carbon.RankScore() > fable52.RankScore() && fable52.RankScore() > opus55.RankScore() && opus55.RankScore() > astra62.RankScore()) {
		t.Fatalf("expected flagship Gemini 4 Carbon > Claude Fable 5.2 > Claude Opus 5.5 > GPT-6.2 Astra, got %d > %d > %d > %d",
			carbon.RankScore(), fable52.RankScore(), opus55.RankScore(), astra62.RankScore())
	}
	if !(neon.RankScore() > sonnet55.RankScore() && sonnet55.RankScore() > sol62.RankScore()) {
		t.Fatalf("expected strong Gemini 4 Neon > Claude Sonnet 5.5 > GPT-6.2 Sol, got %d > %d > %d",
			neon.RankScore(), sonnet55.RankScore(), sol62.RankScore())
	}
	if !(hydrogen.RankScore() > haiku55.RankScore() && haiku55.RankScore() > terra61.RankScore()) {
		t.Fatalf("expected fast Gemini 4 Hydrogen > Claude Haiku 5.5 > GPT-6.1 Terra, got %d > %d > %d",
			hydrogen.RankScore(), haiku55.RankScore(), terra61.RankScore())
	}
	if !(helium.RankScore() > luna62.RankScore()) {
		t.Fatalf("expected lite Gemini 4 Helium > GPT-6.2 Luna, got %d > %d",
			helium.RankScore(), luna62.RankScore())
	}
}
