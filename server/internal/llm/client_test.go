package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000}
}
func (l *memoryListener) dial(ctx context.Context, _, address string) (net.Conn, error) {
	if address != l.Addr().String() {
		return nil, fmt.Errorf("unexpected in-memory HTTP address %s", address)
	}
	client, server := net.Pipe()
	select {
	case l.connections <- server:
		return client, nil
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	case <-l.done:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	}
}

func newStreamServer(handler http.Handler) *httptest.Server {
	if os.Getenv("NUAN_TEST_IN_MEMORY_HTTP") != "1" {
		return httptest.NewServer(handler)
	}
	l := &memoryListener{connections: make(chan net.Conn), done: make(chan struct{})}
	s := &httptest.Server{Listener: l, Config: &http.Server{Handler: handler}}
	s.Start()
	s.Client().Transport = &http.Transport{DialContext: l.dial}
	return s
}

func streamClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := newStreamServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient("", srv.URL, "test-key")
	c.HTTP = srv.Client()
	c.HTTP.Timeout = 3 * time.Second
	return c
}

func TestStreamContentReasoningUsageAndDone(t *testing.T) {
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("unexpected request: %s %s %v", r.Method, r.URL, r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["stream"] != true || body["model"] != "fake" || body["reasoning_effort"] != "high" || body["max_tokens"] != float64(42) {
			t.Errorf("request body: %#v", body)
		}
		if opts, ok := body["stream_options"].(map[string]any); !ok || opts["include_usage"] != true {
			t.Errorf("missing usage option: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": heartbeat\r\n\r\ndata: invalid json\n\ndata: {"+`"choices":[{"delta":{"reasoning_content":"想","reasoning":"ignored"}}]}`+"\r\n\r\n")
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"reasoning":"好了"}}]}`+"\n\n")
		for _, s := range []string{"你", "好", "🌞"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", s)
		}
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20,"completion_tokens_details":{"reasoning_tokens":5}}}`+"\n\ndata: [DONE]\n\n")
		// Anything after [DONE] must never reach the callback.
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"content":"extra"}}]}`+"\n\n")
	})
	var content, reasoning strings.Builder
	res, err := c.Stream(t.Context(), Request{Model: "fake", MaxTokens: 42, ReasoningEffort: "high", Messages: []Message{{Role: "user", Content: "hello"}}}, func(e Event) {
		content.WriteString(e.Content)
		reasoning.WriteString(e.Reasoning)
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.String() != "你好🌞" || reasoning.String() != "想好了" || res.FinishReason != "stop" || res.Usage == nil || *res.Usage != (Usage{12, 8, 20, 5, 0}) {
		t.Fatalf("content=%q reasoning=%q result=%+v usage=%+v", content.String(), reasoning.String(), res, res.Usage)
	}
}

func TestStreamAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
		retry            bool
	}{
		{"nested", `{"error":{"message":"bad key"}}`, "bad key", 401, false},
		{"top-level", `{"message":"slow down"}`, "slow down", 429, true},
		{"plain", "  unavailable \n", "unavailable", 503, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := streamClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			_, err := c.Stream(t.Context(), Request{}, func(Event) { t.Error("unexpected event") })
			var ae *APIError
			if !errors.As(err, &ae) || ae.Status != tc.status || ae.Message != tc.want || ae.Retryable() != tc.retry {
				t.Fatalf("error=%#v, want status=%d message=%q retry=%v", err, tc.status, tc.want, tc.retry)
			}
		})
	}
}

func TestStreamTermination(t *testing.T) {
	for _, tc := range []struct {
		name, ending string
		wantErr      bool
	}{
		{"done", "data: [DONE]\n\n", false},
		{"finish-before-eof", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", false},
		{"premature-eof", "", true},
		{"in-band-error", "data: {\"error\":{\"message\":\"broken\"}}\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"+tc.ending)
			})
			var got string
			_, err := c.Stream(t.Context(), Request{}, func(e Event) { got += e.Content })
			if (err != nil) != tc.wantErr || got != "partial" {
				t.Fatalf("content=%q error=%v", got, err)
			}
			if tc.name == "premature-eof" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("want unexpected EOF, got %v", err)
			}
			if tc.name == "in-band-error" {
				var ae *APIError
				if !errors.As(err, &ae) || ae.Message != "broken" {
					t.Fatalf("want APIError, got %v", err)
				}
			}
		})
	}
}

func TestStreamIdleTimeout(t *testing.T) {
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	c.IdleTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := c.Stream(t.Context(), Request{}, func(Event) {})
	if !errors.Is(err, errIdle) {
		t.Fatalf("want idle timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("idle timeout elapsed=%s", elapsed)
	}
}

func TestStreamContextCancellation(t *testing.T) {
	ready := make(chan struct{})
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		close(ready)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() {
		select {
		case <-ready:
			cancel()
		case <-ctx.Done():
		}
	}()
	_, err := c.Stream(ctx, Request{}, func(Event) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

// IdleTimeout promises byte inactivity, so a slowly arriving SSE line is active.
func TestStreamIdleTimeoutCountsPartialLineActivity(t *testing.T) {
	c := streamClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: ")
		w.(http.Flusher).Flush()
		tick := time.NewTicker(40 * time.Millisecond)
		defer tick.Stop()
		for i := 0; i < 12; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
			}
			fmt.Fprint(w, " ")
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "[DONE]\n\n")
	})
	c.IdleTimeout = 200 * time.Millisecond
	_, err := c.Stream(t.Context(), Request{}, func(Event) {})
	if err != nil {
		t.Fatalf("bytes arrived every 40ms, stream must not time out: %v", err)
	}
}
