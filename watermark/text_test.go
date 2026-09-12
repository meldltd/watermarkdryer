package watermark

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestUnicodeCleaning(t *testing.T) {
	tests := []struct {
		name, input, want string
		opts              Options
	}{
		{"clean", "Hello, world!\r\nČeščina 日本語 العربية 👋🏽", "Hello, world!\r\nČeščina 日本語 العربية 👋🏽", Options{}},
		{"carriers", "a\u200bb\u2060c\ufeffd\u034fe\u00adf", "abcdef", Options{}},
		{"spaces", "a\u00a0b\u2009c\u202fd\u3000e", "a b c d e", Options{}},
		{"keep spaces", "a\u00a0b", "a\u00a0b", Options{KeepSpaces: true}},
		{"emoji", "👨‍👩‍👧 ❤️‍🔥 ⚖️ 1️⃣ ©️ ‼️", "👨‍👩‍👧 ❤️‍🔥 ⚖️ 1️⃣ ©️ ‼️", Options{}},
		{"free glue", "x\u200dy\ufe0fz\U000e0100", "xyz", Options{}},
		{"scripts", "می‌روم क्‍ष", "می‌روم क्‍ष", Options{}},
		{"CJK selectors", "漢\ufe00葛\U000e0100", "漢\ufe00葛\U000e0100", Options{}},
		{"mongolian", "ᠠ\u180b", "ᠠ\u180b", Options{}},
		{"removed selector base", "a\u180e\u180b\u180c", "a", Options{}},
		{"khmer", "ក\u17b4", "ក\u17b4", Options{}},
		{"hangul", "ᄀ\u1160", "ᄀ\u1160", Options{}},
		{"flag", "🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f", "🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f", Options{}},
		{"incomplete flag", "🏴\U000e0067\U000e0062", "🏴", Options{}},
		{"legitimate bidi", "a\u200f\u202bשלום\u202c!", "a\u200f\u202bשלום\u202c!", Options{}},
		{"bidi override", "a\u202eb\u202cc\u202a", "abc", Options{}},
		{"explicit bidi", "a\u200f\u202bשלום\u202c!", "aשלום!", Options{StripBidi: true}},
		{"explicit glue", "👨‍👩 ⚖️ می‌روم", "👨👩 ⚖ میروم", Options{StripGlue: true}},
		{"private and reserved", "a\ue000\U000f0000\U00100000\ufdd0\ufffe\U0010ffff\U000e0000\U000e0080\u2065b", "ab", Options{}},
		{"orthographic Cf", "\u0600ع \U00013430\U00013000", "\u0600ع \U00013430\U00013000", Options{}},
		{"isolated layout", "a\U00013430b", "ab", Options{}},
		{"confusables default", "АBC а Ａｚ", "АBC а Ａｚ", Options{}},
		{"confusables opt-in", "АBC а Ａｚ", "ABC a Az", Options{AggressiveHomoglyphs: true}},
		{"NFKC", "ﬀ Ａ ①", "ff A 1", Options{NFKC: true}},
		{"NFKC script context", "가\u1160", "가", Options{NFKC: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hits := CleanText(tt.input, tt.opts)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if again, _ := CleanText(got, tt.opts); again != got {
				t.Fatalf("not idempotent: %q -> %q", got, again)
			}
			if got != tt.input && len(hits) == 0 {
				t.Fatal("unreported modification")
			}
		})
	}
}

func TestInputLocationsAndCounts(t *testing.T) {
	_, hits := CleanText("é\u200b\n\u200bX\u200b", Options{})
	if len(hits) != 1 || hits[0].Count != 3 {
		t.Fatalf("%+v", hits)
	}
	want := []Location{{1, 1, 2}, {3, 2, 1}, {5, 2, 3}}
	for i, l := range hits[0].Locations {
		if l != want[i] {
			t.Fatalf("%+v != %+v", l, want[i])
		}
	}
	_, hits = CleanText(strings.Repeat("\u200b", 10000), Options{})
	if hits[0].Count != 10000 || len(hits[0].Locations) != 10 {
		t.Fatal("sample limit not applied")
	}
}

func TestEncodingRoundTrips(t *testing.T) {
	for _, e := range []textEncoding{{name: "UTF-8"}, {name: "UTF-8", bom: []byte{0xEF, 0xBB, 0xBF}}, {"UTF-16LE", []byte{0xFF, 0xFE}, 2, binary.LittleEndian}, {"UTF-16BE", []byte{0xFE, 0xFF}, 2, binary.BigEndian}, {"UTF-32LE", []byte{0xFF, 0xFE, 0, 0}, 4, binary.LittleEndian}, {"UTF-32BE", []byte{0, 0, 0xFE, 0xFF}, 4, binary.BigEndian}} {
		t.Run(e.name, func(t *testing.T) {
			input := e.encode("A\u200bB\r\n👨‍👩")
			out, r, err := Process("file.txt", input, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !r.WouldChange || !bytes.Equal(out, e.encode("AB\r\n👨‍👩")) {
				t.Fatalf("bad roundtrip: %x %+v", out, r)
			}
			out2, r2, err := Process("file.txt", out, Options{})
			if err != nil || r2.WouldChange || !bytes.Equal(out, out2) {
				t.Fatalf("second pass changed: %+v %v", r2, err)
			}
		})
	}
}

func TestRefuseMalformedEncodings(t *testing.T) {
	for _, data := range [][]byte{{0xFF}, {0xFF, 0xFE, 0x00}, {0xFE, 0xFF, 0xD8, 0x00}, {0xFE, 0xFF, 0xDC, 0x00}, {0xFF, 0xFE, 0, 0, 0, 0, 0x11, 0}, {'a', 0, 'b'}, {0xFF, 0xFE, 0, 0, 0, 0xD8, 0, 0}} {
		if _, _, err := Process("file.txt", data, Options{}); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
}

func TestUnknownAndDisguisedBinary(t *testing.T) {
	for _, data := range [][]byte{[]byte("%PDF-1.7\n"), []byte("PK\x03\x04abc"), []byte("GIF89a"), {0, 1, 2, 3}} {
		out, r, err := Process("file", data, Options{})
		if err != nil || !r.Unsupported || !bytes.Equal(out, data) {
			t.Fatalf("%x: %+v %v", data, r, err)
		}
	}
	_, r, err := Process("file.txt", []byte("%PDF-1.7"), Options{})
	if err != nil || !r.Unsupported {
		t.Fatal("extension overrode PDF magic")
	}
}

func FuzzProcess(f *testing.F) {
	for _, s := range []string{"hello\u200bworld", "👨‍👩\r\n", "---\ngenerator: Claude\n---\nHello", "\x89PNG\r\n\x1a\n", "\xff\xd8", "RIFF\x04\x00\x00\x00WEBP", "\xff\xfea\x00"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		original := bytes.Clone(data)
		out, r, err := Process("input.md", data, Options{})
		if !bytes.Equal(data, original) {
			t.Fatal("modified input buffer")
		}
		if err != nil || r.Unsupported {
			return
		}
		if r.WouldChange == bytes.Equal(data, out) {
			t.Fatal("change flag disagrees")
		}
		out2, r2, err := Process("input.md", out, Options{})
		if err != nil || r2.WouldChange || !bytes.Equal(out, out2) {
			t.Fatalf("not idempotent: %q -> %q -> %q (%v)", data, out, out2, err)
		}
	})
}
