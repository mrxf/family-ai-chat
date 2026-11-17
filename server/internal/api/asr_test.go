package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"nuan/internal/asr"
)

func TestASRAdminGatingAndWebSocketStream(t *testing.T) {
	f := newAPIFixture(t)

	// 1. Initially voice_input is false -> microphone button stays hidden.
	var boot struct {
		Features struct {
			VoiceInput bool   `json:"voice_input"`
			VoiceMode  string `json:"voice_mode"`
		} `json:"features"`
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features.VoiceInput || boot.Features.VoiceMode != "" {
		t.Fatalf("expected voice disabled initially, got %+v", boot.Features)
	}

	// Non-admin cannot access /api/admin/asr.
	f.json("GET", "/api/admin/asr", nil, 403, nil)
	f.asAdmin()

	// Cannot enable Tencent ASR before credentials are saved.
	f.json("PUT", "/api/admin/asr", map[string]any{"active": "tencent"}, 400, nil)

	// Mock Tencent CAM (GetUserAppId) + Tencent WSS endpoint.
	camSrv := newHTTPTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-TC-Action") != "GetUserAppId" {
			http.Error(w, "unexpected action", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Response":{"AppId":1305171418,"Uin":"10001","OwnerUin":"10001","RequestId":"req-1"}}`)
	}))
	defer camSrv.Close()
	f.api.ASR.TencentCAMEndpoint = camSrv.URL
	f.api.ASR.HTTPClient = camSrv.Client()

	f.api.ASR.DialWS = func(ctx context.Context, rawURL string) (*asr.WSConn, error) {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(u.Path, "/1305171418") {
			return nil, fmt.Errorf("unexpected appid in path: %s", u.Path)
		}
		cConn, sConn := net.Pipe()
		go func() {
			br := bufio.NewReader(sConn)
			req, err := http.ReadRequest(br)
			if err != nil {
				sConn.Close()
				return
			}
			// Compute RFC 6455 accept header via UpgradeWS helper in an httptest-like response,
			// or standard accept calculation:
			wsSrv, err := upgradePipeConn(sConn, br, req)
			if err != nil {
				sConn.Close()
				return
			}
			defer wsSrv.Close()

			_ = wsSrv.WriteText([]byte(`{"code":0,"message":"success","voice_id":"vid-1"}`))
			for {
				op, msg, err := wsSrv.ReadMessage()
				if err != nil {
					return
				}
				if op == asr.OpBinary && len(msg) > 0 {
					_ = wsSrv.WriteText([]byte(`{"code":0,"result":{"slice_type":1,"index":0,"voice_text_str":"你好"}}`))
				}
				if op == asr.OpText && strings.Contains(string(msg), `"end"`) {
					_ = wsSrv.WriteText([]byte(`{"code":0,"result":{"slice_type":2,"index":0,"voice_text_str":"你好小暖"}}`))
					_ = wsSrv.WriteText([]byte(`{"code":0,"final":1}`))
					return
				}
			}
		}()
		return asr.ClientHandshake(ctx, cConn, u)
	}

	// 2. Test Tencent credentials with blank AppID -> auto-resolves AppID!
	var testRes struct {
		OK    bool   `json:"ok"`
		AppID string `json:"app_id"`
	}
	f.json("POST", "/api/admin/asr/test", map[string]any{
		"kind":       "tencent",
		"access_key": "AKID1234567890123456",
		"secret_key": "SK12345678901234567890",
		"model":      "16k_zh",
	}, 200, &testRes)
	if !testRes.OK || testRes.AppID != "1305171418" {
		t.Fatalf("unexpected test result: %+v", testRes)
	}

	// 3. Save & enable Tencent ASR -> secret is masked and bootstrap flips voice_input=true, voice_mode=stream!
	var adminState struct {
		Active    string `json:"active"`
		Enabled   bool   `json:"enabled"`
		Mode      string `json:"mode"`
		Providers []struct {
			Kind          string `json:"kind"`
			AppID         string `json:"app_id"`
			HasSecret     bool   `json:"has_secret"`
			SecretKeyHint string `json:"secret_key_hint"`
			Ready         bool   `json:"ready"`
		} `json:"providers"`
	}
	f.json("PUT", "/api/admin/asr", map[string]any{
		"active": "tencent",
		"config": map[string]any{
			"kind":       "tencent",
			"access_key": "AKID1234567890123456",
			"secret_key": "SK12345678901234567890",
			"model":      "16k_zh",
		},
	}, 200, &adminState)
	if !adminState.Enabled || adminState.Active != "tencent" || adminState.Mode != "stream" {
		t.Fatalf("unexpected admin state: %+v", adminState)
	}
	if adminState.Providers[0].AppID != "1305171418" || !adminState.Providers[0].HasSecret || strings.Contains(adminState.Providers[0].SecretKeyHint, "1234567890") {
		t.Fatalf("secret not properly masked or appid missing: %+v", adminState.Providers[0])
	}

	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if !boot.Features.VoiceInput || boot.Features.VoiceMode != "stream" {
		t.Fatalf("expected bootstrap voice_input=true stream, got %+v", boot.Features)
	}

	// 4. Connect to /api/asr/stream as a client and verify end-to-end streaming recognition!
	wsClient := dialTestASRStream(t, f)
	defer wsClient.Close()

	if err := wsClient.WriteBinary(make([]byte, 6400)); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := wsClient.WriteText([]byte(`{"type":"end"}`)); err != nil {
		t.Fatalf("write end: %v", err)
	}

	var gotDone string
	for {
		op, payload, err := wsClient.ReadMessage()
		if err != nil {
			t.Fatalf("read ws event: %v", err)
		}
		if op != asr.OpText {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			FullText string `json:"full_text"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == "error" {
			t.Fatalf("stream error: %s", ev.Message)
		}
		if ev.Type == "done" {
			gotDone = ev.FullText
			break
		}
	}
	if gotDone != "你好小暖" {
		t.Fatalf("expected full_text %q, got %q", "你好小暖", gotDone)
	}

	// 5. Turning voice input off hides the mic immediately while preserving saved keys.
	f.json("PUT", "/api/admin/asr", map[string]any{"active": ""}, 200, &adminState)
	if adminState.Enabled || !adminState.Providers[0].HasSecret {
		t.Fatalf("expected disabled with saved secret preserved: %+v", adminState)
	}
	f.json("GET", "/api/bootstrap", nil, 200, &boot)
	if boot.Features.VoiceInput {
		t.Fatalf("expected bootstrap voice_input=false after turning off")
	}
}

type hijackResponseWriter struct {
	conn net.Conn
	br   *bufio.Reader
	hdr  http.Header
}

func (h *hijackResponseWriter) Header() http.Header         { return h.hdr }
func (h *hijackResponseWriter) Write(b []byte) (int, error) { return h.conn.Write(b) }
func (h *hijackResponseWriter) WriteHeader(int)             {}
func (h *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.conn, bufio.NewReadWriter(h.br, bufio.NewWriter(h.conn)), nil
}

func upgradePipeConn(conn net.Conn, br *bufio.Reader, req *http.Request) (*asr.WSConn, error) {
	return asr.UpgradeWS(&hijackResponseWriter{conn: conn, br: br, hdr: http.Header{}}, req)
}

func dialTestASRStream(t *testing.T, f *apiFixture) *asr.WSConn {
	t.Helper()
	u, err := url.Parse(strings.Replace(f.http.URL, "http://", "ws://", 1) + "/api/asr/stream?access_token=" + url.QueryEscape(f.token))
	if err != nil {
		t.Fatal(err)
	}
	var rawConn net.Conn
	if l, ok := f.http.Listener.(*memoryListener); ok {
		rawConn, err = l.dial(t.Context(), "tcp", l.Addr().String())
	} else {
		rawConn, err = net.Dial("tcp", u.Host)
	}
	if err != nil {
		t.Fatal(err)
	}
	ws, err := asr.ClientHandshake(t.Context(), rawConn, u)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}
