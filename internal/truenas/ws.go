package truenas

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// A small WebSocket client (RFC 6455), just enough for TrueNAS's JSON-RPC API:
// one text message per call, no extensions, no subprotocols. It exists because
// Homie depends on nothing but the Go standard library.

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// maxMessage bounds one reply: pools and disks come to a few kilobytes,
	// the reporting graphs to a few hundred.
	maxMessage = 8 << 20
)

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	id   int
}

// dialWS opens a TLS connection to host (host:port) and upgrades it. The
// certificate is not verified: a NAS on a home network usually has a
// self-signed one, the same posture as Homie's service checks.
func dialWS(ctx context.Context, host, path string) (*wsConn, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    &tls.Config{InsecureSkipVerify: true, ServerName: hostOnly(host)},
	}
	raw, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	c := &wsConn{conn: raw, r: bufio.NewReader(raw)}
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}

	key := make([]byte, 16)
	_, _ = rand.Read(key)
	nonce := base64.StdEncoding.EncodeToString(key)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nOrigin: https://%s\r\nUser-Agent: homie-widget\r\n\r\n",
		path, host, nonce, host)
	if _, err := io.WriteString(raw, req); err != nil {
		raw.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(c.r, &http.Request{Method: http.MethodGet})
	if err != nil {
		raw.Close()
		return nil, err
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(nonce + wsGUID))
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		raw.Close()
		return nil, errNotWebSocket
	}
	return c, nil
}

var errNotWebSocket = errors.New("the server did not accept a WebSocket connection")

func hostOnly(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil || net.ParseIP(h) != nil {
		return ""
	}
	return h
}

func (c *wsConn) Close() {
	_ = c.write(0x8, nil) // close frame, best effort
	_ = c.conn.Close()
}

// write sends one final frame. Frames from a client must be masked.
func (c *wsConn) write(opcode byte, payload []byte) error {
	n := len(payload)
	hdr := []byte{0x80 | opcode}
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 1<<16:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	hdr = append(hdr, mask...)
	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	_, err := c.conn.Write(append(hdr, masked...))
	return err
}

// read returns the next complete text or binary message, answering pings and
// joining fragments on the way.
func (c *wsConn) read() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.r, h[:]); err != nil {
			return nil, err
		}
		fin, opcode := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return nil, errors.New("reply too large")
		}
		var mask [4]byte
		masked := h[1]&0x80 != 0 // servers do not mask, but be tolerant
		if masked {
			if _, err := io.ReadFull(c.r, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			if err := c.write(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

// rpcError is an error answered by the server.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// call makes one JSON-RPC 2.0 call and decodes its result into out (which may
// be nil). Messages without the matching id are skipped.
func (c *wsConn) call(method string, params any, out any) error {
	c.id++
	id := c.id
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := c.write(0x1, req); err != nil {
		return err
	}
	for {
		msg, err := c.read()
		if err != nil {
			return err
		}
		var env struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if json.Unmarshal(msg, &env) != nil || env.ID == nil || *env.ID != id {
			continue
		}
		if env.Error != nil {
			return env.Error
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(env.Result, out)
	}
}

// hostPort returns the host:port to dial for an https URL.
func hostPort(rawURL string) (hostport, path string, err error) {
	u, err := parseHTTPS(rawURL)
	if err != nil {
		return "", "", err
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(strings.Trim(u.Hostname(), "[]"), "443")
	}
	return host, "/api/current", nil
}
