package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"sync"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nuan/internal/llm"
	"nuan/internal/store"
)

const (
	// Model turns per reply: the first may call tools (several pictures are
	// drawn in parallel), the last gets no tools so it must answer in words.
	maxToolRounds    = 2
	maxImagesPerTurn = 3
	imageTimeout     = 3 * time.Minute
)

// drawSlots bounds concurrent image generations server-wide (memory for decoding, upstream load).
var drawSlots = make(chan struct{}, 4)

// generate_image: the chat model writes the prompt, the server draws with the
// admin's image model and shows the picture in the reply.
var imageTool = llm.Tool{
	Name: "generate_image",
	Description: "Draw a picture and show it to the user. Call this whenever the user wants an image: draw, paint, " +
		"generate, design a poster / avatar / wallpaper / card / illustration, etc. One call draws one picture.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type": "string",
				"description": "Detailed description for the image model, in English: subject, style, composition, lighting, colors, mood. " +
					"Keep any text that must appear in the picture verbatim in its original language, in quotes.",
			},
			"image": map[string]any{
				"type": "string",
				"description": "Only when modifying an existing picture (one drawn earlier or a photo the user sent): its number shown in the conversation, " +
					"e.g. \"图2\". The prompt then says what to change and what to keep. Omit for a new picture.",
			},
			"aspect": map[string]any{
				"type": "string",
				"enum": []string{"square", "portrait", "landscape"},
				"description": "square by default; portrait for phone wallpapers, posters and full-body people; landscape for scenery and banners. " +
					"When modifying a picture, omit it to keep the original shape.",
			},
		},
		"required": []string{"prompt"},
	},
}

// imageSize maps the tool's aspect to a size the model family accepts ("" = model default).
func imageSize(upstream, aspect string) string {
	m := strings.ToLower(upstream)
	switch {
	case strings.Contains(m, "dall-e-2"):
		return "" // square only
	case strings.Contains(m, "dall-e-3"):
		return map[string]string{"square": "1024x1024", "portrait": "1024x1792", "landscape": "1792x1024"}[aspect]
	}
	return map[string]string{"square": "1024x1024", "portrait": "1024x1536", "landscape": "1536x1024"}[aspect]
}

const toolPrompt = `

# 画图
你可以调用 generate_image 工具画图，画好的图会直接显示在你的回复里。
- 用户想要图片时（画、生成、做一张海报/头像/壁纸/贺卡/表情等）直接调用，不要先追问细节；只有完全不知道要画什么时才问一句。
- prompt 用详细的英文写（主体、风格、构图、光线、色彩、氛围）；图里要出现的中文文字原样保留并加引号。
- 一次调用画一张；用户要几张不同的图时，在同一次回复里同时发起几次调用（最多 3 张）。用户没要求多张时只画一张。
- 对话里的每张图都有编号（如「图2」），用户发的照片和你画的图都有。用户想修改某张图（换颜色、加东西、换背景、改成卡通风格、修照片等）时，把它的编号填进 image 参数，prompt 用英文写清要改什么、哪些保持不变；用户说"这张""刚才那张"时一般指最近的一张。
- 想要全新的图时不要填 image。
- 画好后用一两句话自然地回应，不要复述提示词，不要输出链接或 JSON。
- 不要说自己不能画图。`

const noImagePrompt = `

你目前不能画图。如果用户想要你画图或生成图片，温和地说明管理员还没有开通画图功能；可以先用文字描述画面，或帮他写一段画图提示词。`

const otherModelDrawsPrompt = `

当前这个模型不能画图（别的模型可以）。如果用户想要你画图或生成图片，温和地告诉他：点屏幕顶部的模型名称，换一个模型再说一遍就能画。`

// imageModel returns the configured image model when it is usable.
func (s *Service) imageModel(ctx context.Context) (*store.Model, *store.Provider) {
	id, _ := s.Store.GetSetting(ctx, "image_model_id")
	if id == "" {
		return nil, nil
	}
	m, p, ok := s.usableAny(ctx, id)
	if !ok || m.Kind != store.KindImage {
		return nil, nil
	}
	return m, p
}

// ImageAvailable reports whether conversations can draw pictures.
func (s *Service) ImageAvailable(ctx context.Context) bool {
	m, _ := s.imageModel(ctx)
	return m != nil
}

// toolsFor returns the tools a chat model may use in this reply.
func (s *Service) toolsFor(ctx context.Context, m *store.Model) []llm.Tool {
	if !m.Tools || s.toolsRejected(m) {
		return nil
	}
	if im, _ := s.imageModel(ctx); im != nil {
		return []llm.Tool{imageTool}
	}
	return nil
}

// drawPrompt is the system prompt section about drawing for this reply.
func (s *Service) drawPrompt(ctx context.Context, canDraw bool) string {
	switch {
	case canDraw:
		return toolPrompt
	case s.ImageAvailable(ctx):
		return otherModelDrawsPrompt
	}
	return noImagePrompt
}

func toolKey(m *store.Model) string { return fmt.Sprintf("%s@%d", m.ID, m.UpdatedAt) }

// toolsRejected reports whether the upstream refused tools for this model
// (remembered until the model is edited, so nobody waits for the retry again).
func (s *Service) toolsRejected(m *store.Model) bool {
	_, ok := s.noTools.Load(toolKey(m))
	return ok
}

func (s *Service) markToolsRejected(m *store.Model) { s.noTools.Store(toolKey(m), true) }

// toolTurn carries the state of one reply's tool use.
type toolTurn struct {
	user   *store.User
	convID string
	asstID string
	run    *Run
	images int
	cost   float64
}

// runTools executes the calls of one round (pictures are drawn in parallel)
// and returns the text handed back to the model for each call, in order.
func (s *Service) runTools(ctx context.Context, t *toolTurn, calls []llm.ToolCall) []string {
	results := make([]string, len(calls))
	var jobs []drawJob
	im, ip := s.imageModel(ctx)
	var refs []store.Attachment
	refsLoaded := false
	for i, call := range calls {
		if call.Name != imageTool.Name {
			results[i] = "没有这个工具：" + call.Name
			continue
		}
		var args struct {
			Prompt string `json:"prompt"`
			Image  string `json:"image"`
			Aspect string `json:"aspect"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || strings.TrimSpace(args.Prompt) == "" {
			results[i] = "参数不对：需要 prompt（画面描述）。"
			continue
		}
		if im == nil {
			results[i] = "画图功能现在不可用（管理员关闭了画图模型）。请向用户说明。"
			continue
		}
		if t.images+len(jobs) >= maxImagesPerTurn {
			results[i] = fmt.Sprintf("这条回复最多画 %d 张，这张没有画。", maxImagesPerTurn)
			continue
		}
		j := drawJob{i: i, prompt: clip(strings.TrimSpace(args.Prompt), 4000), size: imageSize(im.UpstreamModel, args.Aspect)}
		if ref := strings.TrimSpace(args.Image); ref != "" {
			if !refsLoaded {
				refs, refsLoaded = s.imageRefs(ctx, t.convID, t.asstID), true
			}
			n := refNumber(ref)
			if n < 1 || n > len(refs) {
				results[i] = fmt.Sprintf("没有找到「%s」：这段对话里的图片编号是 图1 到 图%d。", ref, len(refs))
				if len(refs) == 0 {
					results[i] = "这段对话里还没有图片，没法修改；不填 image 就是画一张新图。"
				}
				continue
			}
			j.source = &refs[n-1]
			j.sourceNo = n
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		return results
	}

	// Daily limit: refuse what does not fit, draw the rest.
	allowed, limit := s.reserveImages(ctx, t.user, len(jobs))
	if limit > 0 {
		defer s.releaseImages(t.user.ID, allowed)
	}
	for _, j := range jobs[allowed:] {
		if allowed == 0 {
			results[j.i] = fmt.Sprintf("今天的画图次数已经用完了（每人每天 %d 张），明天再来。请温和地告诉用户，不要假装画好了。", limit)
		} else {
			results[j.i] = fmt.Sprintf("今天的画图次数只够再画 %d 张（每人每天 %d 张），这张没有画。请告诉用户。", allowed, limit)
		}
	}
	jobs = jobs[:allowed]
	if len(jobs) == 0 {
		return results
	}

	label := "正在画图…"
	edits := 0
	for _, j := range jobs {
		if j.source != nil {
			edits++
		}
	}
	switch {
	case len(jobs) > 1:
		label = fmt.Sprintf("正在画 %d 张图…", len(jobs))
	case edits == 1:
		label = "正在改图…"
	}
	t.run.setActivity(label)
	defer t.run.setActivity("")

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			att, cost, redrawn, err := s.drawImage(ctx, t.user.ID, im, ip, j)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					results[j.i] = "用户停止了画图。"
					return
				}
				s.Log.Warn("image generation failed", "message_id", t.asstID, "model", im.UpstreamModel, "edit", j.source != nil, "err", err)
				results[j.i] = "画图失败：" + friendlyImageError(err) + "。请用一两句话告诉用户没画成和原因，可以建议换个说法再试；不要假装已经画好。"
				return
			}
			t.images++
			t.cost += cost
			all := t.run.addAttachment(att)
			pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.Store.SetAttachments(pctx, t.asstID, all); err != nil {
				s.Log.Error("save attachments", "message_id", t.asstID, "err", err)
			}
			if err := s.Store.LogImage(pctx, t.user.ID, im.ID, cost); err != nil {
				s.Log.Error("log image", "message_id", t.asstID, "err", err)
			}
			cancel()
			s.Log.Info("image generated", "message_id", t.asstID, "model", im.UpstreamModel, "edit", j.source != nil, "redrawn", redrawn,
				"ms", time.Since(started).Milliseconds(), "cost", cost)
			switch {
			case redrawn:
				results[j.i] = fmt.Sprintf("画图模型不支持在原图上直接修改，已按图%d 的提示词加上修改要求重新画了一张，并显示给用户了；新图和原图会有些不同，请顺带说明。不要复述提示词，不要输出 JSON 或链接。", j.sourceNo)
			case j.source != nil:
				results[j.i] = "已经在原图基础上改好，新图片显示在聊天界面里给用户看了。请用一两句温暖自然的话告诉用户改好了，可以问要不要再调整；不要复述提示词，不要输出 JSON 或链接。"
			default:
				results[j.i] = "图片已经画好，并已显示在聊天界面里给用户看了。请用一两句温暖自然的话告诉用户画好了，可以简单说说画面或问要不要调整；不要复述提示词，不要输出 JSON 或链接。"
			}
		}()
	}
	wg.Wait()
	return results
}

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
func (s *Service) drawImage(ctx context.Context, userID string, im *store.Model, ip *store.Provider, j drawJob) (att store.Attachment, cost float64, redrawn bool, err error) {
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
	f, err := s.SaveImage(ctx, userID, res.Data)
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
	history, err := s.Store.ListMessages(ctx, convID, beforeID, 500)
	if err != nil {
		return nil
	}
	return collectRefs(history)
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
	f := &store.File{ID: store.NewID(), UserID: userID, Mime: "image/jpeg", Size: int64(len(out)), SHA256: hex.EncodeToString(sum[:]), Width: b.Dx(), Height: b.Dy()}
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
