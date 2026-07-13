package engine

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// ExtractPNGMetadata parses PNG file binary signature and extracts tEXt/iTXt chunk metadata.
func ExtractPNGMetadata(r io.Reader) (map[string]string, error) {
	var sig [8]byte
	if _, err := io.ReadFull(r, sig[:]); err != nil {
		return nil, err
	}
	if !bytes.Equal(sig[:], []byte("\x89PNG\r\n\x1a\n")) {
		return nil, fmt.Errorf("not a valid PNG file")
	}

	metadata := make(map[string]string)

	for {
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}

		var chunkType [4]byte
		if _, err := io.ReadFull(r, chunkType[:]); err != nil {
			return nil, err
		}

		// Read chunk data
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}

		// Read CRC
		var crc [4]byte
		if _, err := io.ReadFull(r, crc[:]); err != nil {
			return nil, err
		}

		chunkName := string(chunkType[:])
		if chunkName == "tEXt" {
			parts := bytes.SplitN(data, []byte{0}, 2)
			if len(parts) == 2 {
				key := string(parts[0])
				val := string(parts[1])
				metadata[key] = val
			}
		} else if chunkName == "iTXt" {
			parts := bytes.SplitN(data, []byte{0}, 4)
			if len(parts) >= 4 {
				key := string(parts[0])
				if len(parts[1]) >= 2 {
					compFlag := parts[1][0]
					subparts := bytes.SplitN(parts[3], []byte{0}, 2)
					if len(subparts) == 2 {
						textBytes := subparts[1]
						if compFlag == 0 {
							metadata[key] = string(textBytes)
						} else {
							zr, err := zlib.NewReader(bytes.NewReader(textBytes))
							if err == nil {
								decompressed, err := io.ReadAll(zr)
								if err == nil {
									metadata[key] = string(decompressed)
								}
								zr.Close()
							}
						}
					}
				}
			}
		} else if chunkName == "IEND" {
			break
		}
	}
	return metadata, nil
}

// ParseSDParameters parses a Stable Diffusion parameter block into structured metadata keys.
func ParseSDParameters(params string) map[string]string {
	res := make(map[string]string)
	res["sd_raw"] = params

	lines := strings.Split(params, "\n")
	if len(lines) == 0 {
		return res
	}

	// Positive Prompt is typically line 0
	res["sd_prompt"] = strings.TrimSpace(lines[0])

	// Parse subsequent lines
	var paramLine string
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Negative prompt:") {
			res["sd_negative_prompt"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "Negative prompt:"))
		} else if strings.Contains(trimmed, "Steps:") {
			paramLine = trimmed
		}
	}

	// Parse key-value pairs in parameters line, e.g. "Steps: 20, Sampler: Euler a, CFG scale: 7, Seed: 12345678, Size: 512x512"
	if paramLine != "" {
		parts := strings.Split(paramLine, ",")
		for _, part := range parts {
			kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
			if len(kv) == 2 {
				key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(kv[0]), " ", "_"))
				val := strings.TrimSpace(kv[1])
				res["sd_"+key] = val
			}
		}
	}

	return res
}
