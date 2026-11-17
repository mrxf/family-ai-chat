// Package asr provides pluggable speech-to-text backends: real-time WebSocket
// streaming (Tencent Cloud ASR v2, and future iFlytek) plus OpenAI-compatible
// file upload transcription.
package asr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"nuan/internal/store"
)

const (
	KindTencent = "tencent"
	KindOpenAI  = "openai"
	KindXfyun   = "xfyun"

	ModeStream = "stream"
	ModeUpload = "upload"

	SettingActiveProvider = "asr_provider"
)

var (
	ErrDisabled       = errors.New("服务器还没有开启语音识别")
	ErrBadConfig      = errors.New("语音识别配置不完整")
	ErrNotImplemented = errors.New("该语音识别供应商暂未开放启用")
)

// Event is a normalized recognition update emitted by any streaming ASR driver.
type Event struct {
	Type     string `json:"type"`      // "partial" | "segment" | "done"
	Index    int    `json:"index"`     // 0-based sentence index
	Text     string `json:"text"`      // current sentence text
	FullText string `json:"full_text"` // accumulated text across all sentences so far
}

// StreamSession represents one active real-time speech recognition stream.
// Audio input is always 16kHz, 16-bit signed little-endian, mono PCM.
type StreamSession interface {
	WritePCM(pcm []byte) error
	Finish() error
	Events() <-chan Event
	Err() error
	Close() error
}

// StreamDriver is implemented by streaming ASR vendors (Tencent, iFlytek, ...).
type StreamDriver interface {
	Test(ctx context.Context) error
	OpenStream(ctx context.Context) (StreamSession, error)
}

// ModelOption describes a selectable recognition engine/model for a provider.
type ModelOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProviderPreset describes a supported (or reserved) ASR provider for the admin UI.
type ProviderPreset struct {
	Kind         string        `json:"kind"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Mode         string        `json:"mode"` // "stream" | "upload"
	Available    bool          `json:"available"`
	KeyURL       string        `json:"key_url"`
	DefaultURL   string        `json:"default_url,omitempty"`
	DefaultModel string        `json:"default_model"`
	Models       []ModelOption `json:"models,omitempty"`
}

var Presets = []ProviderPreset{
	{
		Kind:         KindTencent,
		Name:         "腾讯云实时语音",
		Description:  "按住说话边说边出字，松手即发，支持普通话、粤语及多种方言",
		Mode:         ModeStream,
		Available:    true,
		KeyURL:       "https://console.cloud.tencent.com/cam/capi",
		DefaultModel: "16k_zh",
		Models:       TencentModels,
	},
	{
		Kind:         KindOpenAI,
		Name:         "OpenAI 兼容接口",
		Description:  "支持硅基流动 SenseVoice、Whisper 等接口，松手后整段上传识别",
		Mode:         ModeUpload,
		Available:    true,
		KeyURL:       "https://cloud.siliconflow.cn/account/ak",
		DefaultURL:   "https://api.siliconflow.cn/v1",
		DefaultModel: "FunAudioLLM/SenseVoiceSmall",
	},
	{
		Kind:         KindXfyun,
		Name:         "科大讯飞语音听写",
		Description:  "流式 WebSocket 语音听写（接口已预留，后续版本支持）",
		Mode:         ModeStream,
		Available:    false,
		KeyURL:       "https://console.xfyun.cn/app/myapp",
		DefaultModel: "mandarin",
	},
}

func PresetFor(kind string) (ProviderPreset, bool) {
	for _, p := range Presets {
		if p.Kind == kind {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

// Service coordinates active ASR configuration, connection testing, streaming, and file upload transcription.
type Service struct {
	Store      *store.Store
	HTTPClient *http.Client
	DialWS     WSDialFunc

	// Overridable endpoints for unit testing.
	TencentASRHost     string // default "asr.cloud.tencent.com"
	TencentASRScheme   string // default "wss"
	TencentCAMEndpoint string // default "https://cam.tencentcloudapi.com"
}

func NewService(st *store.Store) *Service {
	return &Service{
		Store:              st,
		HTTPClient:         &http.Client{Timeout: 30 * time.Second},
		DialWS:             DialWS,
		TencentASRHost:     "asr.cloud.tencent.com",
		TencentASRScheme:   "wss",
		TencentCAMEndpoint: "https://cam.tencentcloudapi.com",
	}
}

// IsReady reports whether c has all required credentials filled in for its kind.
func IsReady(c *store.ASRConfig) bool {
	if c == nil {
		return false
	}
	switch c.Kind {
	case KindTencent:
		return strings.TrimSpace(c.AppID) != "" &&
			strings.TrimSpace(c.AccessKey) != "" &&
			strings.TrimSpace(c.SecretKey) != ""
	case KindOpenAI:
		return strings.TrimSpace(c.BaseURL) != "" &&
			strings.TrimSpace(c.SecretKey) != ""
	case KindXfyun:
		return false // reserved
	default:
		return false
	}
}

// ActiveStatus returns whether voice input is enabled and its mode ("stream" or "upload").
// If the selected provider is missing credentials, voice input stays disabled so the
// microphone button never appears in a broken state.
func (s *Service) ActiveStatus(ctx context.Context) (enabled bool, mode string, kind string) {
	active, err := s.Store.GetSetting(ctx, SettingActiveProvider)
	if err != nil || active == "" {
		return false, "", ""
	}
	preset, ok := PresetFor(active)
	if !ok || !preset.Available {
		return false, "", ""
	}
	cfg, err := s.Store.GetASRConfig(ctx, active)
	if err != nil || !IsReady(cfg) {
		return false, "", ""
	}
	return true, preset.Mode, active
}

func (s *Service) streamDriverFor(cfg *store.ASRConfig) (StreamDriver, error) {
	switch cfg.Kind {
	case KindTencent:
		return &tencentDriver{svc: s, cfg: cfg}, nil
	case KindXfyun:
		return &xfyunDriver{cfg: cfg}, nil
	default:
		return nil, ErrBadConfig
	}
}

// OpenStream opens a real-time recognition session using the currently active streaming provider.
func (s *Service) OpenStream(ctx context.Context) (StreamSession, error) {
	enabled, mode, kind := s.ActiveStatus(ctx)
	if !enabled || mode != ModeStream {
		return nil, ErrDisabled
	}
	cfg, err := s.Store.GetASRConfig(ctx, kind)
	if err != nil {
		return nil, ErrDisabled
	}
	drv, err := s.streamDriverFor(cfg)
	if err != nil {
		return nil, err
	}
	return drv.OpenStream(ctx)
}

// TranscribeUpload runs batch file transcription when the active provider uses upload mode.
func (s *Service) TranscribeUpload(ctx context.Context, filename string, file io.Reader) (string, error) {
	enabled, mode, kind := s.ActiveStatus(ctx)
	if !enabled || mode != ModeUpload {
		return "", ErrDisabled
	}
	cfg, err := s.Store.GetASRConfig(ctx, kind)
	if err != nil {
		return "", ErrDisabled
	}
	if kind != KindOpenAI {
		return "", ErrDisabled
	}
	return s.transcribeOpenAI(ctx, cfg, filename, file)
}

// ResolveAndValidate populates auto-discoverable fields (like Tencent AppID via CAM)
// and verifies that required fields are present.
func (s *Service) ResolveAndValidate(ctx context.Context, cfg *store.ASRConfig) error {
	preset, ok := PresetFor(cfg.Kind)
	if !ok {
		return errors.New("不支持的语音识别类型")
	}
	if !preset.Available {
		return ErrNotImplemented
	}
	switch cfg.Kind {
	case KindTencent:
		if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
			return errors.New("请填写腾讯云 SecretId 和 SecretKey")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			cfg.Model = preset.DefaultModel
		}
		if strings.TrimSpace(cfg.AppID) == "" {
			appID, err := s.ResolveTencentAppID(ctx, cfg.AccessKey, cfg.SecretKey)
			if err != nil {
				return err
			}
			cfg.AppID = appID
		}
		return nil
	case KindOpenAI:
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return errors.New("请填写接口地址")
		}
		if strings.TrimSpace(cfg.SecretKey) == "" {
			return errors.New("请填写 API Key")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			cfg.Model = preset.DefaultModel
		}
		return nil
	default:
		return ErrNotImplemented
	}
}

// Test verifies connectivity and credentials for a candidate ASR configuration.
// It may populate cfg.AppID if auto-resolved (e.g. Tencent CAM GetUserAppId).
func (s *Service) Test(ctx context.Context, cfg *store.ASRConfig) (latencyMs int64, err error) {
	start := time.Now()
	if err := s.ResolveAndValidate(ctx, cfg); err != nil {
		return 0, err
	}
	switch cfg.Kind {
	case KindTencent:
		drv := &tencentDriver{svc: s, cfg: cfg}
		err = drv.Test(ctx)
	case KindOpenAI:
		err = s.testOpenAI(ctx, cfg)
	case KindXfyun:
		err = ErrNotImplemented
	default:
		err = ErrBadConfig
	}
	return time.Since(start).Milliseconds(), err
}
