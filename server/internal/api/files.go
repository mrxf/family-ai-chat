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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	_ "golang.org/x/image/webp"

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
