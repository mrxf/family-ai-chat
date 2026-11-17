package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"nuan/internal/asr"
	"nuan/internal/llm"
	"nuan/internal/store"
)

func (s *Server) voiceStatus(ctx context.Context) (enabled bool, mode string) {
	if s.ASR != nil {
		if en, m, _ := s.ASR.ActiveStatus(ctx); en {
			return true, m
		}
	}
	if s.Cfg.ASR != nil && s.Cfg.ASR.BaseURL != "" && s.Cfg.ASR.APIKey != "" {
		return true, asr.ModeUpload
	}
	return false, ""
}

// asrStream upgrades GET /api/asr/stream to a real-time recognition WebSocket:
//   - Client -> Server: binary frames (16kHz 16-bit mono PCM), then text `{"type":"end"}`
//   - Server -> Client: JSON text frames `{"type":"partial"|"segment"|"done"|"error", ...}`
func (s *Server) asrStream(w http.ResponseWriter, r *http.Request) {
	ws, err := asr.UpgradeWS(w, r)
	if err != nil {
		return
	}
	defer ws.Close()

	writeWSJSON := func(v any) error {
		b, _ := json.Marshal(v)
		return ws.WriteText(b)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	if s.ASR == nil {
		_ = writeWSJSON(map[string]any{"type": "error", "message": asr.ErrDisabled.Error()})
		return
	}
	sess, err := s.ASR.OpenStream(ctx)
	if err != nil {
		_ = writeWSJSON(map[string]any{"type": "error", "message": err.Error()})
		return
	}
	defer sess.Close()

	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for ev := range sess.Events() {
			if err := writeWSJSON(ev); err != nil {
				_ = sess.Close()
				return
			}
		}
		if err := sess.Err(); err != nil {
			_ = writeWSJSON(map[string]any{"type": "error", "message": err.Error()})
		}
	}()

	for {
		_ = ws.SetReadDeadline(time.Now().Add(20 * time.Second))
		op, payload, err := ws.ReadMessage()
		if err != nil {
			_ = sess.Close()
			<-eventsDone
			return
		}
		switch op {
		case asr.OpBinary:
			if err := sess.WritePCM(payload); err != nil {
				_ = sess.Close()
				<-eventsDone
				return
			}
		case asr.OpText:
			var cmd struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(payload, &cmd)
			if cmd.Type == "end" {
				_ = sess.Finish()
				select {
				case <-eventsDone:
				case <-time.After(10 * time.Second):
					_ = sess.Close()
					<-eventsDone
				}
				return
			}
		}
	}
}

// transcribe handles POST /api/audio/transcriptions for upload-mode ASR providers
// (OpenAI-compatible services such as SiliconFlow SenseVoice, or legacy Config.ASR).
func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "bad_file", "没有收到录音")
		return
	}
	defer file.Close()

	name := hdr.Filename
	if name == "" {
		name = "voice.m4a"
	}

	if s.ASR != nil {
		if en, mode, _ := s.ASR.ActiveStatus(r.Context()); en && mode == asr.ModeUpload {
			text, err := s.ASR.TranscribeUpload(r.Context(), name, file)
			if err != nil {
				s.Log.Warn("asr upload failed", "err", err)
				writeErr(w, 502, "asr_failed", err.Error())
				return
			}
			writeJSON(w, 200, map[string]string{"text": text})
			return
		}
	}

	// Legacy static fallback when api.Config.ASR was injected directly.
	legacy := s.Cfg.ASR
	if legacy == nil || legacy.BaseURL == "" || legacy.APIKey == "" {
		writeErr(w, 404, "voice_disabled", "服务器没有开启录音转写")
		return
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", legacy.Model)
	part, _ := mw.CreateFormFile("file", name)
	if _, err := io.Copy(part, file); err != nil {
		writeErr(w, 400, "bad_file", "录音读取失败")
		return
	}
	_ = mw.Close()

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, llm.Endpoint(legacy.BaseURL, "/audio/transcriptions"), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+legacy.APIKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeErr(w, 502, "asr_failed", "语音识别服务连接失败")
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		s.Log.Warn("asr failed", "status", resp.StatusCode, "body", truncate(string(raw), 300))
		writeErr(w, 502, "asr_failed", "语音识别失败，请再说一次")
		return
	}
	var out struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &out)
	writeJSON(w, 200, map[string]string{"text": strings.TrimSpace(out.Text)})
}

// ---- admin ASR management ----

type asrProviderView struct {
	asr.ProviderPreset
	AppID         string `json:"app_id"`
	AccessKey     string `json:"access_key"`
	HasSecret     bool   `json:"has_secret"`
	SecretKeyHint string `json:"secret_key_hint"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	Ready         bool   `json:"ready"`
}

type asrInput struct {
	Kind      string  `json:"kind"`
	AppID     *string `json:"app_id"`
	AccessKey *string `json:"access_key"`
	SecretKey *string `json:"secret_key"` // omitted or "" = keep existing stored secret
	BaseURL   *string `json:"base_url"`
	Model     *string `json:"model"`
}

func (s *Server) buildASRConfig(ctx context.Context, in asrInput) (*store.ASRConfig, string, string) {
	kind := strings.TrimSpace(in.Kind)
	preset, ok := asr.PresetFor(kind)
	if !ok {
		return nil, "bad_kind", "不支持的语音识别类型"
	}
	if !preset.Available {
		return nil, "not_available", asr.ErrNotImplemented.Error()
	}

	cfg, err := s.Store.GetASRConfig(ctx, kind)
	isNew := err != nil
	if isNew {
		cfg = &store.ASRConfig{
			Kind:    kind,
			BaseURL: preset.DefaultURL,
			Model:   preset.DefaultModel,
		}
	}
	oldOrigin := origin(cfg.BaseURL)
	oldAK := cfg.AccessKey
	oldAppID := cfg.AppID

	if in.AppID != nil {
		cfg.AppID = strings.TrimSpace(*in.AppID)
	}
	if in.AccessKey != nil {
		cfg.AccessKey = strings.TrimSpace(*in.AccessKey)
	}
	newSecret := in.SecretKey != nil && strings.TrimSpace(*in.SecretKey) != ""
	if newSecret {
		cfg.SecretKey = strings.TrimSpace(*in.SecretKey)
	}
	if in.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if in.Model != nil {
		cfg.Model = strings.TrimSpace(*in.Model)
	}
	if cfg.Model == "" {
		cfg.Model = preset.DefaultModel
	}

	// If the admin switched to a different Tencent SecretId without manually changing AppID,
	// clear the stale AppID so ResolveAndValidate fetches the new account's AppID automatically.
	if kind == asr.KindTencent && oldAK != "" && cfg.AccessKey != oldAK && cfg.AppID == oldAppID && (in.AppID == nil || strings.TrimSpace(*in.AppID) == oldAppID) {
		cfg.AppID = ""
	}

	if kind == asr.KindOpenAI {
		if cfg.BaseURL == "" || !validBaseURL(cfg.BaseURL) {
			return nil, "bad_url", "接口地址需要以 http:// 或 https:// 开头"
		}
		if !isNew && !newSecret && oldOrigin != "" && origin(cfg.BaseURL) != oldOrigin {
			return nil, "key_required", "修改了接口地址，请重新填写 API Key"
		}
	}
	return cfg, "", ""
}

func (s *Server) adminGetASR(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	active, _ := s.Store.GetSetting(ctx, asr.SettingActiveProvider)
	saved, err := s.Store.ListASRConfigs(ctx)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	enabled, mode, _ := s.ASR.ActiveStatus(ctx)

	views := make([]asrProviderView, 0, len(asr.Presets))
	for _, pr := range asr.Presets {
		v := asrProviderView{
			ProviderPreset: pr,
			BaseURL:        pr.DefaultURL,
			Model:          pr.DefaultModel,
		}
		if c, ok := saved[pr.Kind]; ok {
			v.AppID = c.AppID
			v.AccessKey = c.AccessKey
			v.HasSecret = c.SecretKey != ""
			if c.SecretKey != "" {
				v.SecretKeyHint = maskKey(c.SecretKey)
			}
			if c.BaseURL != "" {
				v.BaseURL = c.BaseURL
			}
			if c.Model != "" {
				v.Model = c.Model
			}
			v.Ready = asr.IsReady(c)
		}
		views = append(views, v)
	}
	writeJSON(w, 200, map[string]any{
		"active":    active,
		"enabled":   enabled,
		"mode":      mode,
		"providers": views,
	})
}

func (s *Server) adminTestASR(w http.ResponseWriter, r *http.Request) {
	var in asrInput
	if !decode(w, r, &in) {
		return
	}
	cfg, code, msg := s.buildASRConfig(r.Context(), in)
	if code != "" {
		writeErr(w, 400, code, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	ms, err := s.ASR.Test(ctx, cfg)
	if err != nil {
		writeJSON(w, 200, map[string]any{
			"ok":         false,
			"latency_ms": ms,
			"app_id":     cfg.AppID,
			"message":    err.Error(),
		})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"latency_ms": ms,
		"app_id":     cfg.AppID,
		"message":    "连接成功，语音识别服务正常",
	})
}

func (s *Server) adminPutASR(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active *string   `json:"active"`
		Config *asrInput `json:"config"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()

	if in.Config != nil {
		cfg, code, msg := s.buildASRConfig(ctx, *in.Config)
		if code != "" {
			writeErr(w, 400, code, msg)
			return
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.ASR.ResolveAndValidate(rctx, cfg)
		cancel()
		if err != nil {
			writeErr(w, 400, "bad_asr_config", err.Error())
			return
		}
		if err := s.Store.UpsertASRConfig(ctx, cfg); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}

	if in.Active != nil {
		act := strings.TrimSpace(*in.Active)
		if act != "" {
			preset, ok := asr.PresetFor(act)
			if !ok {
				writeErr(w, 400, "bad_kind", "不支持的语音识别类型")
				return
			}
			if !preset.Available {
				writeErr(w, 400, "not_available", asr.ErrNotImplemented.Error())
				return
			}
			saved, err := s.Store.GetASRConfig(ctx, act)
			if err != nil || !asr.IsReady(saved) {
				if errors.Is(err, store.ErrNotFound) || saved == nil {
					writeErr(w, 400, "not_configured", "请先填写并保存语音识别密钥，再开启语音输入")
					return
				}
				writeErr(w, 400, "not_configured", "密钥配置还不完整，请补全后再开启")
				return
			}
		}
		if err := s.Store.SetSetting(ctx, asr.SettingActiveProvider, act); err != nil {
			s.writeStoreErr(w, r, err)
			return
		}
	}

	s.adminGetASR(w, r)
}
