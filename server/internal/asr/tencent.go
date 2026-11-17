package asr

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"nuan/internal/store"
)

const (
	// 200ms of 16kHz, 16-bit (2 bytes), mono PCM = 6400 bytes, matching Tencent's recommended 1:1 packet size.
	tencentChunkBytes = 6400
	// Minimum gap between consecutive 200ms packets when flushing a burst, keeping rate well under Tencent's 3x limit (code 4000).
	tencentMinPacketGap = 45 * time.Millisecond
	tencentMaxBufBytes  = 512 * 1024
)

var TencentModels = []ModelOption{
	{
		ID:          "16k_zh",
		Name:        "中文普通话通用（16k_zh）",
		Description: "标准普通话实时识别，适用免费额度与通用资源包",
	},
	{
		ID:          "16k_zh_en",
		Name:        "中英 + 31 种方言大模型（16k_zh_en）",
		Description: "支持普通话、英语及粤语、川话、东北话、河南话等方言，抗噪更强（需账号支持大模型 1.0）",
	},
	{
		ID:          "16k_yue",
		Name:        "粤语通用（16k_yue）",
		Description: "粤语实时识别",
	},
	{
		ID:          "16k_en",
		Name:        "英语通用（16k_en）",
		Description: "英语实时识别",
	},
}

// BuildTencentWSSURL constructs a signed WebSocket URL for Tencent Cloud Realtime ASR v2.
func BuildTencentWSSURL(scheme, host, appID, secretID, secretKey, engineModel, voiceID string, now time.Time, nonce uint32) string {
	if scheme == "" {
		scheme = "wss"
	}
	if host == "" {
		host = "asr.cloud.tencent.com"
	}
	if engineModel == "" {
		engineModel = "16k_zh"
	}
	ts := now.Unix()
	exp := ts + 86400

	params := map[string]string{
		"engine_model_type": engineModel,
		"expired":           strconv.FormatInt(exp, 10),
		"needvad":           "1",
		"nonce":             strconv.FormatUint(uint64(nonce), 10),
		"secretid":          secretID,
		"timestamp":         strconv.FormatInt(ts, 10),
		"voice_format":      "1", // PCM
		"voice_id":          voiceID,
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var q strings.Builder
	for i, k := range keys {
		if i > 0 {
			q.WriteByte('&')
		}
		q.WriteString(k)
		q.WriteByte('=')
		q.WriteString(params[k])
	}
	rawQuery := q.String()

	// Signature plaintext excludes "wss://".
	signPlain := fmt.Sprintf("%s/asr/v2/%s?%s", host, appID, rawQuery)
	mac := hmac.New(sha1.New, []byte(secretKey))
	mac.Write([]byte(signPlain))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s://%s/asr/v2/%s?%s&signature=%s", scheme, host, appID, rawQuery, url.QueryEscape(sig))
}

func randomNonce() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	// Positive integer up to 9 digits (<= 10 digits required by Tencent).
	return 100000 + (binary.BigEndian.Uint32(b[:]) % 900000000)
}

// ResolveTencentAppID queries Tencent Cloud CAM (GetUserAppId, TC3-HMAC-SHA256) so admins
// only need to paste SecretId + SecretKey without hunting for AppID.
func (s *Service) ResolveTencentAppID(ctx context.Context, secretID, secretKey string) (string, error) {
	endpoint := s.TencentCAMEndpoint
	if endpoint == "" {
		endpoint = "https://cam.tencentcloudapi.com"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	host := u.Host
	const (
		service     = "cam"
		action      = "GetUserAppId"
		version     = "2019-01-16"
		contentType = "application/json; charset=utf-8"
		payload     = "{}"
	)
	now := time.Now().UTC()
	ts := now.Unix()
	date := now.Format("2006-01-02")

	payloadHash := sha256Hex([]byte(payload))
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-tc-action:%s\n",
		contentType, strings.ToLower(host), strings.ToLower(action))
	signedHeaders := "content-type;host;x-tc-action"
	canonicalReq := strings.Join([]string{
		http.MethodPost,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(ts, 10),
		scope,
		sha256Hex([]byte(canonicalReq)),
	}, "\n")

	kDate := hmacSHA256([]byte("TC3"+strings.TrimSpace(secretKey)), []byte(date))
	kService := hmacSHA256(kDate, []byte(service))
	kSigning := hmacSHA256(kService, []byte("tc3_request"))
	sig := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	authHeader := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		strings.TrimSpace(secretID), scope, signedHeaders, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/", strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Host", host)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", version)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("Authorization", authHeader)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接腾讯云鉴权服务失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var out struct {
		Response struct {
			AppId int64 `json:"AppId"`
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", errors.New("腾讯云返回格式异常，请稍后再试")
	}
	if e := out.Response.Error; e != nil {
		switch {
		case strings.HasPrefix(e.Code, "AuthFailure"):
			return "", errors.New("SecretId 或 SecretKey 不正确，请核对后重试")
		case strings.Contains(e.Code, "UnauthorizedOperation"):
			return "", errors.New("当前子账号没有自动读取 AppID 的权限，请手动填写 AppID（在腾讯云控制台右上角「账号信息」可查看）")
		default:
			return "", fmt.Errorf("腾讯云校验失败（%s）：%s", e.Code, e.Message)
		}
	}
	if out.Response.AppId <= 0 {
		return "", errors.New("未能自动获取到腾讯云 AppID，请手动填写 AppID")
	}
	return strconv.FormatInt(out.Response.AppId, 10), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	return mac.Sum(nil)
}

func formatTencentError(code int, msg string) error {
	switch code {
	case 4000:
		return errors.New("语音发送过快，请重试")
	case 4001:
		return fmt.Errorf("语音识别参数不支持（4001）：%s", msg)
	case 4002:
		return errors.New("腾讯云密钥鉴权失败（4002），请检查 AppID、SecretId 和 SecretKey 是否匹配")
	case 4003:
		return errors.New("腾讯云账号尚未开通「语音识别」服务（4003），请先去腾讯云控制台开通")
	case 4004:
		return errors.New("腾讯云语音识别资源包已用完（4004），请购买资源包或开启后付费")
	case 4005:
		return errors.New("腾讯云账户欠费已停服（4005），请充值后再试")
	case 4006:
		return errors.New("当前同时说话的人数超过账号并发上限（4006）")
	case 4007:
		return errors.New("音频格式解码失败（4007）")
	case 4008:
		return errors.New("长时间没有收到声音，连接已断开")
	case 5000, 5001, 5002:
		return errors.New("腾讯云语音服务繁忙，请再试一次")
	case 6001:
		return errors.New("当前服务器位于境外或开启了境外代理，无法调用腾讯云国内站（6001）")
	default:
		if msg != "" {
			return fmt.Errorf("语音识别出错（%d）：%s", code, msg)
		}
		return fmt.Errorf("语音识别出错（错误码 %d）", code)
	}
}

type tencentMsg struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	VoiceID   string `json:"voice_id"`
	MessageID string `json:"message_id"`
	Final     int    `json:"final"`
	Result    *struct {
		SliceType    int    `json:"slice_type"`
		Index        int    `json:"index"`
		VoiceTextStr string `json:"voice_text_str"`
	} `json:"result"`
}

type tencentDriver struct {
	svc *Service
	cfg *store.ASRConfig
}

func (d *tencentDriver) dialAndHandshake(ctx context.Context) (*WSConn, error) {
	voiceID := uuid.NewString()
	wssURL := BuildTencentWSSURL(
		d.svc.TencentASRScheme,
		d.svc.TencentASRHost,
		strings.TrimSpace(d.cfg.AppID),
		strings.TrimSpace(d.cfg.AccessKey),
		strings.TrimSpace(d.cfg.SecretKey),
		strings.TrimSpace(d.cfg.Model),
		voiceID,
		time.Now(),
		randomNonce(),
	)
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ws, err := d.svc.DialWS(dialCtx, wssURL)
	if err != nil {
		return nil, fmt.Errorf("连接腾讯云语音服务失败：%w", err)
	}

	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, payload, err := ws.ReadMessage()
	_ = ws.SetReadDeadline(time.Time{})
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("腾讯云握手失败：%w", err)
	}
	var handshake tencentMsg
	if err := json.Unmarshal(payload, &handshake); err != nil {
		ws.Close()
		return nil, errors.New("腾讯云握手响应格式不正确")
	}
	if handshake.Code != 0 {
		ws.Close()
		return nil, formatTencentError(handshake.Code, handshake.Message)
	}
	return ws, nil
}

// Test performs a real WebSocket handshake against Tencent ASR and closes cleanly.
func (d *tencentDriver) Test(ctx context.Context) error {
	ws, err := d.dialAndHandshake(ctx)
	if err != nil {
		return err
	}
	defer ws.Close()
	_ = ws.WriteText([]byte(`{"type":"end"}`))
	return nil
}

func (d *tencentDriver) OpenStream(ctx context.Context) (StreamSession, error) {
	ws, err := d.dialAndHandshake(ctx)
	if err != nil {
		return nil, err
	}
	s := &tencentSession{
		ws:     ws,
		events: make(chan Event, 32),
		wakeCh: make(chan struct{}, 1),
		doneCh: make(chan struct{}),
	}
	go s.sendLoop(ctx)
	go s.readLoop()
	return s, nil
}

type tencentSession struct {
	ws     *WSConn
	events chan Event

	mu       sync.Mutex
	pcmBuf   bytes.Buffer
	finishing bool
	closed   bool
	err      error

	wakeCh chan struct{}
	doneCh chan struct{}
}

func (s *tencentSession) notifyWake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *tencentSession) setErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *tencentSession) WritePCM(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	s.mu.Lock()
	if s.closed || s.finishing {
		s.mu.Unlock()
		return netErrClosed
	}
	if s.pcmBuf.Len()+len(pcm) > tencentMaxBufBytes {
		s.mu.Unlock()
		return errors.New("语音缓冲已满")
	}
	_, _ = s.pcmBuf.Write(pcm)
	s.mu.Unlock()
	s.notifyWake()
	return nil
}

var netErrClosed = errors.New("session closed")

func (s *tencentSession) Finish() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.finishing = true
	s.mu.Unlock()
	s.notifyWake()
	return nil
}

func (s *tencentSession) Events() <-chan Event { return s.events }

func (s *tencentSession) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *tencentSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.notifyWake()
	return s.ws.Close()
}

func (s *tencentSession) sendLoop(ctx context.Context) {
	var lastSent time.Time
	chunk := make([]byte, tencentChunkBytes)

	for {
		s.mu.Lock()
		closed := s.closed
		finishing := s.finishing
		avail := s.pcmBuf.Len()
		var toSend []byte
		sendEnd := false

		if !closed {
			if avail >= tencentChunkBytes {
				n, _ := s.pcmBuf.Read(chunk)
				toSend = chunk[:n]
			} else if finishing {
				if avail > 0 {
					n, _ := s.pcmBuf.Read(chunk[:avail])
					toSend = chunk[:n]
				} else {
					sendEnd = true
				}
			}
		}
		moreReady := !closed && (s.pcmBuf.Len() >= tencentChunkBytes || (finishing && !sendEnd))
		s.mu.Unlock()

		if closed {
			return
		}

		if len(toSend) > 0 {
			if !lastSent.IsZero() {
				if wait := tencentMinPacketGap - time.Since(lastSent); wait > 0 {
					select {
					case <-time.After(wait):
					case <-ctx.Done():
						_ = s.Close()
						return
					}
				}
			}
			if err := s.ws.WriteBinary(toSend); err != nil {
				s.setErr(errors.New("语音上传中断，请重试"))
				_ = s.Close()
				return
			}
			lastSent = time.Now()
			if moreReady {
				continue
			}
		}

		if sendEnd {
			_ = s.ws.WriteText([]byte(`{"type":"end"}`))
			return
		}

		select {
		case <-s.wakeCh:
		case <-s.doneCh:
			return
		case <-ctx.Done():
			_ = s.Close()
			return
		}
	}
}

func (s *tencentSession) readLoop() {
	defer close(s.doneCh)
	defer close(s.events)
	defer s.ws.Close()

	var segments []string

	for {
		_ = s.ws.SetReadDeadline(time.Now().Add(25 * time.Second))
		op, payload, err := s.ws.ReadMessage()
		if err != nil {
			s.mu.Lock()
			wasClosed := s.closed
			s.mu.Unlock()
			if !wasClosed && !errors.Is(err, io.EOF) {
				s.setErr(errors.New("语音识别连接中断"))
			}
			return
		}
		if op != OpText {
			continue
		}

		var msg tencentMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Code != 0 {
			s.setErr(formatTencentError(msg.Code, msg.Message))
			return
		}

		if r := msg.Result; r != nil && r.Index >= 0 && r.Index < 500 {
			for len(segments) <= r.Index {
				segments = append(segments, "")
			}
			segments[r.Index] = r.VoiceTextStr
			full := strings.Join(segments, "")
			evType := "partial"
			if r.SliceType == 2 {
				evType = "segment"
			}
			s.events <- Event{
				Type:     evType,
				Index:    r.Index,
				Text:     r.VoiceTextStr,
				FullText: full,
			}
		}

		if msg.Final == 1 {
			s.events <- Event{
				Type:     "done",
				FullText: strings.Join(segments, ""),
			}
			return
		}
	}
}
