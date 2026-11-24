package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nuan/internal/llm"
	"nuan/internal/store"
)

// drawJob is one picture to draw, or to modify when source is set.
type drawJob struct {
	i            int
	prompt, size string
	source       *store.Attachment
	sourceNo     int // its number in the conversation (图N)
}

// drawImage generates one picture and stores it as a file owned by userID.
// redrawn reports that the model could not edit, so the picture was drawn anew
// from the source's prompt plus the requested change.
func (s *Service) drawImage(ctx context.Context, userID string, private bool, im *store.Model, ip *store.Provider, j drawJob) (att store.Attachment, cost float64, redrawn bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	req := llm.ImageRequest{Model: im.UpstreamModel, Prompt: j.prompt, Size: j.size}
	if j.source != nil {
		if req.Image, req.ImageMime, err = s.editSource(ctx, j.source.FileID, userID); err != nil {
			return att, 0, false, err
		}
	}
	select {
	case drawSlots <- struct{}{}:
		defer func() { <-drawSlots }()
	case <-ctx.Done():
		return att, 0, false, ctx.Err()
	}
	c := s.client(ip)
	res, err := c.GenerateImage(ctx, req)
	var ae *llm.APIError
	if err != nil && req.Size != "" && errors.As(err, &ae) && ae.Status == 400 && strings.Contains(strings.ToLower(ae.Message), "size") {
		req.Size = "" // the model does not take this size: let it pick
		res, err = c.GenerateImage(ctx, req)
	}
	if err != nil && j.source != nil && j.source.Prompt != "" && editUnsupported(err) {
		// The model cannot edit: redraw a picture we drew from its prompt plus the change.
		req.Image, req.ImageMime = nil, ""
		req.Prompt = clip(j.source.Prompt+"\n\nChange: "+j.prompt, 4000)
		redrawn = true
		res, err = c.GenerateImage(ctx, req)
	}
	if err != nil {
		return att, 0, false, err
	}
	f, err := s.saveImage(ctx, userID, res.Data, private)
	if err != nil {
		return att, 0, false, err
	}
	if u := res.Usage; u != nil {
		cost = u.Cost
		if cost <= 0 {
			cost = im.EstimateCost(u.PromptTokens, u.CompletionTokens)
		}
	}
	att = store.Attachment{FileID: f.ID, Mime: f.Mime, Width: f.Width, Height: f.Height, Prompt: j.prompt}
	if j.source != nil {
		att.Source = j.source.FileID
	}
	return att, cost, redrawn, nil
}

// editUnsupported reports an upstream error that means "this model cannot edit
// pictures" rather than a problem with this particular request.
func editUnsupported(err error) bool {
	var ae *llm.APIError
	if !errors.As(err, &ae) || isSafetyRejection(ae.Message) {
		return false
	}
	switch ae.Status {
	case 400, 404, 405, 415, 422, 501:
		return true
	}
	return false
}

const maxEditSide = 2048

// editSource loads a picture of the conversation as input for an edit:
// at most 2048 px on the long side, JPEG (no alpha: some APIs read it as a mask).
func (s *Service) editSource(ctx context.Context, fileID, userID string) ([]byte, string, error) {
	f, err := s.Store.GetFile(ctx, fileID, userID)
	if err != nil {
		return nil, "", fmt.Errorf("source image: %w", err)
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, "", fmt.Errorf("source image: %w", err)
	}
	if f.Mime == "image/jpeg" && max(f.Width, f.Height) <= maxEditSide {
		return data, f.Mime, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode source image: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > maxEditSide {
		w, h = max(1, w*maxEditSide/long), max(1, h*maxEditSide/long)
	}
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(flat, flat.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 90}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// imageRefs lists the pictures of a conversation before the given message, in
// order: 图N in the prompt is refs[N-1]. Photos the user sent and pictures
// the assistant drew share one numbering.
func (s *Service) imageRefs(ctx context.Context, convID, beforeID string) []store.Attachment {
	refs, err := s.Store.ListConversationImageRefs(ctx, convID, beforeID, 500)
	if err != nil {
		return nil
	}
	return refs
}

func collectRefs(history []*store.Message) []store.Attachment {
	var refs []store.Attachment
	for _, m := range history {
		for _, a := range m.Attachments {
			if strings.HasPrefix(a.Mime, "image/") {
				refs = append(refs, a)
			}
		}
	}
	return refs
}

// refNumber parses "图2", "2", "#2" or "第2张" into 2 (0 when there is no number).
func refNumber(ref string) int {
	n, seen := 0, false
	for _, r := range ref {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			seen = true
			if n > 100000 {
				return 0
			}
		} else if seen {
			break
		}
	}
	return n
}

// ---- daily limit ----

// imageLimit is the admin's per-person daily picture limit (0 = no limit).
func (s *Service) imageLimit(ctx context.Context) int {
	v, _ := s.Store.GetSetting(ctx, "image_daily_limit")
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// startOfDay is today's midnight in China time, in unix ms.
func startOfDay() int64 {
	now := time.Now().In(shanghai)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai).UnixMilli()
}

// reserveImages books up to n pictures against the user's daily limit and
// returns how many may be drawn. When limit > 0 the caller releases them with
// releaseImages once drawn (by then they are in image_log). Admins are not
// limited (limit 0).
func (s *Service) reserveImages(ctx context.Context, u *store.User, n int) (allowed, limit int) {
	if u.Role == "admin" {
		return n, 0
	}
	limit = s.imageLimit(ctx)
	if limit == 0 {
		return n, 0
	}
	s.drawMu.Lock()
	defer s.drawMu.Unlock()
	used, err := s.Store.ImagesSince(ctx, u.ID, startOfDay())
	if err != nil {
		s.Log.Error("count images", "user_id", u.ID, "err", err)
		return n, 0 // the limit is a guard against runaway cost, not worth failing the reply
	}
	allowed = max(0, min(n, limit-used-s.drawPending[u.ID]))
	if s.drawPending == nil {
		s.drawPending = map[string]int{}
	}
	s.drawPending[u.ID] += allowed
	return allowed, limit
}

func (s *Service) releaseImages(userID string, n int) {
	s.drawMu.Lock()
	defer s.drawMu.Unlock()
	if s.drawPending == nil {
		return
	}
	if s.drawPending[userID] -= n; s.drawPending[userID] <= 0 {
		delete(s.drawPending, userID)
	}
}

// ImagesLeftToday is how many more pictures the user may draw today (-1 = no limit).
func (s *Service) ImagesLeftToday(ctx context.Context, u *store.User) int {
	limit := s.imageLimit(ctx)
	if limit == 0 || u.Role == "admin" {
		return -1
	}
	used, err := s.Store.ImagesSince(ctx, u.ID, startOfDay())
	if err != nil {
		return -1
	}
	return max(0, limit-used)
}

// SaveImage stores a generated picture as a JPEG file owned by userID
// (upstreams return multi-megabyte PNGs; JPEG is a fraction of that).
func (s *Service) SaveImage(ctx context.Context, userID string, data []byte) (*store.File, error) {
	return s.saveImage(ctx, userID, data, false)
}

func (s *Service) saveImage(ctx context.Context, userID string, data []byte, private bool) (*store.File, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}
	// JPEG has no alpha: put transparent pictures on white instead of black.
	b := img.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.White, image.Point{}, draw.Src)
	draw.Draw(flat, b, img, b.Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	sum := sha256.Sum256(out)
	f := &store.File{ID: store.NewID(), UserID: userID, Mime: "image/jpeg", Size: int64(len(out)), SHA256: hex.EncodeToString(sum[:]), Width: b.Dx(), Height: b.Dy(), Private: private}
	dir := filepath.Join(s.DataDir, "files", time.Now().Format("2006-01"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f.Path = filepath.Join(dir, f.ID+".jpg")
	if err := os.WriteFile(f.Path, out, 0o644); err != nil {
		return nil, err
	}
	if err := s.Store.CreateFile(ctx, f); err != nil {
		os.Remove(f.Path)
		return nil, err
	}
	return f, nil
}

func friendlyImageError(err error) string {
	var ae *llm.APIError
	if errors.As(err, &ae) {
		low := strings.ToLower(ae.Message)
		switch {
		case isSafetyRejection(ae.Message):
			return "这个内容触发了画图服务的安全限制，画不了"
		case ae.Status == 429:
			return "画图服务太忙了，请稍后再试"
		case ae.Status == 401 || ae.Status == 403:
			return "画图服务认证失败，需要管理员检查"
		case ae.Status == 402 || strings.Contains(low, "quota") || strings.Contains(low, "balance") || strings.Contains(low, "insufficient"):
			return "画图服务余额不足，需要管理员充值"
		case ae.Status == 404:
			return "画图模型暂时用不了，需要管理员检查"
		case ae.Status >= 500:
			return "画图服务暂时出错了"
		}
		return "画图服务返回错误：" + clip(ae.Message, 120)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "画图超时了"
	}
	return "连接画图服务出了问题"
}

func isSafetyRejection(msg string) bool {
	low := strings.ToLower(msg)
	for _, k := range []string{"safety", "moderation", "policy", "rejected", "violat", "sensitive"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
