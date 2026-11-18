package asr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

func TestBuildTencentWSSURLMatchesSpec(t *testing.T) {
	// Verify alphabetical sorting, absence of wss:// in signature plaintext, and strict URL encoding of + / =.
	now := time.Unix(1673408372, 0)
	got := BuildTencentWSSURL(
		"wss",
		"asr.cloud.tencent.com",
		"1259220000",
		"AKIDexample123456",
		"SKexampleSecretKey+/=",
		"16k_zh",
		"c64385ee-3e5c-4fc5-bbfd-7c71addb35b0",
		now,
		1673408372,
	)
	if !strings.HasPrefix(got, "wss://asr.cloud.tencent.com/asr/v2/1259220000?engine_model_type=16k_zh&expired=1673494772&needvad=1&nonce=1673408372&secretid=AKIDexample123456&timestamp=1673408372&voice_format=1&voice_id=c64385ee-3e5c-4fc5-bbfd-7c71addb35b0&signature=") {
		t.Fatalf("unexpected url prefix: %s", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	sigPart := got[strings.LastIndex(got, "signature=")+len("signature="):]
	if strings.ContainsAny(sigPart, "+/=") {
		t.Fatalf("signature must percent-encode +, /, =: %s", sigPart)
	}
	if u.Query().Get("signature") == "" {
		t.Fatal("decoded signature is empty")
	}
}

// pipeWS creates a connected client+server WSConn pair over net.Pipe() without opening TCP sockets.
func pipeWS(t *testing.T, rawURL string) (client *WSConn, server *WSConn) {
	t.Helper()
	cConn, sConn := net.Pipe()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	srvCh := make(chan *WSConn, 1)
	errCh := make(chan error, 1)

	go func() {
		br := bufio.NewReader(sConn)
		req, err := http.ReadRequest(br)
		if err != nil {
			errCh <- err
			return
		}
		accept := wsAcceptKey(req.Header.Get("Sec-WebSocket-Key"))
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := sConn.Write([]byte(resp)); err != nil {
			errCh <- err
			return
		}
		srvCh <- &WSConn{conn: sConn, br: br, isClient: false}
	}()

	cli, err := ClientHandshake(context.Background(), cConn, u)
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	select {
	case srv := <-srvCh:
		return cli, srv
	case err := <-errCh:
		t.Fatalf("server handshake: %v", err)
		return nil, nil
	}
}

func TestTencentStreamChunkingAndAggregation(t *testing.T) {
	st := storetest.Open(t)

	ctx := context.Background()
	svc := NewService(st)

	// Before configuration, voice input must be disabled.
	if en, mode, _ := svc.ActiveStatus(ctx); en || mode != "" {
		t.Fatalf("expected disabled initially, got enabled=%v mode=%q", en, mode)
	}

	if err := st.UpsertASRConfig(ctx, &store.ASRConfig{
		Kind:      KindTencent,
		AppID:     "1250000001",
		AccessKey: "AKIDtest",
		SecretKey: "SKtest",
		Model:     "16k_zh",
	}); err != nil {
		t.Fatal(err)
	}
	// Still disabled until admin enables asr_provider!
	if en, _, _ := svc.ActiveStatus(ctx); en {
		t.Fatal("expected disabled when asr_provider is unset")
	}
	if err := st.SetSetting(ctx, SettingActiveProvider, KindTencent); err != nil {
		t.Fatal(err)
	}
	if en, mode, kind := svc.ActiveStatus(ctx); !en || mode != ModeStream || kind != KindTencent {
		t.Fatalf("expected tencent stream enabled, got %v %q %q", en, mode, kind)
	}

	var receivedFrames []int
	svc.DialWS = func(ctx context.Context, rawURL string) (*WSConn, error) {
		cli, srv := pipeWS(t, rawURL)
		go func() {
			defer srv.Close()
			// 1. Handshake ack
			_ = srv.WriteText([]byte(`{"code":0,"message":"success","voice_id":"v1"}`))

			// 2. Read binary PCM chunks until {"type":"end"}
			for {
				op, data, err := srv.ReadMessage()
				if err != nil {
					return
				}
				if op == OpBinary {
					receivedFrames = append(receivedFrames, len(data))
					if len(receivedFrames) == 1 {
						_ = srv.WriteText([]byte(`{"code":0,"result":{"slice_type":1,"index":0,"voice_text_str":"今天"}}`))
					} else if len(receivedFrames) == 2 {
						_ = srv.WriteText([]byte(`{"code":0,"result":{"slice_type":2,"index":0,"voice_text_str":"今天天气不错，"}}`))
					}
					continue
				}
				if op == OpText && strings.Contains(string(data), `"end"`) {
					_ = srv.WriteText([]byte(`{"code":0,"result":{"slice_type":2,"index":1,"voice_text_str":"适合出去散步。"}}`))
					_ = srv.WriteText([]byte(`{"code":0,"final":1}`))
					return
				}
			}
		}()
		return cli, nil
	}

	sess, err := svc.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer sess.Close()

	// Write 10,000 bytes in odd-sized chunks -> must be sliced into 6400 + 3600!
	if err := sess.WritePCM(make([]byte, 4000)); err != nil {
		t.Fatal(err)
	}
	if err := sess.WritePCM(make([]byte, 6000)); err != nil {
		t.Fatal(err)
	}
	if err := sess.Finish(); err != nil {
		t.Fatal(err)
	}

	var got []Event
	for ev := range sess.Events() {
		got = append(got, ev)
	}
	if err := sess.Err(); err != nil {
		t.Fatalf("session err: %v", err)
	}
	if len(receivedFrames) != 2 || receivedFrames[0] != 6400 || receivedFrames[1] != 3600 {
		t.Fatalf("expected PCM chunks [6400, 3600], got %v", receivedFrames)
	}
	if len(got) != 4 {
		b, _ := json.Marshal(got)
		t.Fatalf("expected 4 events, got %d: %s", len(got), string(b))
	}
	last := got[len(got)-1]
	if last.Type != "done" || last.FullText != "今天天气不错，适合出去散步。" {
		t.Fatalf("unexpected final event: %+v", last)
	}
}

func TestLiveTencentProbe(t *testing.T) {
	sid := os.Getenv("TENCENT_SECRET_ID")
	skey := os.Getenv("TENCENT_SECRET_KEY")
	if sid == "" || skey == "" {
		t.Skip("set TENCENT_SECRET_ID and TENCENT_SECRET_KEY to run live Tencent probe")
	}
	st := storetest.Open(t)

	svc := NewService(st)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, model := range []string{"16k_zh", "16k_zh_en"} {
		cfg := &store.ASRConfig{
			Kind:      KindTencent,
			AccessKey: sid,
			SecretKey: skey,
			Model:     model,
		}
		ms, err := svc.Test(ctx, cfg)
		t.Logf("model=%s app_id=%s latency=%dms err=%v", model, cfg.AppID, ms, err)
		if model == "16k_zh" && err != nil {
			t.Fatalf("live 16k_zh handshake failed: %v", err)
		}
	}
}
