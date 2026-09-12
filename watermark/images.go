package watermark

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

const maxMetadataBytes = 16 << 20

func inflateMetadata(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxMetadataBytes {
		return nil, fmt.Errorf("compressed metadata exceeds %d bytes", maxMetadataBytes)
	}
	return out, nil
}

func pngText(kind string, payload []byte) (string, string, error) {
	i := bytes.IndexByte(payload, 0)
	if i < 1 || i > 79 {
		return "", "", fmt.Errorf("invalid PNG text keyword")
	}
	key := string(payload[:i])
	rest := payload[i+1:]
	switch kind {
	case "tEXt":
		return key, string(rest), nil
	case "zTXt":
		if len(rest) < 1 || rest[0] != 0 {
			return "", "", fmt.Errorf("invalid PNG compression method")
		}
		out, err := inflateMetadata(rest[1:])
		return key, string(out), err
	case "iTXt":
		if len(rest) < 2 || rest[0] > 1 || rest[1] != 0 {
			return "", "", fmt.Errorf("invalid PNG international text header")
		}
		compressed := rest[0] == 1
		rest = rest[2:]
		for j := 0; j < 2; j++ {
			k := bytes.IndexByte(rest, 0)
			if k < 0 {
				return "", "", fmt.Errorf("truncated PNG international text")
			}
			rest = rest[k+1:]
		}
		if compressed {
			var err error
			rest, err = inflateMetadata(rest)
			if err != nil {
				return "", "", err
			}
		}
		return key, string(rest), nil
	}
	return "", "", fmt.Errorf("unknown PNG text chunk")
}

func cleanPNG(data []byte) ([]byte, []Finding, error) {
	out := append([]byte{}, pngSignature...)
	var hits []Finding
	pos := 8
	ihdr, idat, iend := false, false, false
	budget := 0
	for pos < len(data) {
		if len(data)-pos < 12 {
			return nil, nil, fmt.Errorf("truncated PNG chunk")
		}
		n := uint64(binary.BigEndian.Uint32(data[pos:]))
		if n > uint64(len(data)-pos-12) {
			return nil, nil, fmt.Errorf("PNG chunk exceeds file")
		}
		end := pos + 12 + int(n)
		typ := string(data[pos+4 : pos+8])
		payload := data[pos+8 : end-4]
		if crc32.ChecksumIEEE(data[pos+4:end-4]) != binary.BigEndian.Uint32(data[end-4:end]) {
			return nil, nil, fmt.Errorf("PNG %s checksum mismatch", typ)
		}
		if !ihdr && typ != "IHDR" {
			return nil, nil, fmt.Errorf("PNG must start with IHDR")
		}
		drop := false
		detail := typ
		switch typ {
		case "IHDR":
			if ihdr || n != 13 {
				return nil, nil, fmt.Errorf("invalid PNG IHDR")
			}
			ihdr = true
		case "IDAT":
			idat = true
		case "IEND":
			if n != 0 || !idat || end != len(data) {
				return nil, nil, fmt.Errorf("invalid PNG IEND or trailing bytes")
			}
			iend = true
		case "caBX":
			drop = true
			detail = "PNG caBX JUMBF/C2PA metadata"
		case "tEXt", "zTXt", "iTXt":
			key, value, err := pngText(typ, payload)
			if err != nil {
				return nil, nil, err
			}
			budget += len(value)
			if budget > maxMetadataBytes {
				return nil, nil, fmt.Errorf("PNG metadata exceeds limit")
			}
			drop = metadataHit(key, value) || hasXMPProvenance([]byte(value))
			detail = "PNG " + typ + " field " + key
		case "eXIf":
			drop = metadataBytes(payload)
			detail = "PNG EXIF provenance metadata"
		}
		if drop {
			hits = append(hits, finding("provenance_metadata", detail))
		} else {
			out = append(out, data[pos:end]...)
		}
		pos = end
	}
	if !iend {
		return nil, nil, fmt.Errorf("PNG has no IEND")
	}
	return out, hits, nil
}

// JPEG entropy bytes, quantization tables, colour profiles, and frame headers
// are copied verbatim. Segment parsing continues after every progressive scan.
func cleanJPEG(data []byte) ([]byte, []Finding, error) {
	out := append([]byte{}, data[:2]...)
	var hits []Finding
	pos := 2
	entropy := false
	sawScan := false
	for pos < len(data) {
		if entropy {
			start := pos
			for pos < len(data) {
				if data[pos] != 0xFF {
					pos++
					continue
				}
				j := pos + 1
				for j < len(data) && data[j] == 0xFF {
					j++
				}
				if j >= len(data) {
					return nil, nil, fmt.Errorf("truncated JPEG scan")
				}
				if data[j] == 0 || between(rune(data[j]), 0xD0, 0xD7) {
					pos = j + 1
					continue
				}
				break
			}
			out = append(out, data[start:pos]...)
			entropy = false
		}
		if pos >= len(data) || data[pos] != 0xFF {
			return nil, nil, fmt.Errorf("invalid JPEG marker at %d", pos)
		}
		start := pos
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			return nil, nil, fmt.Errorf("truncated JPEG marker")
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 {
			if pos != len(data) || !sawScan {
				return nil, nil, fmt.Errorf("JPEG has no scan or has trailing bytes")
			}
			out = append(out, data[start:pos]...)
			return out, hits, nil
		}
		if marker == 0x00 || marker == 0xD8 || between(rune(marker), 0xD0, 0xD7) {
			return nil, nil, fmt.Errorf("unexpected JPEG marker")
		}
		if marker == 0x01 {
			out = append(out, data[start:pos]...)
			continue
		}
		if len(data)-pos < 2 {
			return nil, nil, fmt.Errorf("truncated JPEG segment length")
		}
		n := int(binary.BigEndian.Uint16(data[pos:]))
		if n < 2 || n > len(data)-pos {
			return nil, nil, fmt.Errorf("invalid JPEG segment length")
		}
		end := pos + n
		payload := data[pos+2 : end]
		drop := false
		detail := ""
		switch marker {
		case 0xE1, 0xED:
			drop = metadataBytes(payload)
			detail = fmt.Sprintf("JPEG APP%d provenance metadata", marker-0xE0)
			// Generator product names count only in explicit XMP creator fields.
			if !drop && bytes.Contains(payload, []byte("CreatorTool")) {
				drop = products.Match(payload)
			}
		case 0xEB:
			// APP11 JP segments are JUMBF carriers. Remove all fragments,
			// including continuations that do not repeat the C2PA label.
			drop = bytes.HasPrefix(payload, []byte("JP")) || metadataBytes(payload)
			detail = "JPEG APP11 JUMBF/provenance metadata"
		case 0xFE:
			drop = metadataBytes(payload)
			detail = "JPEG provenance comment"
		}
		if drop {
			hits = append(hits, finding("provenance_metadata", detail))
		} else {
			out = append(out, data[start:end]...)
		}
		pos = end
		if marker == 0xDA {
			entropy = true
			sawScan = true
		}
	}
	return nil, nil, fmt.Errorf("JPEG has no EOI")
}

func cleanWebP(data []byte) ([]byte, []Finding, error) {
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return nil, nil, fmt.Errorf("WebP RIFF size mismatch")
	}
	out := append([]byte{}, data[:12]...)
	var hits []Finding
	pos := 12
	clearFlags := byte(0)
	image := false
	vp8x := -1
	for pos < len(data) {
		if len(data)-pos < 8 {
			return nil, nil, fmt.Errorf("truncated WebP chunk")
		}
		n := uint64(binary.LittleEndian.Uint32(data[pos+4:]))
		padded := n + (n & 1)
		if padded > uint64(len(data)-pos-8) {
			return nil, nil, fmt.Errorf("WebP chunk exceeds file")
		}
		end := pos + 8 + int(padded)
		typ := string(data[pos : pos+4])
		payload := data[pos+8 : pos+8+int(n)]
		drop := false
		switch typ {
		case "VP8 ", "VP8L", "ANMF":
			image = true
		case "VP8X":
			if n != 10 || vp8x >= 0 {
				return nil, nil, fmt.Errorf("invalid WebP VP8X")
			}
			vp8x = len(out) + 8
		case "C2PA", "JUMB":
			drop = true
		case "XMP ":
			drop = metadataBytes(payload) || (bytes.Contains(payload, []byte("CreatorTool")) && products.Match(payload))
			if drop {
				clearFlags |= 0x04
			}
		case "EXIF":
			drop = metadataBytes(payload)
			if drop {
				clearFlags |= 0x08
			}
		}
		if drop {
			hits = append(hits, finding("provenance_metadata", "WebP "+strings.TrimSpace(typ)+" metadata"))
		} else {
			out = append(out, data[pos:end]...)
		}
		pos = end
	}
	if !image {
		return nil, nil, fmt.Errorf("WebP has no image chunk")
	}
	if vp8x >= 0 {
		out[vp8x] &= ^clearFlags
	}
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out, hits, nil
}
