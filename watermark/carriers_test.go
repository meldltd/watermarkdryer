package watermark

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func textWrapper(data []byte) string {
	var out strings.Builder
	out.WriteRune(0xFEFF)
	for _, b := range data {
		if b < 16 {
			out.WriteRune(0xFE00 + rune(b))
		} else {
			out.WriteRune(0xE0100 + rune(b) - 16)
		}
	}
	return out.String()
}

func wrapperBytes() []byte {
	// Synthetic framing fixture, not a signed credential or validated manifest.
	return append([]byte("C2PATXT\x00\x01\x00\x00\x00\x08"), []byte{0, 0, 0, 8, 'j', 'u', 'm', 'b'}...)
}

func TestTextManifestFraming(t *testing.T) {
	for _, prefix := range []string{"Hello", "漢", "👨‍👩", ""} {
		input := prefix + textWrapper(wrapperBytes())
		out, r, err := Process("note.txt", []byte(input), Options{})
		if err != nil {
			t.Fatal(err)
		}
		want := prefix
		if prefix == "" {
			want = "\ufeff"
		} // Initial encoding BOM survives.
		if string(out) != want || !r.WouldChange {
			t.Fatalf("got %q want %q", out, want)
		}
		found := 0
		for _, f := range r.Findings {
			if f.Kind == "c2pa_text_manifest" {
				found++
				if f.Verification != "not_verified" || f.Action != "remove" {
					t.Fatal(f)
				}
			}
		}
		if found != 1 {
			t.Fatalf("no wrapper-level finding: %+v", r)
		}
		_, r, err = Process("note.txt", out, Options{})
		if err != nil || r.WouldChange {
			t.Fatal("second pass changed", err)
		}
	}
	_, r, err := Process("note.txt", []byte("A"+textWrapper(wrapperBytes())+"B"+textWrapper(wrapperBytes())), Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range r.Findings {
		if f.Kind == "c2pa_text_manifest" {
			count++
		}
	}
	if count != 2 {
		t.Fatal(count)
	}
}

func TestTextManifestInvalid(t *testing.T) {
	badVersion := wrapperBytes()
	badVersion[8] = 2
	badLength := wrapperBytes()
	binary.BigEndian.PutUint32(badLength[9:], 0xFFFFFFFF)
	badBox := wrapperBytes()
	badBox[17] = 'f'
	boxMismatch := wrapperBytes()
	boxMismatch[16] = 9
	for _, data := range [][]byte{[]byte("C2PATXT\x00"), badVersion, badLength, badBox, boxMismatch} {
		if _, _, err := Process("note.txt", []byte("A"+textWrapper(data)), Options{}); err == nil {
			t.Fatalf("accepted malformed wrapper %x", data)
		}
	}
	_, r, err := Process("note.txt", []byte("A\ufeff\ufe00\ufe01"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.Kind == "c2pa_text_manifest" {
			t.Fatal("random selectors called C2PA")
		}
	}
}

func TestTrailingWhitespaceCarriers(t *testing.T) {
	tail := "\t   \t    \t     \t  "
	input := "é" + tail + "\r\n\tcode\t\r\nend  "
	out, hits := CleanText(input, Options{})
	if out != input || len(hits) != 1 || hits[0].Kind != "whitespace_carrier" || hits[0].Action != "preserve" || hits[0].Confidence != "heuristic" {
		t.Fatalf("%q %+v", out, hits)
	}
	if hits[0].Locations[0] != (Location{1, 1, 2}) {
		t.Fatal(hits)
	}
	out, hits = CleanText(input, Options{StripTrailingWhitespace: true})
	if out != "é\r\n\tcode\r\nend" || len(hits) != 1 || hits[0].Count != len(tail)+3 {
		t.Fatalf("%q %+v", out, hits)
	}
	if again, _ := CleanText(out, Options{StripTrailingWhitespace: true}); again != out {
		t.Fatal("not idempotent")
	}
	for _, s := range []string{"Markdown break  \n", "\tindent\n", "a\t\n", "line        \t\t\t\t\n"} {
		out, hits := CleanText(s, Options{})
		if out != s || len(hits) != 0 {
			t.Fatalf("ordinary whitespace flagged: %q %+v", out, hits)
		}
	}
}

func TestXMPRemoteManifestReference(t *testing.T) {
	for _, property := range []string{`<rdf:Description p:provenance="https://example.org/opaque/123"/>`, `<rdf:Description><p:provenance rdf:resource="https://example.org/opaque/123"/></rdf:Description>`} {
		xmp := `<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:p="http://purl.org/dc/terms/">` + property + `</x:xmpmeta>`
		if !hasXMPProvenance([]byte(xmp)) {
			t.Fatal("missed aliased property")
		}
		var jpg bytes.Buffer
		jpeg.Encode(&jpg, sampleImage(), nil)
		base := jpg.Bytes()
		input := append(append(bytes.Clone(base[:2]), jpegSegment(0xE1, append([]byte("http://ns.adobe.com/xap/1.0/\x00"), []byte(xmp)...))...), base[2:]...)
		out, r, err := Process("image.jpg", input, Options{})
		if err != nil || !r.WouldChange || !bytes.Equal(out, base) {
			t.Fatalf("JPEG: %+v %v", r, err)
		}
		var p bytes.Buffer
		png.Encode(&p, sampleImage())
		base = p.Bytes()
		input = append(append(bytes.Clone(base[:len(base)-12]), pngChunk("tEXt", []byte("XML:com.adobe.xmp\x00"+xmp))...), base[len(base)-12:]...)
		out, r, err = Process("image.png", input, Options{})
		if err != nil || !r.WouldChange || !bytes.Equal(out, base) {
			t.Fatalf("PNG: %+v %v", r, err)
		}
	}
	for _, s := range []string{`<x xmlns:p="https://example.org/other"><p:provenance>https://example.org/a</p:provenance></x>`, `<x><!-- <p:provenance>example</p:provenance> --></x>`, `<x>provenance https://example.org/ordinary</x>`} {
		if hasXMPProvenance([]byte(s)) {
			t.Fatal("false property match", s)
		}
	}
}

func FuzzCarrierParsing(f *testing.F) {
	f.Add([]byte("A" + textWrapper(wrapperBytes())))
	f.Add([]byte("A" + textWrapper([]byte("C2PATXT\x00"))))
	f.Add([]byte("Text\t   \t    \t     \t  \r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		original := bytes.Clone(data)
		out, r, err := Process("input.txt", data, Options{StripTrailingWhitespace: true})
		if !bytes.Equal(data, original) {
			t.Fatal("mutated input")
		}
		if err != nil || r.Unsupported {
			return
		}
		again, after, err := Process("input.txt", out, Options{StripTrailingWhitespace: true})
		if err != nil || after.WouldChange || !bytes.Equal(out, again) {
			t.Fatalf("not idempotent: %q -> %q -> %q (%v)", data, out, again, err)
		}
	})
}
