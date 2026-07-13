package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func writeChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	binary.Write(buf, binary.BigEndian, uint32(len(data)))
	buf.WriteString(chunkType)
	buf.Write(data)
	h := crc32.NewIEEE()
	h.Write([]byte(chunkType))
	h.Write(data)
	binary.Write(buf, binary.BigEndian, h.Sum32())
}

func TestExtractPNGMetadata(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")

	writeChunk(&buf, "IHDR", make([]byte, 13))

	var textData bytes.Buffer
	textData.WriteString("parameters")
	textData.WriteByte(0)
	textData.WriteString("cybernetic owl prompt\nNegative prompt: bad quality\nSteps: 20, Sampler: Euler a, Seed: 1234")
	writeChunk(&buf, "tEXt", textData.Bytes())

	writeChunk(&buf, "IEND", nil)

	meta, err := ExtractPNGMetadata(&buf)
	if err != nil {
		t.Fatalf("failed to extract metadata: %v", err)
	}

	expectedParams := "cybernetic owl prompt\nNegative prompt: bad quality\nSteps: 20, Sampler: Euler a, Seed: 1234"
	if meta["parameters"] != expectedParams {
		t.Errorf("unexpected parameters: got %q, expected %q", meta["parameters"], expectedParams)
	}

	sdMeta := ParseSDParameters(meta["parameters"])
	if sdMeta["sd_prompt"] != "cybernetic owl prompt" {
		t.Errorf("unexpected prompt: got %q, expected 'cybernetic owl prompt'", sdMeta["sd_prompt"])
	}
	if sdMeta["sd_negative_prompt"] != "bad quality" {
		t.Errorf("unexpected negative prompt: got %q, expected 'bad quality'", sdMeta["sd_negative_prompt"])
	}
	if sdMeta["sd_seed"] != "1234" {
		t.Errorf("unexpected seed: got %q, expected '1234'", sdMeta["sd_seed"])
	}
	if sdMeta["sd_sampler"] != "Euler a" {
		t.Errorf("unexpected sampler: got %q, expected 'Euler a'", sdMeta["sd_sampler"])
	}
}
