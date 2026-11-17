package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"nuan/internal/llm"
	"nuan/internal/store"
)

func (s *Service) transcribeOpenAI(ctx context.Context, cfg *store.ASRConfig, filename string, file io.Reader) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "FunAudioLLM/SenseVoiceSmall"
	}
	_ = mw.WriteField("model", model)
	if filename == "" {
		filename = "voice.m4a"
	}
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", errors.New("录音读取失败")
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, llm.Endpoint(cfg.BaseURL, "/audio/transcriptions"), &body)
	if err != nil {
		return "", errors.New("语音识别接口地址不正确")
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.SecretKey))

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("语音识别服务连接失败")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("语音识别失败（HTTP %d），请检查配置或再说一次", resp.StatusCode)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", errors.New("语音识别服务返回格式异常")
	}
	return strings.TrimSpace(out.Text), nil
}

func (s *Service) testOpenAI(ctx context.Context, cfg *store.ASRConfig) error {
	testCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(testCtx, http.MethodGet, llm.Endpoint(cfg.BaseURL, "/models"), nil)
	if err != nil {
		return errors.New("接口地址格式不正确")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.SecretKey))
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接到接口地址：%w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("API Key 不正确或没有权限")
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("服务器返回 HTTP %d，请检查接口地址是否正确", resp.StatusCode)
	}
	return nil
}
