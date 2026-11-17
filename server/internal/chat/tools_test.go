package chat

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nuan/internal/store"
)

func TestSaveImageFlattensTransparencyOnWhite(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u := &store.User{Username: "u", DisplayName: "u", Role: "member", PasswordHash: "x"}
	if err := st.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	s := NewService(st, dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	src := image.NewNRGBA(image.Rect(0, 0, 8, 4)) // fully transparent
	src.Set(7, 3, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)
	f, err := s.SaveImage(t.Context(), u.ID, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if f.Mime != "image/jpeg" || f.Width != 8 || f.Height != 4 {
		t.Fatalf("file: %+v", f)
	}
	raw, _ := os.ReadFile(f.Path)
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatalf("transparent area should be white, got %d %d %d", r>>8, g>>8, b>>8)
	}
	if got, err := st.GetFile(t.Context(), f.ID, u.ID); err != nil || got.Path != f.Path {
		t.Fatalf("file row: %+v %v", got, err)
	}
}

func TestImageSize(t *testing.T) {
	for _, c := range []struct{ model, aspect, want string }{
		{"gpt-image-2.6-sunburst", "portrait", "1024x1536"},
		{"gpt-image-1", "", ""},
		{"dall-e-3", "landscape", "1792x1024"},
		{"dall-e-2", "portrait", ""},
	} {
		if got := imageSize(c.model, c.aspect); got != c.want {
			t.Errorf("%s %s: %q want %q", c.model, c.aspect, got, c.want)
		}
	}
}

func TestRefNumber(t *testing.T) {
	for in, want := range map[string]int{"图2": 2, "2": 2, "#12": 12, "第3张": 3, "图": 0, "": 0, "图1和图2": 1} {
		if got := refNumber(in); got != want {
			t.Errorf("%q: %d want %d", in, got, want)
		}
	}
}

func TestDrawnNoteNumbersPictures(t *testing.T) {
	atts := []store.Attachment{{FileID: "a", Prompt: "dog"}, {FileID: "b", Prompt: "black dog", Source: "a"}}
	got := drawnNote(atts, map[string]int{"a": 3, "b": 4})
	if !strings.Contains(got, "第 1 张是图3，提示词：dog") || !strings.Contains(got, "第 2 张是图4，由图3 修改而来，提示词：black dog") {
		t.Fatalf("note: %s", got)
	}
	if got := drawnNote(atts, nil); strings.Contains(got, "图3") || !strings.Contains(got, "第 1 张，提示词：dog") {
		t.Fatalf("note without numbers: %s", got)
	}
}
