package watermark

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

func selectorByte(r rune) (byte, bool) {
	if between(r, 0xFE00, 0xFE0F) {
		return byte(r - 0xFE00), true
	}
	if between(r, 0xE0100, 0xE01EF) {
		return byte(r - 0xE0100 + 16), true
	}
	return 0, false
}

// C2PA 2.3 Appendix A.7: FEFF + variation-selector encoded C2PATXT wrapper.
// Only framing is checked. Signatures, assertions and content binding are not.
func inspectTextManifests(text string, leadingBOM bool) ([]Finding, error) {
	var hits []Finding
	for pos := 0; pos < len(text); {
		r, size := utf8.DecodeRuneInString(text[pos:])
		start := pos
		body := pos + size
		if r != 0xFEFF {
			if pos != 0 || !leadingBOM {
				pos += size
				continue
			}
			body = pos // The decoder has already consumed the initial encoding BOM.
		}
		end := body
		var header [13]byte
		count := 0
		for end < len(text) {
			v, n := utf8.DecodeRuneInString(text[end:])
			b, ok := selectorByte(v)
			if !ok {
				break
			}
			if count < len(header) {
				header[count] = b
			}
			count++
			end += n
		}
		pos = body
		if end > pos {
			pos = end
		}
		if pos <= start {
			pos = start + size
		}
		if count < 8 || string(header[:8]) != "C2PATXT\x00" {
			continue
		}
		if count < 13 {
			return nil, fmt.Errorf("truncated C2PA text wrapper header at character %d", at(text, start).Offset)
		}
		if header[8] != 1 {
			return nil, fmt.Errorf("unsupported C2PA text wrapper version %d", header[8])
		}
		length := uint64(binary.BigEndian.Uint32(header[9:]))
		if length > maxMetadataBytes || length != uint64(count-13) {
			return nil, fmt.Errorf("invalid C2PA text manifest length %d (available %d, limit %d)", length, count-13, maxMetadataBytes)
		}
		// Decode only the JUMBF box header; never allocate from declared lengths.
		var box [16]byte
		idx := 0
		for _, v := range text[body:end] {
			if idx >= 13 && idx < 29 {
				b, _ := selectorByte(v)
				box[idx-13] = b
			}
			idx++
			if idx >= 29 {
				break
			}
		}
		if length < 8 || string(box[4:8]) != "jumb" {
			return nil, fmt.Errorf("C2PA text wrapper does not contain a JUMBF superbox")
		}
		boxLength := uint64(binary.BigEndian.Uint32(box[:4]))
		if boxLength == 1 {
			if length < 16 {
				return nil, fmt.Errorf("truncated extended JUMBF header")
			}
			boxLength = binary.BigEndian.Uint64(box[8:])
		}
		if boxLength != 0 && boxLength != length {
			return nil, fmt.Errorf("C2PA text JUMBF length mismatch")
		}
		f := finding("c2pa_text_manifest", fmt.Sprintf("C2PA text wrapper v1; %d-byte JUMBF payload; framing checked only", length))
		f.Verification = "not_verified"
		f.Locations = []Location{at(text, start)}
		hits = append(hits, f)
	}
	return hits, nil
}

// Whitespace is not proof of steganography. The default heuristic only reports
// long tab-separated runs compatible with SNOW, and never modifies them.
func trailingWhitespace(text string, strip bool) (string, []Finding) {
	var out strings.Builder
	f := Finding{Kind: "whitespace_carrier", Detail: "SNOW-compatible trailing spaces/tabs (heuristic, not decoded)", Action: "preserve", Confidence: "heuristic"}
	if strip {
		f.Kind = "trailing_whitespace"
		f.Detail = "Trailing spaces/tabs removed by explicit option"
		f.Action = "remove"
		f.Confidence = "informational"
	}
	line, offset, column := 1, 0, 1
	for pos := 0; pos < len(text); {
		end := pos
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			end++
		}
		trim := end
		for trim > pos && (text[trim-1] == ' ' || text[trim-1] == '\t') {
			trim--
		}
		tail := text[trim:end]
		candidate := snowCompatible(tail)
		if len(tail) > 0 && (strip || candidate) {
			f.Count += len(tail)
			if len(f.Locations) < 10 {
				prefix := utf8.RuneCountInString(text[pos:trim])
				f.Locations = append(f.Locations, Location{offset + prefix, line, column + prefix})
			}
		}
		if strip {
			out.WriteString(text[pos:trim])
		}
		next := end
		if next < len(text) {
			next++
			if text[end] == '\r' && next < len(text) && text[next] == '\n' {
				next++
			}
		}
		if strip {
			out.WriteString(text[end:next])
		}
		offset += utf8.RuneCountInString(text[pos:next])
		if strings.Contains(text[end:next], "\n") {
			line++
			column = 1
		} else {
			column += utf8.RuneCountInString(text[pos:next])
		}
		pos = next
	}
	if f.Count == 0 {
		return text, nil
	}
	if !strip {
		return text, []Finding{f}
	}
	return out.String(), []Finding{f}
}

func snowCompatible(tail string) bool {
	if len(tail) < 16 || !strings.HasPrefix(tail, "\t") || strings.Count(tail, "\t") < 4 {
		return false
	}
	for _, s := range strings.Split(tail, "\t") {
		if len(s) > 7 {
			return false
		}
	}
	return true
}
