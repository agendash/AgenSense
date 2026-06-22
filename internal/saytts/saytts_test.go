package saytts

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPCMFromWAV(t *testing.T) {
	pcm := bytes.Repeat([]byte{0x01, 0x02}, 16)
	wav := testWAV(24000, 1, pcm)

	gotPCM, gotFormat, err := PCMFromWAV(wav)
	if err != nil {
		t.Fatalf("PCMFromWAV() error = %v", err)
	}
	if !bytes.Equal(gotPCM, pcm) {
		t.Fatalf("pcm = %d bytes, want %d bytes", len(gotPCM), len(pcm))
	}
	if gotFormat.Codec != "pcm_s16le" || gotFormat.SampleRateHz != 24000 || gotFormat.Channels != 1 {
		t.Fatalf("format = %+v, want 24k mono pcm", gotFormat)
	}
}

func testWAV(sampleRate, channels int, pcm []byte) []byte {
	const bytesPerSample = 2
	byteRate := sampleRate * channels * bytesPerSample
	blockAlign := channels * bytesPerSample
	dataSize := len(pcm)
	riffSize := 36 + dataSize

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(riffSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bytesPerSample*8))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(pcm)
	return buf.Bytes()
}
