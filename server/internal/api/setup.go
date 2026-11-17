package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"nuan/internal/auth"
	"nuan/internal/store"
)

// First-run setup: a server started without NUAN_ADMIN_PASSWORD and without
// users prints a one-time setup code to its log. Whoever can read the log
// (the person who deployed it) creates the first admin from the app.

type setupState struct {
	mu   sync.Mutex
	code string // normalized (no dash, upper case); "" = setup not available
}

const setupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I

func newSetupCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = setupAlphabet[int(b[i])%len(setupAlphabet)]
	}
	return string(b)
}

func normalizeCode(c string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

// InitSetup enables first-run setup when the database has no users and logs the code.
func (s *Server) InitSetup(ctx context.Context) error {
	n, err := s.Store.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	code := newSetupCode()
	s.setup.mu.Lock()
	s.setup.code = code
	s.setup.mu.Unlock()
	s.Log.Warn("首次设置：在小暖 App 登录页填入本服务器地址，按提示输入设置码创建管理员（每次启动重新生成）",
		"setup_code", code[:4]+"-"+code[4:])
	return nil
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"needs_setup": n == 0})
}

func (s *Server) runSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code        string `json:"code"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Device      string `json:"device"`
	}
	if !decode(w, r, &in) {
		return
	}
	ipKey := "setup:" + clientIP(r)
	if s.Limiter.Blocked(ipKey) {
		writeErr(w, 429, "too_many_attempts", "尝试次数太多，请 15 分钟后再试")
		return
	}

	// Serialize setups so two concurrent requests cannot both create an admin.
	s.setup.mu.Lock()
	defer s.setup.mu.Unlock()
	if n, err := s.Store.CountUsers(r.Context()); err != nil {
		s.writeStoreErr(w, r, err)
		return
	} else if n > 0 {
		writeErr(w, 409, "already_setup", "这台服务器已经有管理员了，请直接登录")
		return
	}
	got := normalizeCode(in.Code)
	if s.setup.code == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.setup.code)) != 1 {
		s.Limiter.Fail(ipKey) // per IP only: 32^8 codes make guessing hopeless, a global lock would let anyone block setup
		writeErr(w, 400, "bad_code", "设置码不正确，请查看服务器日志里的 setup_code")
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !usernameRe.MatchString(in.Username) {
		writeErr(w, 400, "bad_username", "用户名需要 2–32 位，可以用字母、数字、汉字")
		return
	}
	if len(in.Password) < 8 {
		writeErr(w, 400, "weak_password", "管理员密码至少 8 位")
		return
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		name = "管理员"
	}
	h, err := auth.HashPassword(in.Password)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	u := &store.User{Username: in.Username, DisplayName: truncate(name, 20), Role: "admin", PasswordHash: h}
	if err := s.Store.CreateUser(r.Context(), u); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	s.setup.code = ""
	s.Log.Info("first-run setup completed", "username", u.Username)
	tok, err := s.Sessions.Create(r.Context(), u.ID, truncate(in.Device, 100))
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok, "user": u})
}
