// Package config loads settings from environment variables (and an optional .env file).
package config

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	Addr      string // NUAN_ADDR, default :8080
	DataDir   string // NUAN_DATA_DIR, default ./data
	LogFormat string // NUAN_LOG_FORMAT: text | json

	// First-run bootstrap (only used when the corresponding tables are empty).
	AdminUsername string // NUAN_ADMIN_USERNAME, default admin
	AdminPassword string // NUAN_ADMIN_PASSWORD (required on first run)
	UpstreamURL   string // NUAN_UPSTREAM_URL
	UpstreamKey   string // NUAN_UPSTREAM_KEY
	Models        string // NUAN_MODELS: comma separated upstream model ids; first is default

	// Optional voice input (OpenAI-compatible /audio/transcriptions).
	ASRURL   string // NUAN_ASR_URL
	ASRKey   string // NUAN_ASR_KEY
	ASRModel string // NUAN_ASR_MODEL, default FunAudioLLM/SenseVoiceSmall
}

func Load() Config {
	loadDotEnv(".env")
	return Config{
		Addr:          env("NUAN_ADDR", ":8080"),
		DataDir:       env("NUAN_DATA_DIR", "./data"),
		LogFormat:     env("NUAN_LOG_FORMAT", "text"),
		AdminUsername: env("NUAN_ADMIN_USERNAME", "admin"),
		AdminPassword: os.Getenv("NUAN_ADMIN_PASSWORD"),
		UpstreamURL:   os.Getenv("NUAN_UPSTREAM_URL"),
		UpstreamKey:   os.Getenv("NUAN_UPSTREAM_KEY"),
		Models:        env("NUAN_MODELS", "gpt-6.1-terra"),
		ASRURL:        os.Getenv("NUAN_ASR_URL"),
		ASRKey:        os.Getenv("NUAN_ASR_KEY"),
		ASRModel:      env("NUAN_ASR_MODEL", "FunAudioLLM/SenseVoiceSmall"),
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// loadDotEnv sets variables from a KEY=VALUE file without overriding the real environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
