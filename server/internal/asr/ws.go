package asr

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	wsMaxMessage  = 1 << 20 // 1 MiB
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

// WSConn is a minimal RFC 6455 WebSocket connection supporting text, binary,
// ping/pong, and close frames without external dependencies.
type WSConn struct {
	conn     net.Conn
	br       *bufio.Reader
	isClient bool

	writeMu sync.Mutex
	closed  bool
}

// WSDialFunc dials a WebSocket URL and completes the opening handshake.
type WSDialFunc func(ctx context.Context, rawURL string) (*WSConn, error)

// DialWS connects to a ws:// or wss:// URL and performs the RFC 6455 handshake.
func DialWS(ctx context.Context, rawURL string) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("bad websocket url: %w", err)
	}
	var hostPort string
	switch u.Scheme {
	case "wss":
		hostPort = u.Host
		if u.Port() == "" {
			hostPort = net.JoinHostPort(u.Hostname(), "443")
		}
	case "ws":
		hostPort = u.Host
		if u.Port() == "" {
			hostPort = net.JoinHostPort(u.Hostname(), "80")
		}
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}

	var conn net.Conn
	var d net.Dialer
	if u.Scheme == "wss" {
		td := tls.Dialer{
			NetDialer: &d,
			Config:    &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12},
		}
		conn, err = td.DialContext(ctx, "tcp", hostPort)
	} else {
		conn, err = d.DialContext(ctx, "tcp", hostPort)
	}
	if err != nil {
		return nil, err
	}
	return ClientHandshake(ctx, conn, u)
}

// ClientHandshake runs the client side of an RFC 6455 opening handshake over an established net.Conn.
func ClientHandshake(ctx context.Context, conn net.Conn, u *url.URL) (*WSConn, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
		defer conn.SetDeadline(time.Time{})
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	reqURI := u.RequestURI()
	if reqURI == "" {
		reqURI = "/"
	}
	var reqBuf strings.Builder
	fmt.Fprintf(&reqBuf, "GET %s HTTP/1.1\r\n", reqURI)
	fmt.Fprintf(&reqBuf, "Host: %s\r\n", u.Host)
	reqBuf.WriteString("Upgrade: websocket\r\n")
	reqBuf.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&reqBuf, "Sec-WebSocket-Key: %s\r\n", wsKey)
	reqBuf.WriteString("Sec-WebSocket-Version: 13\r\n\r\n")

	if _, err := io.WriteString(conn, reqBuf.String()); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReaderSize(conn, 16*1024)
	dummyReq := &http.Request{Method: http.MethodGet, URL: u}
	resp, err := http.ReadResponse(br, dummyReq)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		conn.Close()
		return nil, fmt.Errorf("websocket handshake HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != wsAcceptKey(wsKey) {
		conn.Close()
		return nil, errors.New("invalid Sec-WebSocket-Accept")
	}
	return &WSConn{conn: conn, br: br, isClient: true}, nil
}

// UpgradeWS upgrades an incoming HTTP request to a server-side WebSocket connection.
func UpgradeWS(w http.ResponseWriter, r *http.Request) (*WSConn, error) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, errors.New("websocket requires GET")
	}
	wsKey := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if !headerContainsToken(r.Header.Get("Upgrade"), "websocket") || wsKey == "" {
		http.Error(w, "bad websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("missing websocket upgrade headers")
	}

	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	conn, rw, err := rc.Hijack()
	if err != nil {
		http.Error(w, "websocket hijack unsupported", http.StatusInternalServerError)
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	accept := wsAcceptKey(wsKey)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &WSConn{conn: conn, br: rw.Reader, isClient: false}, nil
}

func wsAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContainsToken(hdr, token string) bool {
	for _, p := range strings.Split(hdr, ",") {
		if strings.EqualFold(strings.TrimSpace(p), token) {
			return true
		}
	}
	return false
}

func (c *WSConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// ReadMessage reads the next complete data message (text or binary), handling
// fragmentation, ping/pong, and close frames automatically.
func (c *WSConn) ReadMessage() (opcode byte, payload []byte, err error) {
	var msgOp byte
	var assembled []byte
	inFragment := false

	for {
		fin, op, frame, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case OpPing:
			_ = c.writeFrame(OpPong, frame)
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.writeFrame(OpClose, nil)
			_ = c.Close()
			return OpClose, nil, io.EOF
		case OpText, OpBinary:
			if inFragment {
				return 0, nil, errors.New("unexpected new data frame during fragmentation")
			}
			if fin {
				return op, frame, nil
			}
			inFragment = true
			msgOp = op
			assembled = append(assembled[:0], frame...)
		case OpContinuation:
			if !inFragment {
				return 0, nil, errors.New("unexpected continuation frame")
			}
			if len(assembled)+len(frame) > wsMaxMessage {
				return 0, nil, errors.New("websocket message too large")
			}
			assembled = append(assembled, frame...)
			if fin {
				return msgOp, assembled, nil
			}
		default:
			return 0, nil, fmt.Errorf("unsupported websocket opcode %d", op)
		}
	}
}

func (c *WSConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return false, 0, nil, err
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if length > wsMaxMessage {
		return false, 0, nil, errors.New("websocket frame exceeds limit")
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, int(length))
	if length > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return false, 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= maskKey[i&3]
			}
		}
	}
	return fin, opcode, payload, nil
}

func (c *WSConn) WriteText(msg []byte) error   { return c.writeFrame(OpText, msg) }
func (c *WSConn) WriteBinary(msg []byte) error { return c.writeFrame(OpBinary, msg) }
func (c *WSConn) WritePing() error             { return c.writeFrame(OpPing, nil) }

func (c *WSConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return net.ErrClosed
	}

	var hdr [14]byte
	hdr[0] = 0x80 | (opcode & 0x0F) // FIN=1
	n := 2
	maskBit := byte(0)
	if c.isClient {
		maskBit = 0x80
	}
	l := len(payload)
	switch {
	case l < 126:
		hdr[1] = maskBit | byte(l)
	case l <= 65535:
		hdr[1] = maskBit | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(l))
		n = 4
	default:
		hdr[1] = maskBit | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(l))
		n = 10
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	defer c.conn.SetWriteDeadline(time.Time{})

	if !c.isClient {
		if _, err := c.conn.Write(hdr[:n]); err != nil {
			return err
		}
		if l > 0 {
			_, err := c.conn.Write(payload)
			return err
		}
		return nil
	}

	var maskKey [4]byte
	_, _ = rand.Read(maskKey[:])
	copy(hdr[n:n+4], maskKey[:])
	n += 4

	buf := make([]byte, n+l)
	copy(buf, hdr[:n])
	for i := 0; i < l; i++ {
		buf[n+i] = payload[i] ^ maskKey[i&3]
	}
	_, err := c.conn.Write(buf)
	return err
}

func (c *WSConn) Close() error {
	c.writeMu.Lock()
	if c.closed {
		c.writeMu.Unlock()
		return nil
	}
	c.closed = true
	c.writeMu.Unlock()
	return c.conn.Close()
}
