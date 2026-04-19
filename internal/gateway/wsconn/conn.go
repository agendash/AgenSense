package wsconn

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// OpCode identifies a websocket frame type.
type OpCode byte

const (
	OpContinuation OpCode = 0x0
	OpText         OpCode = 0x1
	OpBinary       OpCode = 0x2
	OpClose        OpCode = 0x8
	OpPing         OpCode = 0x9
	OpPong         OpCode = 0xA
)

// Conn is a minimal websocket connection that supports one-frame text and binary messages.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer

	writeMu sync.Mutex
}

// Upgrade upgrades an HTTP request to a websocket connection without external dependencies.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return nil, errors.New("websocket upgrade: missing Connection: upgrade")
	}
	if !headerContainsToken(r.Header, "Upgrade", "websocket") {
		return nil, errors.New("websocket upgrade: missing Upgrade: websocket")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("websocket upgrade: unsupported version")
	}

	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("websocket upgrade: missing Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("websocket upgrade: response writer does not support hijacking")
	}

	rawConn, buf, err := hj.Hijack()
	if err != nil {
		return nil, fmt.Errorf("websocket upgrade: hijack: %w", err)
	}

	accept := computeAcceptKey(key)
	if _, err := fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
		rawConn.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(buf, "Upgrade: websocket\r\n"); err != nil {
		rawConn.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(buf, "Connection: Upgrade\r\n"); err != nil {
		rawConn.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(buf, "Sec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		rawConn.Close()
		return nil, err
	}
	if err := buf.Flush(); err != nil {
		rawConn.Close()
		return nil, err
	}

	return &Conn{
		conn: rawConn,
		r:    buf.Reader,
		w:    buf.Writer,
	}, nil
}

// ReadFrame reads the next complete frame payload.
func (c *Conn) ReadFrame() (OpCode, []byte, error) {
	opcode, payload, err := readFrame(c.r)
	if err != nil {
		return 0, nil, err
	}
	if opcode == OpPing {
		if err := c.WriteFrame(OpPong, payload); err != nil {
			return 0, nil, err
		}
		return c.ReadFrame()
	}
	return opcode, payload, nil
}

// WriteText writes a text frame.
func (c *Conn) WriteText(data []byte) error {
	return c.WriteFrame(OpText, data)
}

// WriteBinary writes a binary frame.
func (c *Conn) WriteBinary(data []byte) error {
	return c.WriteFrame(OpBinary, data)
}

// WriteClose writes a close frame.
func (c *Conn) WriteClose() error {
	return c.WriteFrame(OpClose, nil)
}

// WriteFrame writes a single frame to the connection.
func (c *Conn) WriteFrame(opcode OpCode, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(c.w, opcode, payload)
}

// Close closes the underlying network connection.
func (c *Conn) Close() error {
	return c.conn.Close()
}

func readFrame(r *bufio.Reader) (OpCode, []byte, error) {
	header, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if header&0x80 == 0 {
		return 0, nil, errors.New("websocket read: fragmented frames are not supported")
	}

	opcode := OpCode(header & 0x0F)
	lengthByte, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}

	masked := lengthByte&0x80 != 0
	payloadLength := int64(lengthByte & 0x7F)
	switch payloadLength {
	case 126:
		var v uint16
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			return 0, nil, err
		}
		payloadLength = int64(v)
	case 127:
		var v uint64
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			return 0, nil, err
		}
		if v > 16<<20 {
			return 0, nil, errors.New("websocket read: frame too large")
		}
		payloadLength = int64(v)
	}
	if payloadLength < 0 || payloadLength > 16<<20 {
		return 0, nil, errors.New("websocket read: frame too large")
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

func writeFrame(w *bufio.Writer, opcode OpCode, payload []byte) error {
	if err := w.WriteByte(0x80 | byte(opcode)); err != nil {
		return err
	}

	length := len(payload)
	switch {
	case length < 126:
		if err := w.WriteByte(byte(length)); err != nil {
			return err
		}
	case length <= 0xFFFF:
		if err := w.WriteByte(126); err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint16(length)); err != nil {
			return err
		}
	default:
		if err := w.WriteByte(127); err != nil {
			return err
		}
		if err := binary.Write(w, binary.BigEndian, uint64(length)); err != nil {
			return err
		}
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}

func computeAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContainsToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
