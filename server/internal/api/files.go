package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	_ "golang.org/x/image/webp"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const maxUpload = 20 << 20

var allowedImages = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "bad_file", "没有收到文件，或文件超过 20MB")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil || len(data) > maxUpload {
		writeErr(w, 400, "too_large", "文件超过 20MB")
		return
	}
	mime := http.DetectContentType(data)
	ext, ok := allowedImages[mime]
	if !ok {
		writeErr(w, 400, "unsupported", "目前只支持 JPG、PNG、WebP、GIF 图片")
		return
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeErr(w, 400, "bad_image", "图片无法识别")
		return
	}
	sum := sha256.Sum256(data)
	f := &store.File{ID: store.NewID(), UserID: u.ID, Mime: mime, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Width: cfg.Width, Height: cfg.Height}
	dir := filepath.Join(s.Cfg.DataDir, "files", time.Now().Format("2006-01"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	f.Path = filepath.Join(dir, f.ID+ext)
	if err := os.WriteFile(f.Path, data, 0o644); err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	if err := s.Store.CreateFile(r.Context(), f); err != nil {
		os.Remove(f.Path)
		s.writeStoreErr(w, r, err)
		return
	}
	writeJSON(w, 201, f)
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	f, err := s.Store.GetFile(r.Context(), chi.URLParam(r, "id"), currentUser(r).ID)
	if err != nil {
		s.writeStoreErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, f.Path)
}

// transcribe proxies a short voice recording to an OpenAI-compatible
// /audio/transcriptions endpoint (e.g. SiliconFlow SenseVoice).
func (s *Server) transcribe(w http.ResponseWriter, r *http.Request) {
	asr := s.Cfg.ASR
	if asr == nil {
		writeErr(w, 404, "voice_disabled", "服务器没有开启语音识别")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "bad_file", "没有收到录音")
		return
	}
	defer file.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("model", asr.Model)
	name := hdr.Filename
	if name == "" {
		name = "voice.m4a"
	}
	part, _ := mw.CreateFormFile("file", name)
	if _, err := io.Copy(part, file); err != nil {
		writeErr(w, 400, "bad_file", "录音读取失败")
		return
	}
	mw.Close()

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, llm.Endpoint(asr.BaseURL, "/audio/transcriptions"), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+asr.APIKey)
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

// ---- app self-update ----
//
// Put the APK and a latest.json in <data>/app:
//   {"version":"1.0.1","build":2,"notes":"修复了…","apk":"nuan-1.0.1.apk","force":false}

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.apk$`)

func (s *Server) appVersion(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.Cfg.DataDir, "app", "latest.json"))
	if err != nil {
		writeJSON(w, 200, map[string]any{"available": false})
		return
	}
	var info map[string]any
	if json.Unmarshal(raw, &info) != nil {
		writeJSON(w, 200, map[string]any{"available": false})
		return
	}
	if apk, _ := info["apk"].(string); safeName.MatchString(apk) {
		info["url"] = "/app/download/" + apk
	}
	info["available"] = true
	writeJSON(w, 200, info)
}

func (s *Server) appDownload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !safeName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, filepath.Join(s.Cfg.DataDir, "app", name))
}
