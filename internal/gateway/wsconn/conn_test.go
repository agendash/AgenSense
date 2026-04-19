package wsconn

import (
	"bufio"
	"bytes"
	"testing"
)

func TestWriteFrameReadFrameRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	reader := bufio.NewReader(&buf)

	payload := []byte("hello websocket")
	if err := writeFrame(writer, OpText, payload); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}

	opcode, got, err := readFrame(reader)
	if err != nil {
		t.Fatalf("readFrame() error = %v", err)
	}
	if opcode != OpText {
		t.Fatalf("opcode = %v, want %v", opcode, OpText)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}
