package watermark

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

type textEncoding struct {
	name  string
	bom   []byte
	width int
	order binary.ByteOrder
}

func decodeText(data []byte) (string, textEncoding, error) {
	e := textEncoding{name: "UTF-8"}
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0, 0}):
		e = textEncoding{"UTF-32LE", data[:4], 4, binary.LittleEndian}
	case bytes.HasPrefix(data, []byte{0, 0, 0xFE, 0xFF}):
		e = textEncoding{"UTF-32BE", data[:4], 4, binary.BigEndian}
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		e = textEncoding{"UTF-16LE", data[:2], 2, binary.LittleEndian}
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		e = textEncoding{"UTF-16BE", data[:2], 2, binary.BigEndian}
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		e.bom = data[:3]
	}
	data = data[len(e.bom):]
	var text string
	if e.width == 0 {
		if !utf8.Valid(data) {
			return "", e, fmt.Errorf("not valid UTF-8 (UTF-16/32 require a BOM)")
		}
		text = string(data)
	} else {
		if len(data)%e.width != 0 {
			return "", e, fmt.Errorf("truncated %s code unit", e.name)
		}
		rs := make([]rune, 0, len(data)/e.width)
		for i := 0; i < len(data); i += e.width {
			var r rune
			if e.width == 4 {
				u := e.order.Uint32(data[i:])
				if u > utf8.MaxRune {
					return "", e, fmt.Errorf("invalid UTF-32 scalar")
				}
				r = rune(u)
				if utf16.IsSurrogate(r) {
					return "", e, fmt.Errorf("invalid UTF-32 surrogate")
				}
			} else {
				r = rune(e.order.Uint16(data[i:]))
				if between(r, 0xD800, 0xDBFF) {
					if i+4 > len(data) {
						return "", e, fmt.Errorf("unpaired UTF-16 surrogate")
					}
					low := rune(e.order.Uint16(data[i+2:]))
					if !between(low, 0xDC00, 0xDFFF) {
						return "", e, fmt.Errorf("unpaired UTF-16 surrogate")
					}
					r = utf16.DecodeRune(r, low)
					i += 2
				} else if utf16.IsSurrogate(r) {
					return "", e, fmt.Errorf("unpaired UTF-16 surrogate")
				}
			}
			rs = append(rs, r)
		}
		text = string(rs)
	}
	for _, r := range text {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f' {
			return "", e, fmt.Errorf("binary control byte U+%04X in text", r)
		}
	}
	return text, e, nil
}

func (e textEncoding) encode(text string) []byte {
	out := append([]byte{}, e.bom...)
	if e.width == 0 {
		return append(out, []byte(text)...)
	}
	var b [4]byte
	if e.width == 2 {
		for _, u := range utf16.Encode([]rune(text)) {
			e.order.PutUint16(b[:], u)
			out = append(out, b[:2]...)
		}
	} else {
		for _, r := range text {
			e.order.PutUint32(b[:], uint32(r))
			out = append(out, b[:]...)
		}
	}
	return out
}
