package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nuan/internal/auth"
	"nuan/internal/chat"
	"nuan/internal/llm"
	"nuan/internal/memory"
	"nuan/internal/store"
	"nuan/internal/store/storetest"
)

// Opt in only where the sandbox prohibits binding localhost sockets. net.Pipe
// still exercises net/http, chi, flushing, disconnects and request cancellation.
// The default path uses ordinary TCP httptest.NewServer instances.
type memoryListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
	address     *net.TCPAddr
}

var memoryPort atomic.Int32

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr { return l.address }
func (l *memoryListener) dial(ctx context.Context, network, address string) (net.Conn, error) {
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

func newHTTPTestServer(handler http.Handler) *httptest.Server {
	if os.Getenv("NUAN_TEST_IN_MEMORY_HTTP") != "1" {
		return httptest.NewServer(handler)
	}
	l := &memoryListener{connections: make(chan net.Conn), done: make(chan struct{}), address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 10000 + int(memoryPort.Add(1))}}
	s := &httptest.Server{Listener: l, Config: &http.Server{Handler: handler}}
	s.Start()
	s.Client().Transport = &http.Transport{DialContext: l.dial}
	return s
}

// A plan can be clock driven, stepped by the test, or left blocked until cancellation.
type upstreamPlan struct {
	tokens                []string
	interval              time.Duration
	steps                 <-chan struct{}
	titleSteps            <-chan struct{}
	blockAfter            bool
	errorAfter            bool
	failStatus, failCount int
}

type upstreamMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type upstreamRequest struct {
	Model    string            `json:"model"`
	Stream   bool              `json:"stream"`
	Effort   string            `json:"reasoning_effort"`
	Messages []upstreamMessage `json:"messages"`
}

func (r upstreamRequest) prompt() string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(r.Messages[i].Content, &text) == nil {
			return withoutSystemNote(text)
		}
		var parts []struct{ Type, Text string }
		if json.Unmarshal(r.Messages[i].Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "text" {
					return withoutSystemNote(p.Text)
				}
			}
		}
	}
	return ""
}

func withoutSystemNote(text string) string {
	if strings.HasPrefix(text, "（系统附注，不是用户说的话") {
		if i := strings.Index(text, "）\n"); i >= 0 {
			return text[i+len("）\n"):]
		}
	}
	return text
}

type fakeUpstream struct {
	extraction string

	server    *httptest.Server
	mu        sync.Mutex
	plans     map[string]upstreamPlan
	requests  map[string][]upstreamRequest
	cancelled chan string
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	u := &fakeUpstream{plans: map[string]upstreamPlan{}, requests: map[string][]upstreamRequest{}, cancelled: make(chan string, 32)}
	u.server = newHTTPTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == "GET" {
			if r.Header.Get("Authorization") != "Bearer local-test-key" {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"fake","created":1},{"id":"gpt-6.1-terra","created":3},{"id":"text-embedding-3-small","created":2},{"id":"gpt-4o-2024-08-06","created":4}]}`)
			return
		}
		if r.URL.Path == "/v1/embeddings" {
			fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"prompt_tokens":2}}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer local-test-key" {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
			http.Error(w, "bad request", 400)
			return
		}
		var req upstreamRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad JSON", 400)
			return
		}
		if !req.Stream {
			u.mu.Lock()
			extraction := u.extraction
			u.mu.Unlock()
			if extraction != "" && len(req.Messages) > 0 && strings.Contains(string(req.Messages[0].Content), "记忆整理员") {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": extraction}}}})
				return
			}
			u.mu.Lock()
			titleSteps := u.plans[req.prompt()].titleSteps
			u.mu.Unlock()
			if titleSteps != nil {
				select {
				case <-titleSteps:
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"家庭测试标题"}}]}`)
			return
		}
		prompt := req.prompt()
		u.mu.Lock()
		plan, ok := u.plans[prompt]
		u.requests[prompt] = append(u.requests[prompt], req)
		attempt := len(u.requests[prompt])
		u.mu.Unlock()
		if !ok {
			plan.tokens = []string{"你", "好", "🙂"}
			plan.interval = 5 * time.Millisecond
		}
		if attempt <= plan.failCount {
			w.WriteHeader(plan.failStatus)
			fmt.Fprint(w, `{"error":{"message":"planned failure"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		defer func() {
			if r.Context().Err() != nil {
				select {
				case u.cancelled <- prompt:
				default:
				}
			}
		}()
		var ticks <-chan time.Time
		if plan.interval > 0 {
			ticker := time.NewTicker(plan.interval)
			defer ticker.Stop()
			ticks = ticker.C
		}
		for _, token := range plan.tokens {
			if plan.steps != nil {
				select {
				case <-plan.steps:
				case <-r.Context().Done():
					return
				}
			}
			if ticks != nil {
				select {
				case <-ticks:
				case <-r.Context().Done():
					return
				}
			}
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": token, "reasoning_content": "想"}}}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
		if plan.blockAfter {
			<-r.Context().Done()
			return
		}
		if plan.errorAfter {
			fmt.Fprint(w, "data: {\"error\":{\"message\":\"stream broke\"}}\n\n")
			return
		}
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`+"\n\ndata: [DONE]\n\n")
	}))
	return u
}

func (u *fakeUpstream) plan(prompt string, p upstreamPlan) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.plans[prompt] = p
}
func (u *fakeUpstream) recorded(prompt string) []upstreamRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]upstreamRequest(nil), u.requests[prompt]...)
}

type apiFixture struct {
	t     *testing.T
	st    *store.Store
	svc   *chat.Service
	api   *Server
	http  *httptest.Server
	up    *fakeUpstream
	user  *store.User
	model *store.Model
	token string
}

var passwordOnce sync.Once
var passwordHash string
var passwordErr error

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	dir := t.TempDir()
	st := storetest.Open(t)
	passwordOnce.Do(func() { passwordHash, passwordErr = auth.HashPassword("test-password") })
	if passwordErr != nil {
		t.Fatal(passwordErr)
	}
	u := &store.User{Username: "alice", DisplayName: "测试用户", Role: "member", PasswordHash: passwordHash}
	if err := st.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	up := newFakeUpstream(t)
	if l, ok := up.server.Listener.(*memoryListener); ok {
		// NewClient exposes the shared transport through its public HTTP field.
		// These fixtures are intentionally sequential; restore it after all work.
		transport := llm.NewClient("", up.server.URL, "").HTTP.Transport.(*http.Transport)
		oldDial, oldProxy := transport.DialContext, transport.Proxy
		transport.CloseIdleConnections()
		transport.DialContext, transport.Proxy = l.dial, nil
		t.Cleanup(func() { transport.CloseIdleConnections(); transport.DialContext, transport.Proxy = oldDial, oldProxy })
	}
	t.Cleanup(up.server.Close)
	p := &store.Provider{ID: "local", Name: "local", BaseURL: up.server.URL, APIKey: "local-test-key", Enabled: true}
	if err := st.UpsertProvider(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	m := &store.Model{ID: "test-model", ProviderID: p.ID, UpstreamModel: "fake", DisplayName: "fake", Vision: true, DefaultEffort: "low", ThinkEffort: "high", ContextWindow: 128000, MaxOutput: 1000, Enabled: true}
	if err := st.UpsertModel(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(t.Context(), "default_model_id", m.ID); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := chat.NewService(st, dir, log)
	svc.Memory = memory.New(st, svc.ResolveMemoryModel, log)
	s := New(Config{DataDir: dir}, st, svc, log)
	f := &apiFixture{t: t, st: st, svc: svc, api: s, http: newHTTPTestServer(s.Routes()), up: up, user: u, model: m}
	f.http.Client().Timeout = 4 * time.Second
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		svc.Shutdown(ctx)
		f.http.Close()
	})
	token, err := s.Sessions.Create(t.Context(), u.ID, "test")
	f.token = token
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *apiFixture) request(method, path, token, contentType string, body io.Reader, status int, out any) {
	f.t.Helper()
	req, err := http.NewRequestWithContext(f.t.Context(), method, f.http.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := f.http.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	if resp.StatusCode != status {
		f.t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, resp.StatusCode, status, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			f.t.Fatalf("decode %s: %v", raw, err)
		}
	}
}

func (f *apiFixture) json(method, path string, body any, status int, out any) {
	f.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	f.request(method, path, f.token, "application/json", bytes.NewReader(b), status, out)
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (f *apiFixture) send(body map[string]any) *chat.SendResult {
	f.t.Helper()
	var result chat.SendResult
	f.json("POST", "/api/chat", body, 200, &result)
	if result.Assistant == nil || result.Assistant.ID == "" || result.Conversation == nil || result.UserMessage == nil {
		f.t.Fatalf("invalid send result: %+v", result)
	}
	if !result.Conversation.Private {
		// Titles run in a detached goroutine. Drain that work before fixture teardown.
		eventually(f.t, "title persistence", func() bool {
			c, err := f.st.GetConversation(f.t.Context(), result.Conversation.ID, f.user.ID)
			return err == nil && c.Title != ""
		})
	}
	return &result
}

func (f *apiFixture) waitRun(id string) {
	f.t.Helper()
	eventually(f.t, "run removal", func() bool { return f.svc.Hub.Get(id) == nil })
}

type sseEvent struct {
	name string
	data json.RawMessage
}
type sseReader struct {
	t      *testing.T
	body   io.ReadCloser
	scan   *bufio.Scanner
	cancel context.CancelFunc
}

func (f *apiFixture) stream(id string) *sseReader {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.t.Context(), 4*time.Second)
	req, err := http.NewRequestWithContext(ctx, "GET", f.http.URL+"/api/messages/"+id+"/stream", nil)
	if err != nil {
		cancel()
		f.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	resp, err := f.http.Client().Do(req)
	if err != nil {
		cancel()
		f.t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body.Close()
		cancel()
		f.t.Fatalf("stream response: %s %v", resp.Status, resp.Header)
	}
	s := &sseReader{t: f.t, body: resp.Body, scan: bufio.NewScanner(resp.Body), cancel: cancel}
	s.scan.Buffer(make([]byte, 4096), 2<<20)
	f.t.Cleanup(s.close)
	return s
}

func (s *sseReader) close() { s.cancel(); s.body.Close() }
func (s *sseReader) next() sseEvent {
	s.t.Helper()
	var e sseEvent
	for s.scan.Scan() {
		line := s.scan.Text()
		if line == "" {
			if e.name != "" {
				if !json.Valid(e.data) {
					s.t.Fatalf("invalid SSE JSON: %s", e.data)
				}
				return e
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			e.name = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			e.data = append(e.data, strings.TrimPrefix(line, "data: ")...)
		}
	}
	s.t.Fatalf("stream ended before next event: %v", s.scan.Err())
	return e
}

func decodeEvent[T any](t *testing.T, e sseEvent) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.data, &v); err != nil {
		t.Fatalf("event %s: %s: %v", e.name, e.data, err)
	}
	return v
}

type streamText struct{ Content, Reasoning, Status, Title string }

func (s *sseReader) finish() (streamText, *store.Message, []string) {
	s.t.Helper()
	var text streamText
	var names []string
	for {
		e := s.next()
		names = append(names, e.name)
		switch e.name {
		case "snapshot":
			v := decodeEvent[streamText](s.t, e)
			text.Content = v.Content
			text.Reasoning = v.Reasoning
		case "delta":
			v := decodeEvent[streamText](s.t, e)
			text.Content += v.Content
			text.Reasoning += v.Reasoning
		case "title":
			text.Title = decodeEvent[streamText](s.t, e).Title
		case "done":
			v := decodeEvent[chat.FinalEvent](s.t, e)
			if v.Message == nil {
				s.t.Fatal("done missing message")
			}
			return text, v.Message, names
		default:
			s.t.Fatalf("unknown SSE event %q", e.name)
		}
	}
}

func (f *apiFixture) uploadPNG() (*store.File, []byte) {
	f.t.Helper()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		f.t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "tiny.png")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := part.Write(img.Bytes()); err != nil {
		f.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		f.t.Fatal(err)
	}
	var file store.File
	f.request("POST", "/api/files", f.token, mw.FormDataContentType(), &body, 201, &file)
	if file.Mime != "image/png" || file.Width != 2 || file.Height != 3 {
		f.t.Fatalf("upload: %+v", file)
	}
	return &file, img.Bytes()
}
