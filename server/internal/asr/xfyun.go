package asr

import (
	"context"

	"nuan/internal/store"
)

// xfyunDriver is the extension point for iFlytek (科大讯飞) WebSocket Streaming IAT.
// When implementing:
//  1. Build HMAC-SHA256 RFC1123 date signature for wss://iat-api.xfyun.cn/v2/iat
//  2. Encode 16kHz PCM chunks as Base64 inside JSON frames (status 0 -> 1 -> 2)
//  3. Apply dynamic correction (pgs == "rpl" with rg range) into segments and emit Event{FullText}
//  4. Set Available: true on the KindXfyun entry in Presets and IsReady().
type xfyunDriver struct {
	cfg *store.ASRConfig
}

func (d *xfyunDriver) Test(ctx context.Context) error {
	return ErrNotImplemented
}

func (d *xfyunDriver) OpenStream(ctx context.Context) (StreamSession, error) {
	return nil, ErrNotImplemented
}
